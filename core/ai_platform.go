package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/AbdullahZaher/ruxbase/tools/security"
	"github.com/AbdullahZaher/ruxbase/tools/types"
	"github.com/pocketbase/dbx"
)

const (
	ToolClassRead        = "read"
	ToolClassWrite       = "write"
	ToolClassSensitive   = "sensitive"
	ToolClassDestructive = "destructive"
)

var (
	ErrAgentNotFound         = errors.New("ai agent not found")
	ErrAgentSuspended        = errors.New("ai agent is suspended")
	ErrAgentScopeDenied      = errors.New("ai agent permission denied: outside allowed scope or collections")
	ErrAgentPromptInjection  = errors.New("ai security error: unsafe injection or privileged system target detected")
	ErrAgentApprovalRequired = errors.New("human approval required for sensitive or destructive agent action")
	ErrAgentApprovalInvalid  = errors.New("invalid, expired, mismatched, or already consumed tool approval")
)

// AgentIdentity represents a scoped, least-privilege AI agent identity.
type AgentIdentity struct {
	Id                 string                  `db:"id" json:"id"`
	Name               string                  `db:"name" json:"name"`
	OwnerId            string                  `db:"owner_id" json:"ownerId"`
	Scopes             types.JSONArray[string] `db:"scopes" json:"scopes"`
	AllowedCollections types.JSONArray[string] `db:"allowed_collections" json:"allowedCollections"`
	Status             string                  `db:"status" json:"status"`
	Created            types.DateTime          `db:"created" json:"created"`
	Updated            types.DateTime          `db:"updated" json:"updated"`
}

// AgentToolDescriptor describes an authorized collection/tool exposed to an agent.
type AgentToolDescriptor struct {
	ToolName       string `json:"toolName"`
	Collection     string `json:"collection"`
	Classification string `json:"classification"`
	RequiredScope  string `json:"requiredScope"`
	NeedsApproval  bool   `json:"needsApproval"`
}

// ToolApprovalRequest tracks human-in-the-loop approval for sensitive agent operations.
type ToolApprovalRequest struct {
	Id             string             `db:"id" json:"id"`
	AgentId        string             `db:"agent_id" json:"agentId"`
	ToolName       string             `db:"tool_name" json:"toolName"`
	Classification string             `db:"classification" json:"classification"`
	TargetResource string             `db:"target_resource" json:"targetResource"`
	Payload        types.JSONMap[any] `db:"payload" json:"payload,omitempty"`
	Status         string             `db:"status" json:"status"`
	ApprovedBy     string             `db:"approved_by" json:"approvedBy"`
	DecisionReason string             `db:"decision_reason" json:"decisionReason"`
	Created        types.DateTime     `db:"created" json:"created"`
	Updated        types.DateTime     `db:"updated" json:"updated"`
}

// AgentAuditDecision records every AI tool permission evaluation.
type AgentAuditDecision struct {
	Id             string         `db:"id" json:"id"`
	AgentId        string         `db:"agent_id" json:"agentId"`
	OwnerId        string         `db:"owner_id" json:"ownerId"`
	ToolName       string         `db:"tool_name" json:"toolName"`
	Classification string         `db:"classification" json:"classification"`
	TargetResource string         `db:"target_resource" json:"targetResource"`
	Decision       string         `db:"decision" json:"decision"`
	Reason         string         `db:"reason" json:"reason"`
	Created        types.DateTime `db:"created" json:"created"`
}

// KnowledgeChunk stores an owner-scoped RAG/knowledge document fragment.
type KnowledgeChunk struct {
	Id        string             `db:"id" json:"id"`
	Namespace string             `db:"namespace" json:"namespace"`
	OwnerId   string             `db:"owner_id" json:"ownerId"`
	Title     string             `db:"title" json:"title"`
	Content   string             `db:"content" json:"content"`
	Metadata  types.JSONMap[any] `db:"metadata" json:"metadata,omitempty"`
	Created   types.DateTime     `db:"created" json:"created"`
	Updated   types.DateTime     `db:"updated" json:"updated"`
}

