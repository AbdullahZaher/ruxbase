package apis

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/tools/router"
)

// bindAIPlatformApi registers AI agent governance, human approval, knowledge/RAG, and
// versioned plugin lifecycle endpoints.
func bindAIPlatformApi(app core.App, rg *router.RouterGroup[*core.RequestEvent]) {
	aiAdmin := rg.Group("/ai").Bind(RequireSuperuserAuth())
	aiAdmin.POST("/agents", aiRegisterAgent)
	aiAdmin.POST("/approvals/{id}/decide", aiDecideApproval)
	aiAdmin.GET("/audit", aiListAuditLogs)

	aiUser := rg.Group("/ai").Bind(RequireAuth())
	aiUser.GET("/agents/{id}/tools", aiDiscoverTools)
	aiUser.POST("/agents/{id}/invoke", aiInvokeTool)
	aiUser.POST("/agents/{id}/approvals", aiRequestApproval)
	aiUser.POST("/knowledge", aiIndexKnowledge)
	aiUser.GET("/knowledge/search", aiSearchKnowledge)

	pluginsAdmin := rg.Group("/plugins").Bind(RequireSuperuserAuth())
	pluginsAdmin.GET("", pluginsList)
	pluginsAdmin.POST("/load", pluginsLoad)
	pluginsAdmin.POST("/{name}/upgrade", pluginsUpgrade)
	pluginsAdmin.POST("/{name}/unload", pluginsUnload)
}

func aiRegisterAgent(e *core.RequestEvent) error {
	var body struct {
		Name               string   `json:"name"`
		OwnerId            string   `json:"ownerId"`
		Scopes             []string `json:"scopes"`
		AllowedCollections []string `json:"allowedCollections"`
	}
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("Invalid agent payload.", err)
	}

	agent, err := core.RegisterAgent(e.Request.Context(), e.App, &core.AgentIdentity{
		Name:               body.Name,
		OwnerId:            body.OwnerId,
		Scopes:             body.Scopes,
		AllowedCollections: body.AllowedCollections,
	})
	if err != nil {
		return e.BadRequestError("Failed to register AI agent.", err)
	}
	return e.JSON(http.StatusCreated, agent)
}

func checkAgentCallerAccess(e *core.RequestEvent, agentId string) (*core.AgentIdentity, error) {
	agent, err := core.GetAgent(e.Request.Context(), e.App, agentId)
	if err != nil {
		return nil, err
	}
	if !e.HasSuperuserAuth() && agent.OwnerId != "" && (e.Auth == nil || agent.OwnerId != e.Auth.Id) {
		return nil, e.ForbiddenError("Cross-tenant AI agent access denied.", nil)
	}
	return agent, nil
}

func aiDiscoverTools(e *core.RequestEvent) error {
	agentId := e.Request.PathValue("id")
	if _, err := checkAgentCallerAccess(e, agentId); err != nil {
		if errors.Is(err, core.ErrAgentNotFound) {
			return e.NotFoundError("Agent not found.", err)
		}
		return err
	}

	tools, err := core.DiscoverAgentTools(e.Request.Context(), e.App, agentId)
	if err != nil {
		return e.BadRequestError(err.Error(), err)
	}
	return e.JSON(http.StatusOK, map[string]any{
		"agentId": agentId,
		"tools":   tools,
	})
}

func aiRequestApproval(e *core.RequestEvent) error {
	agentId := e.Request.PathValue("id")
	if _, err := checkAgentCallerAccess(e, agentId); err != nil {
		if errors.Is(err, core.ErrAgentNotFound) {
			return e.NotFoundError("Agent not found.", err)
		}
		return err
	}

	var body struct {
		ToolName       string         `json:"toolName"`
		Classification string         `json:"classification"`
		TargetResource string         `json:"targetResource"`
		Payload        map[string]any `json:"payload"`
	}
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("Invalid approval request payload.", err)
	}

	req, err := core.RequestToolApproval(e.Request.Context(), e.App, agentId, body.ToolName, body.Classification, body.TargetResource, body.Payload)
	if err != nil {
		return e.BadRequestError("Failed to create approval request.", err)
	}
	return e.JSON(http.StatusCreated, req)
}

func aiDecideApproval(e *core.RequestEvent) error {
	approvalId := e.Request.PathValue("id")
	var body struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("Invalid approval decision.", err)
	}

	var approver string
	if e.Auth != nil {
		approver = e.Auth.GetString("email")
	}

	req, err := core.DecideToolApproval(e.Request.Context(), e.App, approvalId, body.Status, approver, body.Reason)
	if err != nil {
		return e.BadRequestError(err.Error(), err)
	}
	return e.JSON(http.StatusOK, req)
}

