package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/AbdullahZaher/ruxbase/tools/security"
	"github.com/AbdullahZaher/ruxbase/tools/types"
	"github.com/dop251/goja"
	"github.com/pocketbase/dbx"
)

var (
	ErrFunctionNotFound       = errors.New("function not found")
	ErrFunctionExecutionError = errors.New("function execution error")
	ErrFunctionTimeout        = errors.New("function execution timeout exceeded")
)

// Function represents a deployed serverless compute function.
type Function struct {
	Id         string                `db:"id" json:"id"`
	Name       string                `db:"name" json:"name"`
	Entrypoint string                `db:"entrypoint" json:"entrypoint"`
	Runtime    string                `db:"runtime" json:"runtime"`
	Code       string                `db:"code" json:"code"`
	EnvVars    types.JSONMap[string] `db:"env_vars" json:"envVars,omitempty"`
	TimeoutSec int                   `db:"timeout_sec" json:"timeoutSec"`
	Status     string                `db:"status" json:"status"`
	Version    int                   `db:"version" json:"version"`
	Created    types.DateTime        `db:"created" json:"created"`
	Updated    types.DateTime        `db:"updated" json:"updated"`
}

// FunctionInvocation records the execution telemetry and audit trail.
type FunctionInvocation struct {
	Id         string         `db:"id" json:"id"`
	FunctionId string         `db:"function_id" json:"functionId"`
	Status     string         `db:"status" json:"status"`
	DurationMs int            `db:"duration_ms" json:"durationMs"`
	Error      string         `db:"error" json:"error"`
	Created    types.DateTime `db:"created" json:"created"`
}

// FunctionManager manages isolated compute functions and runs them within a strict sandbox.
type FunctionManager struct {
	app App
	mu  sync.RWMutex
}

// NewFunctionManager creates a new FunctionManager instance.
func NewFunctionManager(app App) *FunctionManager {
	return &FunctionManager{app: app}
}

// CreateFunction registers a new isolated function definition.
func (fm *FunctionManager) CreateFunction(fn *Function) (*Function, error) {
	if fn == nil {
		return nil, errors.New("cannot create nil function")
	}
	if fn.Name == "" {
		return nil, errors.New("function name is required")
	}
	if fn.Code == "" {
		return nil, errors.New("function code is required")
	}
	if fn.Entrypoint == "" {
		fn.Entrypoint = "main"
	}
	if fn.Runtime == "" {
		fn.Runtime = "js"
	}
	if fn.TimeoutSec <= 0 {
		fn.TimeoutSec = 10
	}
	if fn.Status == "" {
		fn.Status = "active"
	}
	if fn.Version <= 0 {
		fn.Version = 1
	}
	if fn.Id == "" {
		fn.Id = security.RandomStringWithAlphabet(15, "abcdefghijklmnopqrstuvwxyz0123456789")
	}
	now := types.NowDateTime()
	fn.Created = now
	fn.Updated = now

	envBytes, _ := json.Marshal(fn.EnvVars)

	_, err := fm.app.DB().NewQuery(`
		INSERT INTO {{_functions}} (
			[[id]], [[name]], [[entrypoint]], [[runtime]], [[code]],
			[[env_vars]], [[timeout_sec]], [[status]], [[version]], [[created]], [[updated]]
		) VALUES (
			{:id}, {:name}, {:entrypoint}, {:runtime}, {:code},
			{:env}::jsonb, {:timeout}, {:status}, {:version}, {:created}, {:updated}
		)
	`).Bind(dbx.Params{
		"id":         fn.Id,
		"name":       fn.Name,
		"entrypoint": fn.Entrypoint,
		"runtime":    fn.Runtime,
		"code":       fn.Code,
		"env":        string(envBytes),
		"timeout":    fn.TimeoutSec,
		"status":     fn.Status,
		"version":    fn.Version,
		"created":    fn.Created.String(),
		"updated":    fn.Updated.String(),
	}).Execute()

	if err != nil {
		return nil, fmt.Errorf("failed to create function: %w", err)
	}

	return fn, nil
}

// GetFunction retrieves a function by name or ID.
func (fm *FunctionManager) GetFunction(nameOrId string) (*Function, error) {
	fn := &Function{}
	err := fm.app.DB().NewQuery(`
		SELECT [[id]], [[name]], [[entrypoint]], [[runtime]], [[code]],
		       [[env_vars]], [[timeout_sec]], [[status]], [[version]], [[created]], [[updated]]
		FROM {{_functions}}
		WHERE [[id]] = {:val} OR [[name]] = {:val}
		LIMIT 1
	`).Bind(dbx.Params{"val": nameOrId}).One(fn)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrFunctionNotFound
		}
		return nil, err
	}

	return fn, nil
}

