package e2e_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AbdullahZaher/ruxbase/apis"
	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/tests"
)

func TestAI_ScopedAgentDiscoveryLeastPrivilegeAndCrossTenantNegatives(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	// Create user A (agent owner) and user B (other tenant)
	usersCol, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatalf("Failed to find users collection: %v", err)
	}

	userA, err := app.FindAuthRecordByEmail("users", "test@example.com")
	if err != nil {
		t.Fatalf("Failed to find test user A: %v", err)
	}
	userAToken, _ := userA.NewAuthToken()

	userB := core.NewRecord(usersCol)
	userB.SetEmail("bob@example.com")
	userB.SetPassword("Password123456!")
	if err := app.Save(userB); err != nil {
		t.Fatalf("Failed to create user B: %v", err)
	}
	userBToken, _ := userB.NewAuthToken()

	superuser, err := app.FindAuthRecordByEmail(core.CollectionNameSuperusers, "test@example.com")
	if err != nil {
		t.Fatalf("Failed to find superuser: %v", err)
	}
	superToken, _ := superuser.NewAuthToken()

	// Create two non-system collections: "public_articles" (allowed) and "payroll_vault" (denied)
	colAllowed := core.NewBaseCollection("public_articles")
	colAllowed.Fields.Add(&core.TextField{Name: "title"})
	if err := app.Save(colAllowed); err != nil {
		t.Fatalf("Failed to create allowed collection: %v", err)
	}

	colDenied := core.NewBaseCollection("payroll_vault")
	colDenied.Fields.Add(&core.TextField{Name: "salary"})
	if err := app.Save(colDenied); err != nil {
		t.Fatalf("Failed to create denied collection: %v", err)
	}

	router, err := apis.NewRouter(app)
	if err != nil {
		t.Fatalf("Failed to create router: %v", err)
	}
	mux, err := router.BuildMux()
	if err != nil {
		t.Fatalf("Failed to build mux: %v", err)
	}

	// 1. Register scoped AI agent owned by userA, allowed only on "public_articles" with read scope
	regBody, _ := json.Marshal(map[string]any{
		"name":               "content-reader-agent",
		"ownerId":            userA.Id,
		"scopes":             []string{"collections:read"},
		"allowedCollections": []string{"public_articles"},
	})
	reqReg := httptest.NewRequest(http.MethodPost, "/api/ai/agents", bytes.NewReader(regBody))
	reqReg.Header.Set("Authorization", superToken)
	reqReg.Header.Set("Content-Type", "application/json")
	recReg := httptest.NewRecorder()
	mux.ServeHTTP(recReg, reqReg)
	if recReg.Code != http.StatusCreated {
		t.Fatalf("Expected 201 Created registering agent, got %d: %s", recReg.Code, recReg.Body.String())
	}

	var agent core.AgentIdentity
	_ = json.Unmarshal(recReg.Body.Bytes(), &agent)

	// 2. Tool discovery by owner userA must expose ONLY public_articles (never payroll_vault or _superusers)
	reqDisc := httptest.NewRequest(http.MethodGet, "/api/ai/agents/"+agent.Id+"/tools", nil)
	reqDisc.Header.Set("Authorization", userAToken)
	recDisc := httptest.NewRecorder()
	mux.ServeHTTP(recDisc, reqDisc)
	if recDisc.Code != http.StatusOK {
		t.Fatalf("Expected 200 on tool discovery, got %d: %s", recDisc.Code, recDisc.Body.String())
	}
	discStr := recDisc.Body.String()
	if !strings.Contains(discStr, "read_public_articles") {
		t.Fatalf("Expected read_public_articles in discovery output: %s", discStr)
	}
	if strings.Contains(discStr, "payroll_vault") || strings.Contains(discStr, "_superusers") || strings.Contains(discStr, "write_public_articles") {
		t.Fatalf("Least-privilege / scope leak in discovery output! %s", discStr)
	}

	// 3. Cross-tenant negative: User B cannot discover or invoke User A's agent
	reqCrossDisc := httptest.NewRequest(http.MethodGet, "/api/ai/agents/"+agent.Id+"/tools", nil)
	reqCrossDisc.Header.Set("Authorization", userBToken)
	recCrossDisc := httptest.NewRecorder()
	mux.ServeHTTP(recCrossDisc, reqCrossDisc)
	if recCrossDisc.Code != http.StatusForbidden {
		t.Fatalf("Cross-tenant tool discovery breach! Expected 403, got %d", recCrossDisc.Code)
	}

	// 4. Denied collection invocation negative -> 403 & recorded in audit log
	invokeDeniedBody, _ := json.Marshal(map[string]any{
		"toolName":         "read_payroll_vault",
		"targetCollection": "payroll_vault",
		"classification":   core.ToolClassRead,
		"requiredScope":    "collections:read",
	})
	reqDenied := httptest.NewRequest(http.MethodPost, "/api/ai/agents/"+agent.Id+"/invoke", bytes.NewReader(invokeDeniedBody))
	reqDenied.Header.Set("Authorization", userAToken)
	reqDenied.Header.Set("Content-Type", "application/json")
	recDenied := httptest.NewRecorder()
	mux.ServeHTTP(recDenied, reqDenied)
	if recDenied.Code != http.StatusForbidden {
		t.Fatalf("Expected 403 for unapproved collection payroll_vault, got %d", recDenied.Code)
	}

	// 5. Prompt injection negative -> attempting DROP TABLE or _superusers inside payload MUST be blocked & audited
	injBody, _ := json.Marshal(map[string]any{
		"toolName":         "read_public_articles",
		"targetCollection": "public_articles",
		"classification":   core.ToolClassRead,
		"requiredScope":    "collections:read",
		"payload": map[string]any{
			"filter": "id != '' UNION SELECT * FROM _superusers --",
		},
	})
	reqInj := httptest.NewRequest(http.MethodPost, "/api/ai/agents/"+agent.Id+"/invoke", bytes.NewReader(injBody))
	reqInj.Header.Set("Authorization", userAToken)
	reqInj.Header.Set("Content-Type", "application/json")
	recInj := httptest.NewRecorder()
	mux.ServeHTTP(recInj, reqInj)
	if recInj.Code != http.StatusForbidden {
		t.Fatalf("Prompt injection breach! Expected 403, got %d", recInj.Code)
	}
}

