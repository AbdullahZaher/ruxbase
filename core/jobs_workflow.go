package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/AbdullahZaher/ruxbase/tools/security"
	"github.com/AbdullahZaher/ruxbase/tools/types"
	"github.com/pocketbase/dbx"
)

const (
	WorkflowStatusPending   = "pending"
	WorkflowStatusRunning   = "running"
	WorkflowStatusCompleted = "completed"
	WorkflowStatusFailed    = "failed"
)

// WorkflowStep defines a single execution step within a workflow DAG.
type WorkflowStep struct {
	Name      string             `json:"name"`
	Task      string             `json:"task"`
	Input     types.JSONMap[any] `json:"input,omitempty"`
	Condition string             `json:"condition,omitempty"`
	OnSuccess string             `json:"onSuccess,omitempty"`
	OnFailure string             `json:"onFailure,omitempty"`
}

// Workflow represents a multi-step durable business orchestration.
type Workflow struct {
	Id           string                        `db:"id" json:"id"`
	Name         string                        `db:"name" json:"name"`
	Status       string                        `db:"status" json:"status"`
	CurrentStep  int                           `db:"current_step" json:"currentStep"`
	Steps        types.JSONArray[WorkflowStep] `db:"steps" json:"steps"`
	Context      types.JSONMap[any]            `db:"context" json:"context"`
	ScheduleCron string                        `db:"schedule_cron" json:"scheduleCron,omitempty"`
	NextRun      types.DateTime                `db:"next_run" json:"nextRun,omitempty"`
	LastError    string                        `db:"last_error" json:"lastError,omitempty"`
	Created      types.DateTime                `db:"created" json:"created"`
	Updated      types.DateTime                `db:"updated" json:"updated"`
}

// WorkflowHandler executes a workflow step with access to the accumulated context.
type WorkflowHandler func(ctx context.Context, step *WorkflowStep, execContext map[string]any) (map[string]any, error)

var (
	workflowHandlersMu sync.RWMutex
	workflowHandlers   = make(map[string]WorkflowHandler)
)

// RegisterWorkflowHandler registers a handler for workflow steps executing a given task.
func (jm *JobsManager) RegisterWorkflowHandler(task string, handler WorkflowHandler) {
	workflowHandlersMu.Lock()
	defer workflowHandlersMu.Unlock()
	workflowHandlers[task] = handler
}

// CreateWorkflow inserts a new durable workflow definition and initial state into PostgreSQL.
func (jm *JobsManager) CreateWorkflow(wf *Workflow) (*Workflow, error) {
	if wf == nil {
		return nil, errors.New("cannot create nil workflow")
	}

	if wf.Id == "" {
		wf.Id = security.RandomStringWithAlphabet(15, "abcdefghijklmnopqrstuvwxyz0123456789")
	}
	if wf.Name == "" {
		return nil, errors.New("workflow name is required")
	}
	if len(wf.Steps) == 0 {
		return nil, errors.New("workflow must contain at least one step")
	}
	if wf.Status == "" {
		wf.Status = WorkflowStatusPending
	}
	if wf.Context == nil {
		wf.Context = make(types.JSONMap[any])
	}
	now := types.NowDateTime()
	wf.Created = now
	wf.Updated = now

	stepsBytes, _ := json.Marshal(wf.Steps)
	contextBytes, _ := json.Marshal(wf.Context)

	var nextRunVal any
	if !wf.NextRun.IsZero() {
		nextRunVal = wf.NextRun.String()
	}

	_, err := jm.app.DB().NewQuery(`
		INSERT INTO {{_workflows}} (
			[[id]], [[name]], [[status]], [[current_step]], [[steps]], [[context]],
			[[schedule_cron]], [[next_run]], [[last_error]], [[created]], [[updated]]
		) VALUES (
			{:id}, {:name}, {:status}, {:currentStep}, {:steps}::jsonb, {:context}::jsonb,
			{:cron}, {:nextRun}, {:lastError}, {:created}, {:updated}
		)
	`).Bind(dbx.Params{
		"id":          wf.Id,
		"name":        wf.Name,
		"status":      wf.Status,
		"currentStep": wf.CurrentStep,
		"steps":       string(stepsBytes),
		"context":     string(contextBytes),
		"cron":        wf.ScheduleCron,
		"nextRun":     nextRunVal,
		"lastError":   wf.LastError,
		"created":     wf.Created.String(),
		"updated":     wf.Updated.String(),
	}).Execute()

	if err != nil {
		return nil, fmt.Errorf("failed to create workflow: %w", err)
	}

	return wf, nil
}

// GetWorkflow fetches a workflow by its unique ID.
func (jm *JobsManager) GetWorkflow(workflowId string) (*Workflow, error) {
	wf := &Workflow{}
	err := jm.app.DB().NewQuery(`
		SELECT [[id]], [[name]], [[status]], [[current_step]], [[steps]], [[context]],
		       [[schedule_cron]], [[next_run]], [[last_error]], [[created]], [[updated]]
		FROM {{_workflows}}
		WHERE [[id]] = {:id}
	`).Bind(dbx.Params{"id": workflowId}).One(wf)

	if err != nil {
		return nil, err
	}
	return wf, nil
}

// ListWorkflows retrieves workflows matching the optional status filter.
func (jm *JobsManager) ListWorkflows(status string, limit int) ([]*Workflow, error) {
	if limit <= 0 {
		limit = 50
	}
	q := jm.app.DB().Select(
		"id", "name", "status", "current_step", "steps", "context",
		"schedule_cron", "next_run", "last_error", "created", "updated",
	).From("_workflows").Limit(int64(limit)).OrderBy("created DESC")

	if status != "" {
		q.AndWhere(dbx.HashExp{"status": status})
	}

	var workflows []*Workflow
	err := q.All(&workflows)
	return workflows, err
}