// RegisterAgent persists a new scoped AI agent identity.
func RegisterAgent(ctx context.Context, app App, agent *AgentIdentity) (*AgentIdentity, error) {
	if agent == nil || strings.TrimSpace(agent.Name) == "" {
		return nil, errors.New("agent name is required")
	}
	if agent.Id == "" {
		agent.Id = security.RandomStringWithAlphabet(15, "abcdefghijklmnopqrstuvwxyz0123456789")
	}
	if agent.Status == "" {
		agent.Status = "active"
	}
	now := types.NowDateTime()
	agent.Created = now
	agent.Updated = now

	scopesBytes, _ := json.Marshal(agent.Scopes)
	colsBytes, _ := json.Marshal(agent.AllowedCollections)

	_, err := app.DB().NewQuery(`
		INSERT INTO {{_ai_agents}} (
			[[id]], [[name]], [[owner_id]], [[scopes]], [[allowed_collections]], [[status]], [[created]], [[updated]]
		) VALUES (
			{:id}, {:name}, {:ownerId}, {:scopes}::jsonb, {:cols}::jsonb, {:status}, {:created}, {:updated}
		)
	`).WithContext(ctx).Bind(dbx.Params{
		"id":      agent.Id,
		"name":    agent.Name,
		"ownerId": agent.OwnerId,
		"scopes":  string(scopesBytes),
		"cols":    string(colsBytes),
		"status":  agent.Status,
		"created": agent.Created.String(),
		"updated": agent.Updated.String(),
	}).Execute()

	if err != nil {
		return nil, fmt.Errorf("failed to register ai agent: %w", err)
	}
	return agent, nil
}

// GetAgent retrieves an AI agent identity by ID or name.
func GetAgent(ctx context.Context, app App, idOrName string) (*AgentIdentity, error) {
	agent := &AgentIdentity{}
	err := app.DB().NewQuery(`
		SELECT [[id]], [[name]], [[owner_id]], [[scopes]], [[allowed_collections]], [[status]], [[created]], [[updated]]
		FROM {{_ai_agents}}
		WHERE [[id]] = {:val} OR [[name]] = {:val}
		LIMIT 1
	`).WithContext(ctx).Bind(dbx.Params{"val": idOrName}).One(agent)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrAgentNotFound
		}
		return nil, err
	}
	return agent, nil
}

// DiscoverAgentTools returns only the collections and tools permitted for the agent's
// scopes and collection allowlist, strictly excluding privileged internal system collections.
func DiscoverAgentTools(ctx context.Context, app App, agentId string) ([]AgentToolDescriptor, error) {
	agent, err := GetAgent(ctx, app, agentId)
	if err != nil {
		return nil, err
	}
	if agent.Status != "active" {
		return nil, ErrAgentSuspended
	}

	cols, err := app.FindAllCollections()
	if err != nil {
		return nil, err
	}

	var tools []AgentToolDescriptor
	for _, col := range cols {
		// Never expose privileged internal auth/system collections in agent tool discovery
		if col.System || strings.HasPrefix(col.Name, "_") {
			continue
		}
		if len(agent.AllowedCollections) > 0 && !slices.Contains(agent.AllowedCollections, col.Name) {
			continue
		}

		if slices.Contains(agent.Scopes, "collections:read") {
			tools = append(tools, AgentToolDescriptor{
				ToolName:       "read_" + col.Name,
				Collection:     col.Name,
				Classification: ToolClassRead,
				RequiredScope:  "collections:read",
				NeedsApproval:  false,
			})
		}
		if slices.Contains(agent.Scopes, "collections:write") {
			tools = append(tools, AgentToolDescriptor{
				ToolName:       "write_" + col.Name,
				Collection:     col.Name,
				Classification: ToolClassWrite,
				RequiredScope:  "collections:write",
				NeedsApproval:  false,
			})
		}
		if slices.Contains(agent.Scopes, "collections:delete") {
			tools = append(tools, AgentToolDescriptor{
				ToolName:       "delete_" + col.Name,
				Collection:     col.Name,
				Classification: ToolClassDestructive,
				RequiredScope:  "collections:delete",
				NeedsApproval:  true,
			})
		}
	}

	if tools == nil {
		tools = []AgentToolDescriptor{}
	}
	return tools, nil
}

