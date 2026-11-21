package e2e_test

import (
	"context"
	"errors"
	"testing"

	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/tests"
	"github.com/AbdullahZaher/ruxbase/tools/types"
)

func TestDurableWorkflows_SequentialExecutionAndContextChaining(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	// 1. Register workflow step handlers
	app.Jobs().RegisterWorkflowHandler("create_order", func(ctx context.Context, step *core.WorkflowStep, currentContext map[string]any) (map[string]any, error) {
		return map[string]any{
			"orderId":   "ord_wf_101",
			"subtotal":  100.0,
			"userEmail": "customer@example.com",
		}, nil
	})

	app.Jobs().RegisterWorkflowHandler("calculate_tax", func(ctx context.Context, step *core.WorkflowStep, currentContext map[string]any) (map[string]any, error) {
		subtotal, ok := currentContext["subtotal"].(float64)
		if !ok {
			return nil, errors.New("subtotal not found in context")
		}
		tax := subtotal * 0.15
		total := subtotal + tax
		return map[string]any{
			"tax":   tax,
			"total": total,
		}, nil
	})

	app.Jobs().RegisterWorkflowHandler("capture_payment", func(ctx context.Context, step *core.WorkflowStep, currentContext map[string]any) (map[string]any, error) {
		total, ok := currentContext["total"].(float64)
		if !ok || total != 115.0 {
			return nil, errors.New("invalid total for payment capture")
		}
		return map[string]any{
			"paymentId": "pay_xyz_999",
			"status":    "paid",
		}, nil
	})

	// 2. Define sequential workflow
	wf := &core.Workflow{
		Id:   "wf_checkout_01",
		Name: "checkout_pipeline",
		Steps: types.JSONArray[core.WorkflowStep]{
			{Name: "step_order", Task: "create_order"},
			{Name: "step_tax", Task: "calculate_tax"},
			{Name: "step_pay", Task: "capture_payment"},
		},
	}

	createdWf, err := app.Jobs().CreateWorkflow(wf)
	if err != nil {
		t.Fatalf("Failed to create workflow: %v", err)
	}

	// 3. Drive workflow to completion
	completedWf, err := app.Jobs().RunWorkflow(context.Background(), createdWf.Id)
	if err != nil {
		t.Fatalf("Workflow execution failed: %v", err)
	}

	if completedWf.Status != core.WorkflowStatusCompleted {
		t.Fatalf("Expected workflow status completed, got: %s", completedWf.Status)
	}
	if completedWf.CurrentStep != 3 {
		t.Fatalf("Expected current_step = 3, got: %d", completedWf.CurrentStep)
	}

	// Verify context propagation across all steps
	if completedWf.Context["orderId"] != "ord_wf_101" {
		t.Fatalf("Context missing orderId: %v", completedWf.Context)
	}
	if completedWf.Context["tax"] != 15.0 || completedWf.Context["total"] != 115.0 {
		t.Fatalf("Context tax/total calculation incorrect: %v", completedWf.Context)
	}
	if completedWf.Context["paymentId"] != "pay_xyz_999" || completedWf.Context["status"] != "paid" {
		t.Fatalf("Context payment capture missing: %v", completedWf.Context)
	}
}

func TestDurableWorkflows_ConditionalBranchingAndSkipping(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	fraudCheckRan := false
	app.Jobs().RegisterWorkflowHandler("check_fraud", func(ctx context.Context, step *core.WorkflowStep, currentContext map[string]any) (map[string]any, error) {
		fraudCheckRan = true
		return map[string]any{"fraudScore": 0.05}, nil
	})

	app.Jobs().RegisterWorkflowHandler("fulfill_order", func(ctx context.Context, step *core.WorkflowStep, currentContext map[string]any) (map[string]any, error) {
		return map[string]any{"fulfilled": true}, nil
	})

	// Workflow where condition evaluates to false -> step is skipped
	wfSkipped := &core.Workflow{
		Id:   "wf_branch_skip_01",
		Name: "skip_branching_test",
		Context: types.JSONMap[any]{
			"requires_manual_fraud_review": false,
		},
		Steps: types.JSONArray[core.WorkflowStep]{
			{
				Name:      "step_fraud",
				Task:      "check_fraud",
				Condition: "requires_manual_fraud_review",
			},
			{
				Name: "step_fulfill",
				Task: "fulfill_order",
			},
		},
	}

	_, err = app.Jobs().CreateWorkflow(wfSkipped)
	if err != nil {
		t.Fatalf("Failed to create workflow: %v", err)
	}

	resWf, err := app.Jobs().RunWorkflow(context.Background(), "wf_branch_skip_01")
	if err != nil {
		t.Fatalf("Workflow execution error: %v", err)
	}

	if fraudCheckRan {
		t.Fatal("Fraud check handler should have been skipped when condition was false")
	}
	if resWf.Status != core.WorkflowStatusCompleted {
		t.Fatalf("Expected completed status, got: %s", resWf.Status)
	}
	if resWf.Context["fulfilled"] != true {
		t.Fatal("Order was not fulfilled after skip branch")
	}
}

func TestDurableWorkflows_FailureBranchingAndCompensation(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	compensationRan := false
	app.Jobs().RegisterWorkflowHandler("flaky_inventory_reservation", func(ctx context.Context, step *core.WorkflowStep, currentContext map[string]any) (map[string]any, error) {
		return nil, errors.New("out of stock in warehouse")
	})

	app.Jobs().RegisterWorkflowHandler("compensate_refund", func(ctx context.Context, step *core.WorkflowStep, currentContext map[string]any) (map[string]any, error) {
		compensationRan = true
		return map[string]any{
			"refundStatus": "refunded_to_customer",
		}, nil
	})

	// Workflow with OnFailure branching to compensation step
	wfCompensation := &core.Workflow{
		Id:   "wf_compensate_01",
		Name: "compensation_branch_test",
		Steps: types.JSONArray[core.WorkflowStep]{
			{
				Name:      "step_inventory",
				Task:      "flaky_inventory_reservation",
				OnFailure: "step_refund",
			},
			{
				Name: "step_standard_fulfillment",
				Task: "unknown_task",
			},
			{
				Name: "step_refund",
				Task: "compensate_refund",
			},
		},
	}

	_, err = app.Jobs().CreateWorkflow(wfCompensation)
	if err != nil {
		t.Fatalf("Failed to create workflow: %v", err)
	}

	resWf, err := app.Jobs().RunWorkflow(context.Background(), "wf_compensate_01")
	if err != nil {
		t.Fatalf("RunWorkflow returned error: %v", err)
	}

	if !compensationRan {
		t.Fatal("Compensation step did not execute upon step failure")
	}
	if resWf.Status != core.WorkflowStatusCompleted {
		t.Fatalf("Expected workflow to reach completed after successful compensation, got: %s", resWf.Status)
	}
	if resWf.Context["refundStatus"] != "refunded_to_customer" {
		t.Fatalf("Context does not contain compensation result: %v", resWf.Context)
	}
}