func TestAI_HumanApprovalWorkflowAndReplayBypassPrevention(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	userA, _ := app.FindAuthRecordByEmail("users", "test@example.com")
	userAToken, _ := userA.NewAuthToken()
	superuser, _ := app.FindAuthRecordByEmail(core.CollectionNameSuperusers, "test@example.com")
	superToken, _ := superuser.NewAuthToken()

	// Register directly via core helper cleanly
	scopedAgent, err := core.RegisterAgent(t.Context(), app, &core.AgentIdentity{
		Name:               "cleanup-agent",
		OwnerId:            userA.Id,
		Scopes:             []string{"collections:read", "collections:delete"},
		AllowedCollections: []string{"temp_logs"},
	})
	if err != nil {
		t.Fatalf("Failed to register agent: %v", err)
	}

	router, _ := apis.NewRouter(app)
	mux, _ := router.BuildMux()

	// 1. Attempting destructive delete without approval -> MUST fail 403 (Approval Required)
	delBody, _ := json.Marshal(map[string]any{
		"toolName":         "delete_temp_logs",
		"targetCollection": "temp_logs",
		"classification":   core.ToolClassDestructive,
		"requiredScope":    "collections:delete",
	})
	reqNoAppr := httptest.NewRequest(http.MethodPost, "/api/ai/agents/"+scopedAgent.Id+"/invoke", bytes.NewReader(delBody))
	reqNoAppr.Header.Set("Authorization", userAToken)
	reqNoAppr.Header.Set("Content-Type", "application/json")
	recNoAppr := httptest.NewRecorder()
	mux.ServeHTTP(recNoAppr, reqNoAppr)
	if recNoAppr.Code != http.StatusForbidden {
		t.Fatalf("Approval bypass! Expected 403 without approvalId, got %d", recNoAppr.Code)
	}

	// 2. Request human approval
	apprReqBody, _ := json.Marshal(map[string]any{
		"toolName":       "delete_temp_logs",
		"classification": core.ToolClassDestructive,
		"targetResource": "temp_logs",
	})
	reqCreateAppr := httptest.NewRequest(http.MethodPost, "/api/ai/agents/"+scopedAgent.Id+"/approvals", bytes.NewReader(apprReqBody))
	reqCreateAppr.Header.Set("Authorization", userAToken)
	reqCreateAppr.Header.Set("Content-Type", "application/json")
	recCreateAppr := httptest.NewRecorder()
	mux.ServeHTTP(recCreateAppr, reqCreateAppr)
	if recCreateAppr.Code != http.StatusCreated {
		t.Fatalf("Failed to create approval request: %d %s", recCreateAppr.Code, recCreateAppr.Body.String())
	}

	var approval core.ToolApprovalRequest
	_ = json.Unmarshal(recCreateAppr.Body.Bytes(), &approval)

	// 3. Non-superuser attempting to self-approve -> MUST fail 401/403
	decideBody, _ := json.Marshal(map[string]any{"status": "approved", "reason": "self-approved bypass"})
	reqSelfAppr := httptest.NewRequest(http.MethodPost, "/api/ai/approvals/"+approval.Id+"/decide", bytes.NewReader(decideBody))
	reqSelfAppr.Header.Set("Authorization", userAToken)
	reqSelfAppr.Header.Set("Content-Type", "application/json")
	recSelfAppr := httptest.NewRecorder()
	mux.ServeHTTP(recSelfAppr, reqSelfAppr)
	if recSelfAppr.Code != http.StatusForbidden && recSelfAppr.Code != http.StatusUnauthorized {
		t.Fatalf("Approval RBAC bypass! Regular user approved sensitive request: %d", recSelfAppr.Code)
	}

	// 4. Superuser approves request
	reqAdminAppr := httptest.NewRequest(http.MethodPost, "/api/ai/approvals/"+approval.Id+"/decide", bytes.NewReader(decideBody))
	reqAdminAppr.Header.Set("Authorization", superToken)
	reqAdminAppr.Header.Set("Content-Type", "application/json")
	recAdminAppr := httptest.NewRecorder()
	mux.ServeHTTP(recAdminAppr, reqAdminAppr)
	if recAdminAppr.Code != http.StatusOK {
		t.Fatalf("Expected 200 on superuser approval, got %d: %s", recAdminAppr.Code, recAdminAppr.Body.String())
	}

	// 5. Execute destructive action with valid single-use approvalId -> 200 OK
	delWithApprBody, _ := json.Marshal(map[string]any{
		"toolName":         "delete_temp_logs",
		"targetCollection": "temp_logs",
		"classification":   core.ToolClassDestructive,
		"requiredScope":    "collections:delete",
		"approvalId":       approval.Id,
	})
	reqExec := httptest.NewRequest(http.MethodPost, "/api/ai/agents/"+scopedAgent.Id+"/invoke", bytes.NewReader(delWithApprBody))
	reqExec.Header.Set("Authorization", userAToken)
	reqExec.Header.Set("Content-Type", "application/json")
	recExec := httptest.NewRecorder()
	mux.ServeHTTP(recExec, reqExec)
	if recExec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK with valid approvalId, got %d: %s", recExec.Code, recExec.Body.String())
	}

	// 6. Replay same approvalId -> MUST fail 403 (already consumed!)
	reqReplay := httptest.NewRequest(http.MethodPost, "/api/ai/agents/"+scopedAgent.Id+"/invoke", bytes.NewReader(delWithApprBody))
	reqReplay.Header.Set("Authorization", userAToken)
	reqReplay.Header.Set("Content-Type", "application/json")
	recReplay := httptest.NewRecorder()
	mux.ServeHTTP(recReplay, reqReplay)
	if recReplay.Code != http.StatusForbidden {
		t.Fatalf("Approval replay bypass! Reused approvalId returned %d", recReplay.Code)
	}
}