func recordAgentDecision(ctx context.Context, app App, agentId, ownerId, toolName, class, target, decision, reason string) {
	_, _ = app.DB().NewQuery(`
		INSERT INTO {{_ai_audit_logs}} (
			[[id]], [[agent_id]], [[owner_id]], [[tool_name]], [[classification]], [[target_resource]], [[decision]], [[reason]], [[created]]
		) VALUES (
			{:id}, {:agentId}, {:ownerId}, {:tool}, {:class}, {:target}, {:decision}, {:reason}, {:created}
		)
	`).WithContext(ctx).Bind(dbx.Params{
		"id":       security.RandomStringWithAlphabet(15, "abcdefghijklmnopqrstuvwxyz0123456789"),
		"agentId":  agentId,
		"ownerId":  ownerId,
		"tool":     toolName,
		"class":    class,
		"target":   target,
		"decision": decision,
		"reason":   reason,
		"created":  types.NowDateTime().String(),
	}).Execute()
}

func containsUnsafePromptInjection(targetResource string, payload map[string]any) bool {
	if strings.HasPrefix(targetResource, "_") || strings.Contains(targetResource, "*") {
		return true
	}

	rawBytes, _ := json.Marshal(payload)
	upper := strings.ToUpper(targetResource + " " + string(rawBytes))

	forbiddenTokens := []string{
		"_SUPERUSERS",
		"PG_ROLES",
		"PG_SHADOW",
		"DROP TABLE",
		"ALTER ROLE",
		"BYPASSRLS",
		"RUXBASE_ENCRYPTION_KEY",
		"DATABASE_URL",
	}
	for _, tok := range forbiddenTokens {
		if strings.Contains(upper, tok) {
			return true
		}
	}
	return false
}

// RequestToolApproval creates a pending human approval record for sensitive/destructive agent tools.
func RequestToolApproval(ctx context.Context, app App, agentId, toolName, class, targetResource string, payload map[string]any) (*ToolApprovalRequest, error) {
	req := &ToolApprovalRequest{
		Id:             security.RandomStringWithAlphabet(15, "abcdefghijklmnopqrstuvwxyz0123456789"),
		AgentId:        agentId,
		ToolName:       toolName,
		Classification: class,
		TargetResource: targetResource,
		Payload:        payload,
		Status:         "pending",
		Created:        types.NowDateTime(),
		Updated:        types.NowDateTime(),
	}
	payloadBytes, _ := json.Marshal(payload)

	_, err := app.DB().NewQuery(`
		INSERT INTO {{_ai_tool_approvals}} (
			[[id]], [[agent_id]], [[tool_name]], [[classification]], [[target_resource]], [[payload]], [[status]], [[created]], [[updated]]
		) VALUES (
			{:id}, {:agentId}, {:tool}, {:class}, {:target}, {:payload}::jsonb, {:status}, {:created}, {:updated}
		)
	`).WithContext(ctx).Bind(dbx.Params{
		"id":      req.Id,
		"agentId": req.AgentId,
		"tool":    req.ToolName,
		"class":   req.Classification,
		"target":  req.TargetResource,
		"payload": string(payloadBytes),
		"status":  req.Status,
		"created": req.Created.String(),
		"updated": req.Updated.String(),
	}).Execute()

	return req, err
}

