package e2e_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AbdullahZaher/ruxbase/apis"
	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/tests"
)

type openAPIContract struct {
	OpenAPI string `json:"openapi"`
	Info    struct {
		Title   string `json:"title"`
		Version string `json:"version"`
	} `json:"info"`
	Paths      map[string]map[string]openAPIOperation `json:"paths"`
	Components struct {
		SecuritySchemes map[string]any `json:"securitySchemes"`
		Schemas         map[string]any `json:"schemas"`
	} `json:"components"`
}

type openAPIOperation struct {
	OperationId string             `json:"operationId"`
	Summary     string             `json:"summary"`
	Security    []map[string][]any `json:"security"`
	Responses   map[string]any     `json:"responses"`
}

type referenceManifest struct {
	SchemaVersion int `json:"schemaVersion"`
	Operations    []struct {
		Method      string `json:"method"`
		Path        string `json:"path"`
		OperationId string `json:"operationId"`
		Success     string `json:"success"`
	} `json:"operations"`
}

func loadOpenAPISpec(t *testing.T) *openAPIContract {
	t.Helper()
	rootPath, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("Failed to resolve root path: %v", err)
	}
	specPath := filepath.Join(rootPath, "docs/contracts/openapi.json")
	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("Failed to read openapi.json: %v", err)
	}

	var spec openAPIContract
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatalf("Failed to parse openapi.json: %v", err)
	}
	return &spec
}

func TestOpenAPIContract_SchemaValidation(t *testing.T) {
	spec := loadOpenAPISpec(t)

	// 1. Validate OpenAPI version
	if spec.OpenAPI != "3.1.0" {
		t.Errorf("Expected OpenAPI version 3.1.0, got %s", spec.OpenAPI)
	}

	// 2. Validate Info object
	if spec.Info.Title != "Ruxbase API (scoped reference)" {
		t.Errorf("Expected title 'Ruxbase API (scoped reference)', got %q", spec.Info.Title)
	}

	// 3. Validate mandatory security schemes
	schemes := spec.Components.SecuritySchemes
	expectedSchemes := []string{"RecordToken", "SuperuserToken", "FileToken", "SuperuserFileToken"}
	for _, schemeName := range expectedSchemes {
		if _, ok := schemes[schemeName]; !ok {
			t.Errorf("Missing mandatory securityScheme: %s", schemeName)
		}
	}

	// 4. Validate operations
	operationIds := make(map[string]string)
	for path, methods := range spec.Paths {
		for method, op := range methods {
			if op.OperationId == "" {
				t.Errorf("Operation %s %s missing operationId", method, path)
			}
			if existing, exists := operationIds[op.OperationId]; exists {
				t.Errorf("Duplicate operationId %q on %s %s (already on %s)", op.OperationId, method, path, existing)
			}
			operationIds[op.OperationId] = method + " " + path

			// Must declare at least one 2xx response
			has2xx := false
			for code := range op.Responses {
				if strings.HasPrefix(code, "2") {
					has2xx = true
					break
				}
			}
			if !has2xx {
				t.Errorf("Operation %s %s (%s) missing 2xx success response", method, path, op.OperationId)
			}
		}
	}
}

func TestOpenAPIContract_NoUndocumentedPrivilegeExposure(t *testing.T) {
	spec := loadOpenAPISpec(t)

	adminPaths := []string{
		"/api/collections",
		"/api/collections/{collection}",
		"/api/collections/{collection}/rls",
		"/api/settings",
		"/api/backups",
		"/api/logs",
		"/api/crons",
		"/api/sql",
	}

	// 1. Verify spec declarations require SuperuserToken
	for _, path := range adminPaths {
		methods, ok := spec.Paths[path]
		if !ok {
			continue
		}
		for method, op := range methods {
			if len(op.Security) == 0 {
				t.Errorf("Administrative operation %s %s lacks security declaration (undocumented privilege exposure)", method, path)
				continue
			}
			hasSuperuser := false
			for _, sec := range op.Security {
				if _, ok := sec["SuperuserToken"]; ok {
					hasSuperuser = true
					break
				}
			}
			if !hasSuperuser {
				t.Errorf("Administrative operation %s %s does not require SuperuserToken: %v", method, path, op.Security)
			}
		}
	}

	// 2. Runtime verification: execute actual HTTP requests against router to prove fail-closed enforcement
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	router, err := apis.NewRouter(app)
	if err != nil {
		t.Fatalf("Failed to create router: %v", err)
	}
	mux, err := router.BuildMux()
	if err != nil {
		t.Fatalf("Failed to build mux: %v", err)
	}

	// Create regular non-admin user
	usersCol, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatalf("Failed to find users collection: %v", err)
	}
	regularUser := core.NewRecord(usersCol)
	regularUser.SetEmail("regular_contract_user@example.com")
	regularUser.SetPassword("validpassword123")
	regularUser.SetVerified(true)
	if err := app.Save(regularUser); err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	userToken, err := regularUser.NewAuthToken()
	if err != nil {
		t.Fatalf("Failed to generate token: %v", err)
	}

	testEndpoints := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/collections"},
		{http.MethodGet, "/api/settings"},
		{http.MethodGet, "/api/backups"},
		{http.MethodGet, "/api/logs"},
		{http.MethodGet, "/api/crons"},
		{http.MethodPost, "/api/sql"},
	}

	for _, ep := range testEndpoints {
		// A. Anonymous caller must be rejected (401)
		reqAnon := httptest.NewRequest(ep.method, ep.path, nil)
		recAnon := httptest.NewRecorder()
		mux.ServeHTTP(recAnon, reqAnon)
		if recAnon.Code != http.StatusUnauthorized {
			t.Errorf("Undocumented privilege exposure! Anonymous %s %s returned %d (expected 401): %s",
				ep.method, ep.path, recAnon.Code, recAnon.Body.String())
		}

		// B. Regular user must be forbidden (403)
		reqUser := httptest.NewRequest(ep.method, ep.path, nil)
		reqUser.Header.Set("Authorization", userToken)
		recUser := httptest.NewRecorder()
		mux.ServeHTTP(recUser, reqUser)
		if recUser.Code != http.StatusForbidden {
			t.Errorf("Undocumented privilege exposure! Regular user %s %s returned %d (expected 403): %s",
				ep.method, ep.path, recUser.Code, recUser.Body.String())
		}
	}
}

func TestOpenAPIContract_RouteCoverageAndInventory(t *testing.T) {
	spec := loadOpenAPISpec(t)

	rootPath, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("Failed to resolve root path: %v", err)
	}
	refPath := filepath.Join(rootPath, "docs/contracts/reference.json")
	data, err := os.ReadFile(refPath)
	if err != nil {
		t.Fatalf("Failed to read reference.json: %v", err)
	}

	var ref referenceManifest
	if err := json.Unmarshal(data, &ref); err != nil {
		t.Fatalf("Failed to parse reference.json: %v", err)
	}

	// Verify that every operation in reference.json inventory exists in openapi.json
	for _, op := range ref.Operations {
		methods, pathOk := spec.Paths[op.Path]
		if !pathOk {
			t.Errorf("Documented operation %s %s not found in OpenAPI paths", op.Method, op.Path)
			continue
		}
		foundOp, methodOk := methods[strings.ToLower(op.Method)]
		if !methodOk {
			t.Errorf("Documented operation %s %s not found in OpenAPI methods", op.Method, op.Path)
			continue
		}
		if foundOp.OperationId != op.OperationId {
			t.Errorf("OperationId mismatch for %s %s: expected %q, got %q",
				op.Method, op.Path, op.OperationId, foundOp.OperationId)
		}
	}
}