func aiInvokeTool(e *core.RequestEvent) error {
	agentId := e.Request.PathValue("id")
	if _, err := checkAgentCallerAccess(e, agentId); err != nil {
		if errors.Is(err, core.ErrAgentNotFound) {
			return e.NotFoundError("Agent not found.", err)
		}
		return err
	}

	var body struct {
		ToolName         string         `json:"toolName"`
		TargetCollection string         `json:"targetCollection"`
		Classification   string         `json:"classification"`
		RequiredScope    string         `json:"requiredScope"`
		Payload          map[string]any `json:"payload"`
		ApprovalId       string         `json:"approvalId"`
	}
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("Invalid tool invocation payload.", err)
	}

	err := core.EvaluateAgentToolCall(
		e.Request.Context(),
		e.App,
		agentId,
		body.ToolName,
		body.TargetCollection,
		body.Classification,
		body.RequiredScope,
		body.Payload,
		body.ApprovalId,
	)
	if err != nil {
		return e.ForbiddenError(err.Error(), err)
	}
	return e.JSON(http.StatusOK, map[string]any{
		"allowed":  true,
		"agentId":  agentId,
		"toolName": body.ToolName,
	})
}

func aiListAuditLogs(e *core.RequestEvent) error {
	limit, _ := strconv.Atoi(e.Request.URL.Query().Get("limit"))
	logs, err := core.ListAgentAuditLogs(e.Request.Context(), e.App, limit)
	if err != nil {
		return e.InternalServerError("Failed to retrieve AI audit logs.", err)
	}
	return e.JSON(http.StatusOK, map[string]any{
		"count": len(logs),
		"items": logs,
	})
}

func aiIndexKnowledge(e *core.RequestEvent) error {
	var body struct {
		Namespace string         `json:"namespace"`
		Title     string         `json:"title"`
		Content   string         `json:"content"`
		Metadata  map[string]any `json:"metadata"`
	}
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("Invalid knowledge chunk payload.", err)
	}

	ownerId := ""
	if e.Auth != nil {
		ownerId = e.Auth.Id
	}

	chunk, err := core.IndexKnowledgeChunk(e.Request.Context(), e.App, &core.KnowledgeChunk{
		Namespace: body.Namespace,
		OwnerId:   ownerId,
		Title:     body.Title,
		Content:   body.Content,
		Metadata:  body.Metadata,
	})
	if err != nil {
		return e.BadRequestError("Failed to index knowledge chunk.", err)
	}
	return e.JSON(http.StatusCreated, chunk)
}

func aiSearchKnowledge(e *core.RequestEvent) error {
	ownerId := ""
	if e.Auth != nil {
		ownerId = e.Auth.Id
	}
	ns := e.Request.URL.Query().Get("namespace")
	q := e.Request.URL.Query().Get("q")
	limit, _ := strconv.Atoi(e.Request.URL.Query().Get("limit"))

	chunks, err := core.SearchKnowledgeChunks(e.Request.Context(), e.App, ownerId, ns, q, limit)
	if err != nil {
		return e.BadRequestError(err.Error(), err)
	}
	return e.JSON(http.StatusOK, map[string]any{
		"count": len(chunks),
		"items": chunks,
	})
}

func pluginsList(e *core.RequestEvent) error {
	plugins, err := core.ListPlugins(e.Request.Context(), e.App)
	if err != nil {
		return e.InternalServerError("Failed to list plugins.", err)
	}
	return e.JSON(http.StatusOK, map[string]any{
		"count": len(plugins),
		"items": plugins,
	})
}

func pluginsLoad(e *core.RequestEvent) error {
	var body struct {
		Name           string   `json:"name"`
		Version        string   `json:"version"`
		MinCoreVersion string   `json:"minCoreVersion"`
		Permissions    []string `json:"permissions"`
	}
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("Invalid plugin load payload.", err)
	}

	spec, err := core.LoadPlugin(e.Request.Context(), e.App, &core.PluginSpec{
		Name:           body.Name,
		Version:        body.Version,
		MinCoreVersion: body.MinCoreVersion,
		Permissions:    body.Permissions,
	})
	if err != nil {
		return e.BadRequestError(err.Error(), err)
	}
	return e.JSON(http.StatusCreated, spec)
}

func pluginsUpgrade(e *core.RequestEvent) error {
	name := e.Request.PathValue("name")
	var body struct {
		Version        string   `json:"version"`
		MinCoreVersion string   `json:"minCoreVersion"`
		Permissions    []string `json:"permissions"`
	}
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("Invalid plugin upgrade payload.", err)
	}

	spec, err := core.UpgradePlugin(e.Request.Context(), e.App, &core.PluginSpec{
		Name:           name,
		Version:        body.Version,
		MinCoreVersion: body.MinCoreVersion,
		Permissions:    body.Permissions,
	})
	if err != nil {
		return e.BadRequestError(err.Error(), err)
	}
	return e.JSON(http.StatusOK, spec)
}

func pluginsUnload(e *core.RequestEvent) error {
	name := e.Request.PathValue("name")
	if err := core.UnloadPlugin(e.Request.Context(), e.App, name); err != nil {
		return e.BadRequestError(err.Error(), err)
	}
	return e.JSON(http.StatusOK, map[string]any{
		"unloaded": true,
		"name":     name,
	})
}