// DecideToolApproval marks a pending approval request as approved or rejected by an authorized human operator.
func DecideToolApproval(ctx context.Context, app App, approvalId, status, approvedBy, reason string) (*ToolApprovalRequest, error) {
	if status != "approved" && status != "rejected" {
		return nil, errors.New("approval decision status must be 'approved' or 'rejected'")
	}

	req := &ToolApprovalRequest{}
	err := app.DB().NewQuery(`
		UPDATE {{_ai_tool_approvals}}
		SET [[status]] = {:status}, [[approved_by]] = {:by}, [[decision_reason]] = {:reason}, [[updated]] = now()
		WHERE [[id]] = {:id} AND [[status]] = 'pending'
		RETURNING [[id]], [[agent_id]], [[tool_name]], [[classification]], [[target_resource]], [[payload]], [[status]], [[approved_by]], [[decision_reason]], [[created]], [[updated]]
	`).WithContext(ctx).Bind(dbx.Params{
		"id":     approvalId,
		"status": status,
		"by":     approvedBy,
		"reason": reason,
	}).One(req)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrAgentApprovalInvalid
		}
		return nil, err
	}
	return req, nil
}

// EvaluateAgentToolCall checks agent identity, prompt-injection safety, collection/scope policies,
// and single-use human approval for sensitive or destructive actions, auditing every outcome.
func EvaluateAgentToolCall(
	ctx context.Context,
	app App,
	agentId string,
	toolName string,
	targetCollection string,
	classification string,
	requiredScope string,
	payload map[string]any,
	approvalId string,
) error {
	agent, err := GetAgent(ctx, app, agentId)
	if err != nil {
		recordAgentDecision(ctx, app, agentId, "", toolName, classification, targetCollection, "deny", err.Error())
		return err
	}
	if agent.Status != "active" {
		recordAgentDecision(ctx, app, agent.Id, agent.OwnerId, toolName, classification, targetCollection, "deny", ErrAgentSuspended.Error())
		return ErrAgentSuspended
	}

	// 1. Prompt injection & privileged system target guard
	if containsUnsafePromptInjection(targetCollection, payload) {
		recordAgentDecision(ctx, app, agent.Id, agent.OwnerId, toolName, classification, targetCollection, "deny", ErrAgentPromptInjection.Error())
		return ErrAgentPromptInjection
	}

	// 2. Scope & collection allowlist enforcement
	if requiredScope != "" && !slices.Contains(agent.Scopes, requiredScope) {
		recordAgentDecision(ctx, app, agent.Id, agent.OwnerId, toolName, classification, targetCollection, "deny", "missing required scope: "+requiredScope)
		return ErrAgentScopeDenied
	}
	if targetCollection != "" && len(agent.AllowedCollections) > 0 && !slices.Contains(agent.AllowedCollections, targetCollection) {
		recordAgentDecision(ctx, app, agent.Id, agent.OwnerId, toolName, classification, targetCollection, "deny", "collection outside agent allowlist: "+targetCollection)
		return ErrAgentScopeDenied
	}

	// 3. Human approval enforcement for sensitive & destructive actions (single-use atomic consumption)
	if classification == ToolClassSensitive || classification == ToolClassDestructive {
		if approvalId == "" {
			recordAgentDecision(ctx, app, agent.Id, agent.OwnerId, toolName, classification, targetCollection, "require_approval", ErrAgentApprovalRequired.Error())
			return ErrAgentApprovalRequired
		}

		res, err := app.DB().NewQuery(`
			UPDATE {{_ai_tool_approvals}}
			SET [[status]] = 'executed', [[updated]] = now()
			WHERE [[id]] = {:id}
			  AND [[agent_id]] = {:agentId}
			  AND [[tool_name]] = {:tool}
			  AND [[target_resource]] = {:target}
			  AND [[status]] = 'approved'
		`).WithContext(ctx).Bind(dbx.Params{
			"id":      approvalId,
			"agentId": agent.Id,
			"tool":    toolName,
			"target":  targetCollection,
		}).Execute()

		if err != nil {
			recordAgentDecision(ctx, app, agent.Id, agent.OwnerId, toolName, classification, targetCollection, "deny", err.Error())
			return err
		}
		affected, _ := res.RowsAffected()
		if affected != 1 {
			recordAgentDecision(ctx, app, agent.Id, agent.OwnerId, toolName, classification, targetCollection, "deny", ErrAgentApprovalInvalid.Error())
			return ErrAgentApprovalInvalid
		}
	}

	recordAgentDecision(ctx, app, agent.Id, agent.OwnerId, toolName, classification, targetCollection, "allow", "policy satisfied")
	return nil
}

