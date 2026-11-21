package e2e_test

import (
	"context"
	"errors"
	"testing"

	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/tests"
	"github.com/AbdullahZaher/ruxbase/tools/types"
	"github.com/pocketbase/dbx"
	"github.com/spf13/cast"
)

func TestFunctions_IsolatedExecutionAndSecretsBoundary(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	// 1. Create a sandboxed JS compute function with declared custom secret
	fn := &core.Function{
		Id:         "fn_tax_calculator",
		Name:       "calculate_tax",
		Entrypoint: "main",
		Runtime:    "js",
		TimeoutSec: 5,
		EnvVars: types.JSONMap[string]{
			"TAX_API_SECRET": "secret_vault_token_999",
		},
		Code: `
			function main(payload) {
				// Access injected secrets boundary
				var secret = env.TAX_API_SECRET;
				if (!secret) {
					throw new Error("Missing TAX_API_SECRET in sandbox");
				}

				// Verify host environment variables are strictly NOT leaked
				if (typeof process !== "undefined" && process.env) {
					throw new Error("Security breach: process.env exposed to untrusted code");
				}

				var amount = payload.amount || 0;
				var rate = payload.rate || 0.15;
				var tax = amount * rate;
				return {
					amount: amount,
					tax: tax,
					total: amount + tax,
					authorizedBy: secret
				};
			}
		`,
	}

	createdFn, err := app.Functions().CreateFunction(fn)
	if err != nil {
		t.Fatalf("Failed to create function: %v", err)
	}
	if createdFn.Name != "calculate_tax" {
		t.Fatalf("Expected function name calculate_tax, got %s", createdFn.Name)
	}

	// 2. Invoke the function
	result, err := app.Functions().Invoke(context.Background(), "calculate_tax", map[string]any{
		"amount": 200.0,
		"rate":   0.20,
	})
	if err != nil {
		t.Fatalf("Function invocation failed: %v", err)
	}

	// Verify returned computations
	if cast.ToFloat64(result["total"]) != 240.0 || cast.ToFloat64(result["tax"]) != 40.0 {
		t.Fatalf("Unexpected function calculation: %v", result)
	}
	if result["authorizedBy"] != "secret_vault_token_999" {
		t.Fatalf("Sandbox failed to access isolated env secrets: %v", result)
	}

	// 3. Verify execution telemetry recorded in PostgreSQL audit logs
	var count int
	_ = app.DB().NewQuery("SELECT count(*) FROM {{_function_invocations}} WHERE [[function_id]] = {:id} AND [[status]] = 'success'").
		Bind(dbx.Params{"id": createdFn.Id}).Row(&count)

	if count != 1 {
		t.Fatalf("Expected 1 recorded invocation in audit log, got %d", count)
	}
}

func TestFunctions_ExecutionTimeoutAndMaliciousLoopContainment(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	// Create function with short 1-second timeout containing an intentional infinite loop
	maliciousFn := &core.Function{
		Id:         "fn_infinite_loop",
		Name:       "runaway_loop",
		Entrypoint: "main",
		Runtime:    "js",
		TimeoutSec: 1, // 1-second execution deadline
		Code: `
			function main(payload) {
				// Malicious or accidental infinite loop
				var i = 0;
				while (true) {
					i++;
				}
				return { count: i };
			}
		`,
	}

	_, err = app.Functions().CreateFunction(maliciousFn)
	if err != nil {
		t.Fatalf("Failed to register function: %v", err)
	}

	// Invoke -> must be interrupted by timeout deadline without hanging test execution
	_, err = app.Functions().Invoke(context.Background(), "runaway_loop", nil)
	if err == nil {
		t.Fatal("Expected execution timeout error from infinite loop")
	}
	if !errors.Is(err, core.ErrFunctionTimeout) {
		t.Fatalf("Expected ErrFunctionTimeout, got: %v", err)
	}

	// Verify failure recorded in audit log
	var status string
	_ = app.DB().NewQuery("SELECT [[status]] FROM {{_function_invocations}} WHERE [[function_id]] = 'fn_infinite_loop' ORDER BY [[created]] DESC LIMIT 1").
		Row(&status)

	if status != "timeout" {
		t.Fatalf("Expected audit invocation status timeout, got: %s", status)
	}
}