// ExecuteNextWorkflowStep executes the current step of a workflow, applying conditions,
// context merging, and branching logic. Returns true if more steps remain, false if completed or failed.
func (jm *JobsManager) ExecuteNextWorkflowStep(ctx context.Context, workflowId string) (bool, error) {
	wf, err := jm.GetWorkflow(workflowId)
	if err != nil {
		return false, err
	}

	if wf.Status == WorkflowStatusCompleted || wf.Status == WorkflowStatusFailed {
		return false, nil
	}

	if wf.CurrentStep >= len(wf.Steps) {
		// All steps executed -> mark completed
		_, err := jm.app.DB().NewQuery(`
			UPDATE {{_workflows}}
			SET [[status]] = {:status},
			    [[updated]] = now()
			WHERE [[id]] = {:id}
		`).Bind(dbx.Params{
			"id":     wf.Id,
			"status": WorkflowStatusCompleted,
		}).Execute()
		return false, err
	}

	step := wf.Steps[wf.CurrentStep]

	// 1. Evaluate condition if present
	if step.Condition != "" {
		if val, exists := wf.Context[step.Condition]; exists {
			// If condition evaluates to false / zero, skip step
			if bVal, ok := val.(bool); ok && !bVal {
				return jm.advanceWorkflowStep(wf, step.OnSuccess, nil)
			}
		}
	}

	// 2. Resolve workflow step handler
	workflowHandlersMu.RLock()
	handler, ok := workflowHandlers[step.Task]
	workflowHandlersMu.RUnlock()

	if !ok {
		// Fallback to standard job handler
		jm.mu.RLock()
		jobHandler, jobOk := jm.handlers[step.Task]
		jm.mu.RUnlock()

		if !jobOk {
			errMsg := fmt.Sprintf("no handler registered for workflow step task: %s", step.Task)
			_ = jm.failWorkflow(wf.Id, errMsg)
			return false, errors.New(errMsg)
		}

		// Adapt standard job handler
		job := &Job{
			Id:      wf.Id + "_step_" + step.Name,
			Queue:   "workflow",
			Task:    step.Task,
			Payload: wf.Context,
		}
		execErr := jobHandler(ctx, job)
		if execErr != nil {
			if step.OnFailure != "" {
				return jm.advanceWorkflowStep(wf, step.OnFailure, nil)
			}
			_ = jm.failWorkflow(wf.Id, execErr.Error())
			return false, execErr
		}
		return jm.advanceWorkflowStep(wf, step.OnSuccess, nil)
	}

	// 3. Execute step with WorkflowHandler
	output, err := handler(ctx, &step, wf.Context)
	if err != nil {
		if step.OnFailure != "" {
			return jm.advanceWorkflowStep(wf, step.OnFailure, output)
		}
		_ = jm.failWorkflow(wf.Id, err.Error())
		return false, err
	}

	return jm.advanceWorkflowStep(wf, step.OnSuccess, output)
}

func (jm *JobsManager) advanceWorkflowStep(wf *Workflow, nextStepName string, stepOutput map[string]any) (bool, error) {
	// Merge output into context
	if wf.Context == nil {
		wf.Context = make(types.JSONMap[any])
	}
	for k, v := range stepOutput {
		wf.Context[k] = v
	}

	nextIdx := wf.CurrentStep + 1
	if nextStepName != "" {
		found := false
		for idx, s := range wf.Steps {
			if s.Name == nextStepName {
				nextIdx = idx
				found = true
				break
			}
		}
		if !found {
			return false, fmt.Errorf("target branch step %s not found in workflow %s", nextStepName, wf.Id)
		}
	}

	status := WorkflowStatusRunning
	if nextIdx >= len(wf.Steps) {
		status = WorkflowStatusCompleted
	}

	contextBytes, _ := json.Marshal(wf.Context)

	_, err := jm.app.DB().NewQuery(`
		UPDATE {{_workflows}}
		SET [[status]] = {:status},
		    [[current_step]] = {:nextIdx},
		    [[context]] = {:context}::jsonb,
		    [[updated]] = now()
		WHERE [[id]] = {:id}
	`).Bind(dbx.Params{
		"id":      wf.Id,
		"status":  status,
		"nextIdx": nextIdx,
		"context": string(contextBytes),
	}).Execute()

	if err != nil {
		return false, err
	}

	return status != WorkflowStatusCompleted, nil
}

func (jm *JobsManager) failWorkflow(workflowId string, errMsg string) error {
	_, err := jm.app.DB().NewQuery(`
		UPDATE {{_workflows}}
		SET [[status]] = {:status},
		    [[last_error]] = {:err},
		    [[updated]] = now()
		WHERE [[id]] = {:id}
	`).Bind(dbx.Params{
		"id":     workflowId,
		"status": WorkflowStatusFailed,
		"err":    errMsg,
	}).Execute()
	return err
}

// RunWorkflow continuously drives workflow steps until completion or error.
func (jm *JobsManager) RunWorkflow(ctx context.Context, workflowId string) (*Workflow, error) {
	for {
		more, err := jm.ExecuteNextWorkflowStep(ctx, workflowId)
		if err != nil {
			return jm.GetWorkflow(workflowId)
		}
		if !more {
			break
		}
	}
	return jm.GetWorkflow(workflowId)
}