// ListAgentAuditLogs lists recent AI agent permission decisions.
func ListAgentAuditLogs(ctx context.Context, app App, limit int) ([]*AgentAuditDecision, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var logs []*AgentAuditDecision
	err := app.DB().NewQuery(`
		SELECT [[id]], [[agent_id]], [[owner_id]], [[tool_name]], [[classification]], [[target_resource]], [[decision]], [[reason]], [[created]]
		FROM {{_ai_audit_logs}}
		ORDER BY [[created]] DESC
		LIMIT {:limit}
	`).WithContext(ctx).Bind(dbx.Params{"limit": limit}).All(&logs)
	if logs == nil {
		logs = []*AgentAuditDecision{}
	}
	return logs, err
}

// IndexKnowledgeChunk inserts an owner-scoped RAG/knowledge fragment.
func IndexKnowledgeChunk(ctx context.Context, app App, chunk *KnowledgeChunk) (*KnowledgeChunk, error) {
	if chunk == nil || strings.TrimSpace(chunk.Content) == "" {
		return nil, errors.New("knowledge chunk content is required")
	}
	if chunk.OwnerId == "" {
		return nil, errors.New("owner_id is required for tenant-isolated knowledge")
	}
	if chunk.Namespace == "" {
		chunk.Namespace = "default"
	}
	if chunk.Id == "" {
		chunk.Id = security.RandomStringWithAlphabet(15, "abcdefghijklmnopqrstuvwxyz0123456789")
	}
	chunk.Created = types.NowDateTime()
	chunk.Updated = chunk.Created

	metaBytes, _ := json.Marshal(chunk.Metadata)
	_, err := app.DB().NewQuery(`
		INSERT INTO {{_ai_knowledge_chunks}} ([[id]], [[namespace]], [[owner_id]], [[title]], [[content]], [[metadata]], [[created]], [[updated]])
		VALUES ({:id}, {:ns}, {:owner}, {:title}, {:content}, {:meta}::jsonb, {:created}, {:updated})
	`).WithContext(ctx).Bind(dbx.Params{
		"id":      chunk.Id,
		"ns":      chunk.Namespace,
		"owner":   chunk.OwnerId,
		"title":   chunk.Title,
		"content": chunk.Content,
		"meta":    string(metaBytes),
		"created": chunk.Created.String(),
		"updated": chunk.Updated.String(),
	}).Execute()
	if err != nil {
		return nil, err
	}
	return chunk, nil
}

// SearchKnowledgeChunks queries knowledge chunks strictly filtered by ownerId to enforce tenant isolation.
func SearchKnowledgeChunks(ctx context.Context, app App, ownerId, namespace, query string, limit int) ([]*KnowledgeChunk, error) {
	if ownerId == "" {
		return nil, errors.New("ownerId is strictly required for knowledge search")
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	sqlStr := `
		SELECT [[id]], [[namespace]], [[owner_id]], [[title]], [[content]], [[metadata]], [[created]], [[updated]]
		FROM {{_ai_knowledge_chunks}}
		WHERE [[owner_id]] = {:owner}
	`
	params := dbx.Params{"owner": ownerId, "limit": limit}
	if namespace != "" {
		sqlStr += " AND [[namespace]] = {:ns}"
		params["ns"] = namespace
	}
	if query != "" {
		sqlStr += " AND ([[content]] ILIKE {:q} OR [[title]] ILIKE {:q})"
		params["q"] = "%" + query + "%"
	}
	sqlStr += " ORDER BY [[created]] DESC LIMIT {:limit}"

	var chunks []*KnowledgeChunk
	err := app.DB().NewQuery(sqlStr).WithContext(ctx).Bind(params).All(&chunks)
	if chunks == nil {
		chunks = []*KnowledgeChunk{}
	}
	return chunks, err
}
