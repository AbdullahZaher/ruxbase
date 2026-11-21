package core_test

import (
	"testing"

	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/tests"
	"github.com/AbdullahZaher/ruxbase/tools/types"
)

func TestCollectionRuleForOp(t *testing.T) {
	col := core.NewBaseCollection("test_col")
	listRule := "status = 'active'"
	viewRule := "id != ''"
	createRule := "@request.auth.id != ''"
	updateRule := "owner = @request.auth.id"
	deleteRule := "@request.auth.id = 'admin'"

	col.ListRule = &listRule
	col.ViewRule = &viewRule
	col.CreateRule = &createRule
	col.UpdateRule = &updateRule
	col.DeleteRule = &deleteRule

	tests := []struct {
		op       string
		expected *string
		wantErr  bool
	}{
		{core.AuthzOpList, &listRule, false},
		{core.AuthzOpView, &viewRule, false},
		{core.AuthzOpCreate, &createRule, false},
		{core.AuthzOpUpdate, &updateRule, false},
		{core.AuthzOpDelete, &deleteRule, false},
		{"invalid_op", nil, true},
	}

	for _, tc := range tests {
		t.Run(tc.op, func(t *testing.T) {
			rule, err := core.CollectionRuleForOp(col, tc.op)
			if (err != nil) != tc.wantErr {
				t.Fatalf("CollectionRuleForOp() error = %v, wantErr %v", err, tc.wantErr)
			}
			if !tc.wantErr && rule != tc.expected {
				t.Fatalf("CollectionRuleForOp() = %v, want %v", rule, tc.expected)
			}
		})
	}

	if _, err := core.CollectionRuleForOp(nil, core.AuthzOpList); err == nil {
		t.Fatal("expected error for nil collection")
	}
}

func TestCanAccessCollectionOp(t *testing.T) {
	col := core.NewBaseCollection("test_col")
	activeRule := "status = 'active'"
	emptyRule := ""

	col.ListRule = &activeRule
	col.ViewRule = &emptyRule
	col.DeleteRule = nil // admin only

	// 1. Nil collection returns error
	if _, _, err := core.CanAccessCollectionOp(nil, nil, core.AuthzOpList); err == nil {
		t.Fatal("expected error for nil collection")
	}

	// 2. Guest user on nil rule -> denied
	allowed, rule, err := core.CanAccessCollectionOp(col, nil, core.AuthzOpDelete)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allowed || rule != nil {
		t.Fatalf("expected denied for nil rule, got allowed=%v, rule=%v", allowed, rule)
	}

	// 3. Guest user on empty rule -> allowed
	allowed, rule, err = core.CanAccessCollectionOp(col, nil, core.AuthzOpView)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed || rule == nil || *rule != "" {
		t.Fatalf("expected allowed for empty rule, got allowed=%v, rule=%v", allowed, rule)
	}

	// 4. Guest user on conditional rule -> allowed with rule
	allowed, rule, err = core.CanAccessCollectionOp(col, nil, core.AuthzOpList)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed || rule == nil || *rule != activeRule {
		t.Fatalf("expected allowed with activeRule, got allowed=%v, rule=%v", allowed, rule)
	}

	// 5. Superuser is always allowed even on nil rule
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()

	superuser, err := app.FindAuthRecordByEmail(core.CollectionNameSuperusers, "test@example.com")
	if err != nil {
		t.Fatal(err)
	}
	suRequestInfo := &core.RequestInfo{Auth: superuser}

	allowed, rule, err = core.CanAccessCollectionOp(col, suRequestInfo, core.AuthzOpDelete)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed || rule != nil {
		t.Fatalf("expected superuser allowed without rule, got allowed=%v, rule=%v", allowed, rule)
	}
}

func TestIsRuleSupportedInRLS(t *testing.T) {
	tests := []struct {
		rule      string
		supported bool
	}{
		{"", true},
		{"   ", true},
		{"status = 'active'", true},
		{"@request.auth.id != '' && owner = @request.auth.id", true},
		{"author.active = true", true},
		{"@request.headers.x_secret = '123'", false},
		{"@request.body.name = 'test'", false},
		{"@request.query.page = '1'", false},
		{"@request.method = 'GET'", false},
		{"@request.context = 'oauth'", false},
		{"title:isset = true", false},
		{"status:changed = true", false},
	}

	for _, tc := range tests {
		t.Run(tc.rule, func(t *testing.T) {
			supported, reason := core.IsRuleSupportedInRLS(tc.rule)
			if supported != tc.supported {
				t.Fatalf("IsRuleSupportedInRLS(%q) = %v (reason: %q), want %v", tc.rule, supported, reason, tc.supported)
			}
			if !tc.supported && reason == "" {
				t.Fatalf("expected non-empty reason for unsupported rule %q", tc.rule)
			}
		})
	}
}

func TestCanAccessRecordDefensiveChecks(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()

	// Nil record should fail closed with error
	if ok, err := app.CanAccessRecord(nil, nil, types.Pointer("")); ok || err == nil {
		t.Fatalf("expected error for nil record, got ok=%v, err=%v", ok, err)
	}

	col, err := app.FindCollectionByNameOrId("demo1")
	if err != nil {
		t.Fatal(err)
	}

	record := core.NewRecord(col)
	record.Set("text", "hello")

	// Nil requestInfo with public rule should succeed safely without panic
	if ok, err := app.CanAccessRecord(record, nil, types.Pointer("")); !ok || err != nil {
		t.Fatalf("expected ok for nil requestInfo with public rule, got ok=%v, err=%v", ok, err)
	}

	// Nil requestInfo with admin-only rule should return false safely without panic
	if ok, err := app.CanAccessRecord(record, nil, nil); ok || err != nil {
		t.Fatalf("expected denied for nil requestInfo with nil rule, got ok=%v, err=%v", ok, err)
	}
}