func TestAI_KnowledgeRAGCrossTenantIsolation(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	usersCol, _ := app.FindCollectionByNameOrId("users")
	userA, _ := app.FindAuthRecordByEmail("users", "test@example.com")
	userAToken, _ := userA.NewAuthToken()

	userB := core.NewRecord(usersCol)
	userB.SetEmail("tenantb_rag@example.com")
	userB.SetPassword("Password123456!")
	_ = app.Save(userB)
	userBToken, _ := userB.NewAuthToken()

	router, _ := apis.NewRouter(app)
	mux, _ := router.BuildMux()

	// User A indexes secret knowledge chunk
	ragBody, _ := json.Marshal(map[string]any{
		"namespace": "handbook",
		"title":     "Merger Secret",
		"content":   "Confidential Project Alpha acquisition valuation is $50M.",
	})
	reqIdx := httptest.NewRequest(http.MethodPost, "/api/ai/knowledge", bytes.NewReader(ragBody))
	reqIdx.Header.Set("Authorization", userAToken)
	reqIdx.Header.Set("Content-Type", "application/json")
	recIdx := httptest.NewRecorder()
	mux.ServeHTTP(recIdx, reqIdx)
	if recIdx.Code != http.StatusCreated {
		t.Fatalf("Failed to index knowledge chunk: %d %s", recIdx.Code, recIdx.Body.String())
	}

	// User A searches for "acquisition" -> returns 1 chunk
	reqSearchA := httptest.NewRequest(http.MethodGet, "/api/ai/knowledge/search?q=acquisition", nil)
	reqSearchA.Header.Set("Authorization", userAToken)
	recSearchA := httptest.NewRecorder()
	mux.ServeHTTP(recSearchA, reqSearchA)
	if recSearchA.Code != http.StatusOK || !strings.Contains(recSearchA.Body.String(), "Project Alpha") {
		t.Fatalf("Owner RAG search failed: %d %s", recSearchA.Code, recSearchA.Body.String())
	}

	// User B searches for "acquisition" -> MUST return 0 items (zero cross-tenant leakage)
	reqSearchB := httptest.NewRequest(http.MethodGet, "/api/ai/knowledge/search?q=acquisition", nil)
	reqSearchB.Header.Set("Authorization", userBToken)
	recSearchB := httptest.NewRecorder()
	mux.ServeHTTP(recSearchB, reqSearchB)
	if recSearchB.Code != http.StatusOK || strings.Contains(recSearchB.Body.String(), "Project Alpha") {
		t.Fatalf("Cross-tenant RAG knowledge leak! User B saw User A's secret chunk: %s", recSearchB.Body.String())
	}
}

