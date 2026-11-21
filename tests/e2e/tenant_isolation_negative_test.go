package e2e_test

import (
	"errors"
	"testing"

	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/tests"
	"github.com/pocketbase/dbx"
)

func TestNegativeIsolation_APIsAndDirectQueries(t *testing.T) {
	testApp, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer testApp.Cleanup()

	// Create Tenant Alpha & Tenant Beta
	tAlpha, err := testApp.CreateTenant("Alpha Corp", "alpha-corp")
	if err != nil {
		t.Fatalf("Failed to create Alpha: %v", err)
	}
	tBeta, err := testApp.CreateTenant("Beta Corp", "beta-corp")
	if err != nil {
		t.Fatalf("Failed to create Beta: %v", err)
	}

	// Create tenant-bound collection "accounts"
	coll := core.NewBaseCollection("accounts")
	coll.Fields.Add(&core.TextField{Name: "account_number", Required: true})
	coll.Fields.Add(&core.TextField{Name: "balance", Required: true})
	if err := testApp.Save(coll); err != nil {
		t.Fatalf("Failed to save accounts collection: %v", err)
	}
	if err := testApp.EnsureTenantCollection(coll); err != nil {
		t.Fatalf("Failed to ensure tenant collection: %v", err)
	}

	// Seed account for Alpha
	recAlpha := core.NewRecord(coll)
	recAlpha.Set("account_number", "ACC_ALPHA_001")
	recAlpha.Set("balance", "10000")
	recAlpha.Set("tenant_id", tAlpha.Id)
	if err := testApp.Save(recAlpha); err != nil {
		t.Fatalf("Failed to save recAlpha: %v", err)
	}

	// Seed account for Beta
	recBeta := core.NewRecord(coll)
	recBeta.Set("account_number", "ACC_BETA_999")
	recBeta.Set("balance", "50000")
	recBeta.Set("tenant_id", tBeta.Id)
	if err := testApp.Save(recBeta); err != nil {
		t.Fatalf("Failed to save recBeta: %v", err)
	}

	// 1. SELECT by explicit ID of Tenant Beta under Tenant Alpha session -> MUST return 0 rows
	err = testApp.RunInScopedTransaction(core.TransactionScope{
		Role:     core.RLSAuthenticatedRole,
		TenantID: tAlpha.Id,
	}, func(txApp core.App) error {
		var numbers []string
		err := txApp.DB().Select("account_number").
			From("accounts").
			Where(dbx.HashExp{"id": recBeta.Id}).
			Column(&numbers)
		if err != nil {
			return err
		}
		if len(numbers) != 0 {
			return errors.New("Tenant Alpha must NOT see Tenant Beta account by ID")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Direct ID cross-tenant query failed: %v", err)
	}

	// 2. Cross-tenant UPDATE attempt: Tenant Alpha attempts to update Tenant Beta's balance
	err = testApp.RunInScopedTransaction(core.TransactionScope{
		Role:     core.RLSAuthenticatedRole,
		TenantID: tAlpha.Id,
	}, func(txApp core.App) error {
		res, err := txApp.DB().NewQuery(`
			UPDATE accounts SET balance = '0' WHERE id = {:betaId}
		`).Bind(dbx.Params{"betaId": recBeta.Id}).Execute()
		if err != nil {
			return err
		}
		rowsAffected, _ := res.RowsAffected()
		if rowsAffected != 0 {
			return errors.New("Cross-tenant UPDATE must affect 0 rows")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Cross-tenant UPDATE test failed: %v", err)
	}

	// 3. Cross-tenant DELETE attempt: Tenant Alpha attempts to delete Tenant Beta's account
	err = testApp.RunInScopedTransaction(core.TransactionScope{
		Role:     core.RLSAuthenticatedRole,
		TenantID: tAlpha.Id,
	}, func(txApp core.App) error {
		res, err := txApp.DB().NewQuery(`
			DELETE FROM accounts WHERE id = {:betaId}
		`).Bind(dbx.Params{"betaId": recBeta.Id}).Execute()
		if err != nil {
			return err
		}
		rowsAffected, _ := res.RowsAffected()
		if rowsAffected != 0 {
			return errors.New("Cross-tenant DELETE must affect 0 rows")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Cross-tenant DELETE test failed: %v", err)
	}

	// Verify Tenant Beta's account balance and record remain intact
	recBetaFresh, err := testApp.FindRecordById("accounts", recBeta.Id)
	if err != nil || recBetaFresh.GetString("balance") != "50000" {
		t.Fatalf("Tenant Beta record was corrupted or altered: %v", err)
	}
}

func TestNegativeIsolation_VectorSearch(t *testing.T) {
	testApp, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer testApp.Cleanup()

	tAlpha, err := testApp.CreateTenant("Alpha AI", "alpha-ai")
	if err != nil {
		t.Fatalf("Failed to create Alpha: %v", err)
	}
	tBeta, err := testApp.CreateTenant("Beta AI", "beta-ai")
	if err != nil {
		t.Fatalf("Failed to create Beta: %v", err)
	}

	// Create collection with VectorField
	coll := core.NewBaseCollection("documents")
	coll.Fields.Add(&core.TextField{Name: "title", Required: true})
	coll.Fields.Add(&core.VectorField{
		Name:       "embedding",
		Dimensions: 3,
		Metric:     core.VectorMetricCosine,
		Index:      true,
	})
	if err := testApp.Save(coll); err != nil {
		t.Fatalf("Failed to save vector collection: %v", err)
	}
	if err := testApp.EnsureTenantCollection(coll); err != nil {
		t.Fatalf("Failed to ensure tenant collection: %v", err)
	}

	// Seed identical vectors in Tenant Alpha and Tenant Beta
	identicalVector := []float32{0.5, 0.5, 0.5}

	docAlpha := core.NewRecord(coll)
	docAlpha.Set("title", "Alpha Secret Knowledge")
	docAlpha.Set("embedding", identicalVector)
	docAlpha.Set("tenant_id", tAlpha.Id)
	if err := testApp.Save(docAlpha); err != nil {
		t.Fatalf("Failed to save docAlpha: %v", err)
	}

	docBeta := core.NewRecord(coll)
	docBeta.Set("title", "Beta Proprietary Formula")
	docBeta.Set("embedding", identicalVector)
	docBeta.Set("tenant_id", tBeta.Id)
	if err := testApp.Save(docBeta); err != nil {
		t.Fatalf("Failed to save docBeta: %v", err)
	}

	// Run vector similarity query under Tenant Alpha session
	err = testApp.RunInScopedTransaction(core.TransactionScope{
		Role:     core.RLSAuthenticatedRole,
		TenantID: tAlpha.Id,
	}, func(txApp core.App) error {
		var titles []string
		err := txApp.DB().NewQuery(`
			SELECT title FROM documents
			ORDER BY embedding <=> '[0.5, 0.5, 0.5]'::vector
			LIMIT 10
		`).Column(&titles)
		if err != nil {
			return err
		}

		if len(titles) != 1 || titles[0] != "Alpha Secret Knowledge" {
			return errors.New("Vector search must strictly isolate results to Tenant Alpha")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Vector search tenant isolation failed: %v", err)
	}
}

func TestNegativeIsolation_JobsAndBatchUpdates(t *testing.T) {
	testApp, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer testApp.Cleanup()

	tAlpha, err := testApp.CreateTenant("Alpha Batch", "alpha-batch")
	if err != nil {
		t.Fatalf("Failed to create Alpha: %v", err)
	}
	tBeta, err := testApp.CreateTenant("Beta Batch", "beta-batch")
	if err != nil {
		t.Fatalf("Failed to create Beta: %v", err)
	}

	coll := core.NewBaseCollection("tasks")
	coll.Fields.Add(&core.TextField{Name: "task_name", Required: true})
	coll.Fields.Add(&core.TextField{Name: "status", Required: true})
	if err := testApp.Save(coll); err != nil {
		t.Fatalf("Failed to save tasks coll: %v", err)
	}
	if err := testApp.EnsureTenantCollection(coll); err != nil {
		t.Fatalf("Failed to ensure tenant collection: %v", err)
	}

	// Seed 2 tasks for Alpha and 2 tasks for Beta with status "pending"
	for i := 1; i <= 2; i++ {
		tA := core.NewRecord(coll)
		tA.Set("task_name", "Alpha Task")
		tA.Set("status", "pending")
		tA.Set("tenant_id", tAlpha.Id)
		if err := testApp.Save(tA); err != nil {
			t.Fatalf("Failed to save task A: %v", err)
		}

		tB := core.NewRecord(coll)
		tB.Set("task_name", "Beta Task")
		tB.Set("status", "pending")
		tB.Set("tenant_id", tBeta.Id)
		if err := testApp.Save(tB); err != nil {
			t.Fatalf("Failed to save task B: %v", err)
		}
	}

	// Simulate background worker executing on behalf of Tenant Alpha
	err = testApp.RunInScopedTransaction(core.TransactionScope{
		Role:     core.RLSAuthenticatedRole,
		TenantID: tAlpha.Id,
	}, func(txApp core.App) error {
		res, err := txApp.DB().NewQuery(`
			UPDATE tasks SET status = 'completed' WHERE status = 'pending'
		`).Execute()
		if err != nil {
			return err
		}
		rowsAffected, _ := res.RowsAffected()
		if rowsAffected != 2 {
			return errors.New("Expected exactly 2 tasks to be updated for Tenant Alpha")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Tenant Alpha worker batch execution failed: %v", err)
	}

	// Verify Tenant Beta tasks are still "pending"
	var betaStatuses []string
	err = testApp.DB().Select("status").
		From("tasks").
		Where(dbx.HashExp{"tenant_id": tBeta.Id}).
		Column(&betaStatuses)
	if err != nil {
		t.Fatalf("Failed to fetch Beta tasks: %v", err)
	}
	if len(betaStatuses) != 2 || betaStatuses[0] != "pending" || betaStatuses[1] != "pending" {
		t.Fatalf("Tenant Beta tasks were leaked or altered: %v", betaStatuses)
	}
}

func TestNegativeIsolation_BackupExportAndUnscopedFailClosed(t *testing.T) {
	testApp, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer testApp.Cleanup()

	tAlpha, err := testApp.CreateTenant("Alpha Backup", "alpha-backup")
	if err != nil {
		t.Fatalf("Failed to create Alpha: %v", err)
	}
	tBeta, err := testApp.CreateTenant("Beta Backup", "beta-backup")
	if err != nil {
		t.Fatalf("Failed to create Beta: %v", err)
	}

	coll := core.NewBaseCollection("invoices")
	coll.Fields.Add(&core.TextField{Name: "amount", Required: true})
	if err := testApp.Save(coll); err != nil {
		t.Fatalf("Failed to save coll: %v", err)
	}
	if err := testApp.EnsureTenantCollection(coll); err != nil {
		t.Fatalf("Failed to ensure tenant collection: %v", err)
	}

	invA := core.NewRecord(coll)
	invA.Set("amount", "500")
	invA.Set("tenant_id", tAlpha.Id)
	if err := testApp.Save(invA); err != nil {
		t.Fatalf("Failed to save invA: %v", err)
	}

	invB := core.NewRecord(coll)
	invB.Set("amount", "900")
	invB.Set("tenant_id", tBeta.Id)
	if err := testApp.Save(invB); err != nil {
		t.Fatalf("Failed to save invB: %v", err)
	}

	// 1. Export query scoped to Tenant Alpha must export ONLY Tenant Alpha's invoice
	err = testApp.RunInScopedTransaction(core.TransactionScope{
		Role:     core.RLSAuthenticatedRole,
		TenantID: tAlpha.Id,
	}, func(txApp core.App) error {
		var amounts []string
		err := txApp.DB().Select("amount").From("invoices").Column(&amounts)
		if err != nil {
			return err
		}
		if len(amounts) != 1 || amounts[0] != "500" {
			return errors.New("Backup export query leaked cross-tenant records")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Backup export isolation failed: %v", err)
	}

	// 2. Unscoped query under restricted role must return 0 rows (fail closed)
	err = testApp.RunInScopedTransaction(core.TransactionScope{
		Role:     core.RLSAuthenticatedRole,
		TenantID: "",
	}, func(txApp core.App) error {
		var amounts []string
		err := txApp.DB().Select("amount").From("invoices").Column(&amounts)
		if err != nil {
			return err
		}
		if len(amounts) != 0 {
			return errors.New("Unscoped query returned rows instead of failing closed")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Unscoped fail-closed test failed: %v", err)
	}
}