// DeleteFunction removes a function by name or ID.
func (fm *FunctionManager) DeleteFunction(nameOrId string) error {
	fn, err := fm.GetFunction(nameOrId)
	if err != nil {
		return err
	}

	_, err = fm.app.DB().NewQuery(`
		DELETE FROM {{_functions}} WHERE [[id]] = {:id}
	`).Bind(dbx.Params{"id": fn.Id}).Execute()

	return err
}

// Invoke executes a function inside an isolated, sandboxed Goja JavaScript runtime.
// Secrets Boundary: Host environment variables, superuser keys, and database connection strings
// are strictly stripped and never injected into the sandboxed VM. Only fn.EnvVars are visible.
func (fm *FunctionManager) Invoke(
	ctx context.Context,
	nameOrId string,
	payload map[string]any,
) (map[string]any, error) {
	fn, err := fm.GetFunction(nameOrId)
	if err != nil {
		return nil, err
	}

	start := time.Now()
	invocation := &FunctionInvocation{
		Id:         security.RandomStringWithAlphabet(15, "abcdefghijklmnopqrstuvwxyz0123456789"),
		FunctionId: fn.Id,
		Created:    types.NowDateTime(),
	}

	// 1. Establish invocation timeout deadline
	timeoutDuration := time.Duration(fn.TimeoutSec) * time.Second
	execCtx, cancel := context.WithTimeout(ctx, timeoutDuration)
	defer cancel()

	// 2. Instantiate isolated JS engine with zero host bindings
	vm := goja.New()

	// Interrupt mechanism for timeout enforcement
	go func() {
		<-execCtx.Done()
		if errors.Is(execCtx.Err(), context.DeadlineExceeded) {
			vm.Interrupt(ErrFunctionTimeout)
		}
	}()

	// 3. Inject ONLY user-configured EnvVars (Secrets Boundary Protection)
	envMap := make(map[string]string)
	for k, v := range fn.EnvVars {
		envMap[k] = v
	}
	_ = vm.Set("env", envMap)

	// 4. Inject payload
	_ = vm.Set("payload", payload)

	// 5. Execute function code
	_, compileErr := vm.RunString(fn.Code)
	if compileErr != nil {
		invocation.Status = "error"
		invocation.DurationMs = int(time.Since(start).Milliseconds())
		invocation.Error = compileErr.Error()
		fm.recordInvocation(invocation)
		return nil, fmt.Errorf("%w: %v", ErrFunctionExecutionError, compileErr)
	}

	// 6. Call entrypoint function
	entryVal := vm.Get(fn.Entrypoint)
	if entryVal == nil {
		err := fmt.Errorf("entrypoint function '%s' not defined", fn.Entrypoint)
		invocation.Status = "error"
		invocation.DurationMs = int(time.Since(start).Milliseconds())
		invocation.Error = err.Error()
		fm.recordInvocation(invocation)
		return nil, err
	}

	callable, ok := goja.AssertFunction(entryVal)
	if !ok {
		err := fmt.Errorf("entrypoint '%s' is not callable", fn.Entrypoint)
		invocation.Status = "error"
		invocation.DurationMs = int(time.Since(start).Milliseconds())
		invocation.Error = err.Error()
		fm.recordInvocation(invocation)
		return nil, err
	}

	resVal, callErr := callable(goja.Undefined(), vm.ToValue(payload))
	invocation.DurationMs = int(time.Since(start).Milliseconds())

	if callErr != nil {
		if errors.Is(execCtx.Err(), context.DeadlineExceeded) {
			invocation.Status = "timeout"
			invocation.Error = ErrFunctionTimeout.Error()
			fm.recordInvocation(invocation)
			return nil, ErrFunctionTimeout
		}
		invocation.Status = "error"
		invocation.Error = callErr.Error()
		fm.recordInvocation(invocation)
		return nil, fmt.Errorf("%w: %v", ErrFunctionExecutionError, callErr)
	}

	invocation.Status = "success"
	fm.recordInvocation(invocation)

	// Convert return value to map[string]any
	resExport := resVal.Export()
	if outMap, ok := resExport.(map[string]any); ok {
		return outMap, nil
	}

	return map[string]any{"result": resExport}, nil
}

func (fm *FunctionManager) recordInvocation(inv *FunctionInvocation) {
	_, _ = fm.app.DB().NewQuery(`
		INSERT INTO {{_function_invocations}} ([[id]], [[function_id]], [[status]], [[duration_ms]], [[error]], [[created]])
		VALUES ({:id}, {:funcId}, {:status}, {:duration}, {:err}, {:created})
	`).Bind(dbx.Params{
		"id":       inv.Id,
		"funcId":   inv.FunctionId,
		"status":   inv.Status,
		"duration": inv.DurationMs,
		"err":      inv.Error,
		"created":  inv.Created.String(),
	}).Execute()
}