func TestPlugins_VersionedLifecycleCompatibilityAndIsolation(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	superuser, _ := app.FindAuthRecordByEmail(core.CollectionNameSuperusers, "test@example.com")
	superToken, _ := superuser.NewAuthToken()
	router, _ := apis.NewRouter(app)
	mux, _ := router.BuildMux()

	// 1. Reject plugin requiring incompatible future Core SDK version (e.g., 99.0.0)
	incompatBody, _ := json.Marshal(map[string]any{
		"name":           "future-plugin",
		"version":        "1.0.0",
		"minCoreVersion": "99.0.0",
		"permissions":    []string{"collections:read"},
	})
	reqIncompat := httptest.NewRequest(http.MethodPost, "/api/plugins/load", bytes.NewReader(incompatBody))
	reqIncompat.Header.Set("Authorization", superToken)
	reqIncompat.Header.Set("Content-Type", "application/json")
	recIncompat := httptest.NewRecorder()
	mux.ServeHTTP(recIncompat, reqIncompat)
	if recIncompat.Code != http.StatusBadRequest {
		t.Fatalf("Expected 400 Bad Request for incompatible minCoreVersion, got %d", recIncompat.Code)
	}

	// 2. Reject plugin requesting prohibited host_secrets or superuser_db privilege
	privBody, _ := json.Marshal(map[string]any{
		"name":           "evil-secret-stealer",
		"version":        "1.0.0",
		"minCoreVersion": "0.1.0",
		"permissions":    []string{"host_secrets", "superuser_db"},
	})
	reqPriv := httptest.NewRequest(http.MethodPost, "/api/plugins/load", bytes.NewReader(privBody))
	reqPriv.Header.Set("Authorization", superToken)
	reqPriv.Header.Set("Content-Type", "application/json")
	recPriv := httptest.NewRecorder()
	mux.ServeHTTP(recPriv, reqPriv)
	if recPriv.Code != http.StatusBadRequest {
		t.Fatalf("Plugin isolation breach! Disallowed permissions accepted with status %d", recPriv.Code)
	}

	// 3. Load compatible v1.0.0 plugin
	loadBody, _ := json.Marshal(map[string]any{
		"name":           "analytics-exporter",
		"version":        "1.0.0",
		"minCoreVersion": "1.0.0",
		"permissions":    []string{"collections:read", "webhook:emit"},
	})
	reqLoad := httptest.NewRequest(http.MethodPost, "/api/plugins/load", bytes.NewReader(loadBody))
	reqLoad.Header.Set("Authorization", superToken)
	reqLoad.Header.Set("Content-Type", "application/json")
	recLoad := httptest.NewRecorder()
	mux.ServeHTTP(recLoad, reqLoad)
	if recLoad.Code != http.StatusCreated {
		t.Fatalf("Expected 201 Created loading plugin, got %d: %s", recLoad.Code, recLoad.Body.String())
	}

	// 4. Controlled upgrade to v1.2.0
	upgBody, _ := json.Marshal(map[string]any{
		"version":        "1.2.0",
		"minCoreVersion": "1.0.0",
		"permissions":    []string{"collections:read"},
	})
	reqUpg := httptest.NewRequest(http.MethodPost, "/api/plugins/analytics-exporter/upgrade", bytes.NewReader(upgBody))
	reqUpg.Header.Set("Authorization", superToken)
	reqUpg.Header.Set("Content-Type", "application/json")
	recUpg := httptest.NewRecorder()
	mux.ServeHTTP(recUpg, reqUpg)
	if recUpg.Code != http.StatusOK || !strings.Contains(recUpg.Body.String(), `"version":"1.2.0"`) {
		t.Fatalf("Plugin upgrade failed: %d %s", recUpg.Code, recUpg.Body.String())
	}

	// 5. Controlled unload
	reqUnload := httptest.NewRequest(http.MethodPost, "/api/plugins/analytics-exporter/unload", nil)
	reqUnload.Header.Set("Authorization", superToken)
	recUnload := httptest.NewRecorder()
	mux.ServeHTTP(recUnload, reqUnload)
	if recUnload.Code != http.StatusOK {
		t.Fatalf("Plugin unload failed: %d %s", recUnload.Code, recUnload.Body.String())
	}
}
