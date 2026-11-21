package core_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/tests"
	"github.com/pocketbase/dbx"
)

func TestRunInTransaction(t *testing.T) {
	app, _ := tests.NewTestApp()
	defer app.Cleanup()

	t.Run("failed nested transaction", func(t *testing.T) {
		app.RunInTransaction(func(txApp core.App) error {
			superuser, _ := txApp.FindAuthRecordByEmail(core.CollectionNameSuperusers, "test@example.com")

			return txApp.RunInTransaction(func(tx2Dao core.App) error {
				if err := tx2Dao.Delete(superuser); err != nil {
					t.Fatal(err)
				}
				return errors.New("test error")
			})
		})

		// superuser should still exist
		superuser, _ := app.FindAuthRecordByEmail(core.CollectionNameSuperusers, "test@example.com")
		if superuser == nil {
			t.Fatal("Expected superuser test@example.com to not be deleted")
		}
	})

	t.Run("successful nested transaction", func(t *testing.T) {
		app.RunInTransaction(func(txApp core.App) error {
			superuser, _ := txApp.FindAuthRecordByEmail(core.CollectionNameSuperusers, "test@example.com")

			return txApp.RunInTransaction(func(tx2Dao core.App) error {
				return tx2Dao.Delete(superuser)
			})
		})

		// superuser should have been deleted
		superuser, _ := app.FindAuthRecordByEmail(core.CollectionNameSuperusers, "test@example.com")
		if superuser != nil {
			t.Fatalf("Expected superuser test@example.com to be deleted, found %v", superuser)
		}
	})
}

func TestTransactionHooksCallsOnFailure(t *testing.T) {
	app, _ := tests.NewTestApp()
	defer app.Cleanup()

	createHookCalls := 0
	updateHookCalls := 0
	deleteHookCalls := 0
	afterCreateHookCalls := 0
	afterUpdateHookCalls := 0
	afterDeleteHookCalls := 0

	app.OnModelCreate().BindFunc(func(e *core.ModelEvent) error {
		createHookCalls++
		return e.Next()
	})

	app.OnModelUpdate().BindFunc(func(e *core.ModelEvent) error {
		updateHookCalls++
		return e.Next()
	})

	app.OnModelDelete().BindFunc(func(e *core.ModelEvent) error {
		deleteHookCalls++
		return e.Next()
	})

	app.OnModelAfterCreateSuccess().BindFunc(func(e *core.ModelEvent) error {
		afterCreateHookCalls++
		return e.Next()
	})

	app.OnModelAfterUpdateSuccess().BindFunc(func(e *core.ModelEvent) error {
		afterUpdateHookCalls++
		return e.Next()
	})

	app.OnModelAfterDeleteSuccess().BindFunc(func(e *core.ModelEvent) error {
		afterDeleteHookCalls++
		return e.Next()
	})

	existingModel, _ := app.FindAuthRecordByEmail(core.CollectionNameSuperusers, "test@example.com")

	app.RunInTransaction(func(txApp1 core.App) error {
		return txApp1.RunInTransaction(func(txApp2 core.App) error {
			// test create
			// ---
			newModel := core.NewRecord(existingModel.Collection())
			newModel.SetEmail("test_new1@example.com")
			newModel.SetPassword("1234567890")
			if err := txApp2.Save(newModel); err != nil {
				t.Fatal(err)
			}

			// test update (twice)
			// ---
			if err := txApp2.Save(existingModel); err != nil {
				t.Fatal(err)
			}
			if err := txApp2.Save(existingModel); err != nil {
				t.Fatal(err)
			}

			// test delete
			// ---
			if err := txApp2.Delete(newModel); err != nil {
				t.Fatal(err)
			}

			return errors.New("test_tx_error")
		})
	})

	if createHookCalls != 1 {
		t.Errorf("Expected createHookCalls to be called 1 time, got %d", createHookCalls)
	}
	if updateHookCalls != 2 {
		t.Errorf("Expected updateHookCalls to be called 2 times, got %d", updateHookCalls)
	}
	if deleteHookCalls != 1 {
		t.Errorf("Expected deleteHookCalls to be called 1 time, got %d", deleteHookCalls)
	}
	if afterCreateHookCalls != 0 {
		t.Errorf("Expected afterCreateHookCalls to be called 0 times, got %d", afterCreateHookCalls)
	}
	if afterUpdateHookCalls != 0 {
		t.Errorf("Expected afterUpdateHookCalls to be called 0 times, got %d", afterUpdateHookCalls)
	}
	if afterDeleteHookCalls != 0 {
		t.Errorf("Expected afterDeleteHookCalls to be called 0 times, got %d", afterDeleteHookCalls)
	}
}

func TestTransactionHooksCallsOnSuccess(t *testing.T) {
	app, _ := tests.NewTestApp()
	defer app.Cleanup()

	createHookCalls := 0
	updateHookCalls := 0
	deleteHookCalls := 0
	afterCreateHookCalls := 0
	afterUpdateHookCalls := 0
	afterDeleteHookCalls := 0

	app.OnModelCreate().BindFunc(func(e *core.ModelEvent) error {
		createHookCalls++
		return e.Next()
	})

	app.OnModelUpdate().BindFunc(func(e *core.ModelEvent) error {
		updateHookCalls++
		return e.Next()
	})

	app.OnModelDelete().BindFunc(func(e *core.ModelEvent) error {
		deleteHookCalls++
		return e.Next()
	})

	app.OnModelAfterCreateSuccess().BindFunc(func(e *core.ModelEvent) error {
		if e.App.IsTransactional() {
			t.Fatal("Expected e.App to be non-transactional")
		}

		afterCreateHookCalls++
		return e.Next()
	})

	app.OnModelAfterUpdateSuccess().BindFunc(func(e *core.ModelEvent) error {
		if e.App.IsTransactional() {
			t.Fatal("Expected e.App to be non-transactional")
		}

		afterUpdateHookCalls++
		return e.Next()
	})

	app.OnModelAfterDeleteSuccess().BindFunc(func(e *core.ModelEvent) error {
		if e.App.IsTransactional() {
			t.Fatal("Expected e.App to be non-transactional")
		}

		afterDeleteHookCalls++
		return e.Next()
	})

	existingModel, _ := app.FindAuthRecordByEmail(core.CollectionNameSuperusers, "test@example.com")

	app.RunInTransaction(func(txApp1 core.App) error {
		return txApp1.RunInTransaction(func(txApp2 core.App) error {
			// test create
			// ---
			newModel := core.NewRecord(existingModel.Collection())
			newModel.SetEmail("test_new1@example.com")
			newModel.SetPassword("1234567890")
			if err := txApp2.Save(newModel); err != nil {
				t.Fatal(err)
			}

			// test update (twice)
			// ---
			if err := txApp2.Save(existingModel); err != nil {
				t.Fatal(err)
			}
			if err := txApp2.Save(existingModel); err != nil {
				t.Fatal(err)
			}

			// test delete
			// ---
			if err := txApp2.Delete(newModel); err != nil {
				t.Fatal(err)
			}

			return nil
		})
	})

	if createHookCalls != 1 {
		t.Errorf("Expected createHookCalls to be called 1 time, got %d", createHookCalls)
	}
	if updateHookCalls != 2 {
		t.Errorf("Expected updateHookCalls to be called 2 times, got %d", updateHookCalls)
	}
	if deleteHookCalls != 1 {
		t.Errorf("Expected deleteHookCalls to be called 1 time, got %d", deleteHookCalls)
	}
	if afterCreateHookCalls != 1 {
		t.Errorf("Expected afterCreateHookCalls to be called 1 time, got %d", afterCreateHookCalls)
	}
	if afterUpdateHookCalls != 2 {
		t.Errorf("Expected afterUpdateHookCalls to be called 2 times, got %d", afterUpdateHookCalls)
	}
	if afterDeleteHookCalls != 1 {
		t.Errorf("Expected afterDeleteHookCalls to be called 1 time, got %d", afterDeleteHookCalls)
	}
}

func TestTransactionFromInnerCreateHook(t *testing.T) {
	t.Parallel()

	app, _ := tests.NewTestApp()
	defer app.Cleanup()

	app.OnRecordCreateExecute("demo2").BindFunc(func(e *core.RecordEvent) error {
		originalApp := e.App
		return e.App.RunInTransaction(func(txApp core.App) error {
			e.App = txApp
			defer func() {
				e.App = originalApp
			}()

			nextErr := e.Next()

			return nextErr
		})
	})

	app.OnRecordAfterCreateSuccess("demo2").BindFunc(func(e *core.RecordEvent) error {
		if e.App.IsTransactional() {
			t.Fatal("Expected e.App to be non-transactional")
		}

		// perform a db query with the app instance to ensure that it is still valid
		_, err := e.App.FindFirstRecordByFilter("demo2", "1=1")
		if err != nil {
			t.Fatalf("Failed to perform a db query after tx success: %v", err)
		}

		return e.Next()
	})

	collection, err := app.FindCollectionByNameOrId("demo2")
	if err != nil {
		t.Fatal(err)
	}

	record := core.NewRecord(collection)

	record.Set("title", "test_inner_tx")

	if err = app.Save(record); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	expectedHookCalls := map[string]int{
		"OnRecordCreateExecute":      1,
		"OnRecordAfterCreateSuccess": 1,
	}
	for k, total := range expectedHookCalls {
		if found, ok := app.EventCalls[k]; !ok || total != found {
			t.Fatalf("Expected %q %d calls, got %d", k, total, found)
		}
	}
}

func TestTransactionFromInnerUpdateHook(t *testing.T) {
	t.Parallel()

	app, _ := tests.NewTestApp()
	defer app.Cleanup()

	app.OnRecordUpdateExecute("demo2").BindFunc(func(e *core.RecordEvent) error {
		originalApp := e.App
		return e.App.RunInTransaction(func(txApp core.App) error {
			e.App = txApp
			defer func() {
				e.App = originalApp
			}()

			nextErr := e.Next()

			return nextErr
		})
	})

	app.OnRecordAfterUpdateSuccess("demo2").BindFunc(func(e *core.RecordEvent) error {
		if e.App.IsTransactional() {
			t.Fatal("Expected e.App to be non-transactional")
		}

		// perform a db query with the app instance to ensure that it is still valid
		_, err := e.App.FindFirstRecordByFilter("demo2", "1=1")
		if err != nil {
			t.Fatalf("Failed to perform a db query after tx success: %v", err)
		}

		return e.Next()
	})

	existingModel, err := app.FindFirstRecordByFilter("demo2", "1=1")
	if err != nil {
		t.Fatal(err)
	}

	if err = app.Save(existingModel); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	expectedHookCalls := map[string]int{
		"OnRecordUpdateExecute":      1,
		"OnRecordAfterUpdateSuccess": 1,
	}
	for k, total := range expectedHookCalls {
		if found, ok := app.EventCalls[k]; !ok || total != found {
			t.Fatalf("Expected %q %d calls, got %d", k, total, found)
		}
	}
}

func TestTransactionFromInnerDeleteHook(t *testing.T) {
	t.Parallel()

	app, _ := tests.NewTestApp()
	defer app.Cleanup()

	app.OnRecordDeleteExecute("demo2").BindFunc(func(e *core.RecordEvent) error {
		originalApp := e.App
		return e.App.RunInTransaction(func(txApp core.App) error {
			e.App = txApp
			defer func() {
				e.App = originalApp
			}()

			nextErr := e.Next()

			return nextErr
		})
	})

	app.OnRecordAfterDeleteSuccess("demo2").BindFunc(func(e *core.RecordEvent) error {
		if e.App.IsTransactional() {
			t.Fatal("Expected e.App to be non-transactional")
		}

		// perform a db query with the app instance to ensure that it is still valid
		_, err := e.App.FindFirstRecordByFilter("demo2", "1=1")
		if err != nil {
			t.Fatalf("Failed to perform a db query after tx success: %v", err)
		}

		return e.Next()
	})

	existingModel, err := app.FindFirstRecordByFilter("demo2", "1=1")
	if err != nil {
		t.Fatal(err)
	}

	if err = app.Delete(existingModel); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	expectedHookCalls := map[string]int{
		"OnRecordDeleteExecute":      1,
		"OnRecordAfterDeleteSuccess": 1,
	}
	for k, total := range expectedHookCalls {
		if found, ok := app.EventCalls[k]; !ok || total != found {
			t.Fatalf("Expected %q %d calls, got %d", k, total, found)
		}
	}
}

func TestRunInTransactionWithContext_PreCancelled(t *testing.T) {
	app, _ := tests.NewTestApp()
	defer app.Cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancelled

	called := false
	err := app.RunInTransactionWithContext(ctx, func(txApp core.App) error {
		called = true
		return nil
	})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled error, got %v", err)
	}
	if called {
		t.Fatal("expected callback not to be invoked when context is cancelled")
	}
}

func TestRunInTransactionWithContext_NestedSavepointIsolation(t *testing.T) {
	app, _ := tests.NewTestApp()
	defer app.Cleanup()

	demoCol, err := app.FindCollectionByNameOrId("demo2")
	if err != nil {
		t.Fatal(err)
	}

	err = app.RunInTransactionWithContext(context.Background(), func(outerTxApp core.App) error {
		// 1. Create a record in outer transaction
		outerRecord := core.NewRecord(demoCol)
		outerRecord.Set("title", "outer_record")
		if err := outerTxApp.Save(outerRecord); err != nil {
			return err
		}

		// 2. Run nested transaction that fails
		nestedErr := outerTxApp.RunInTransactionWithContext(context.Background(), func(nestedTxApp core.App) error {
			innerRecord := core.NewRecord(demoCol)
			innerRecord.Set("title", "inner_failed_record")
			if err := nestedTxApp.Save(innerRecord); err != nil {
				return err
			}
			return errors.New("simulated inner transaction failure")
		})

		if nestedErr == nil {
			t.Fatal("expected nested transaction to fail, got nil")
		}

		// 3. PostgreSQL savepoint isolation check:
		// Despite nested transaction failure, outer transaction is NOT poisoned,
		// and can successfully save another record and commit!
		secondOuterRecord := core.NewRecord(demoCol)
		secondOuterRecord.Set("title", "second_outer_record")
		if err := outerTxApp.Save(secondOuterRecord); err != nil {
			return fmt.Errorf("outer transaction poisoned despite savepoint: %w", err)
		}

		return nil
	})

	if err != nil {
		t.Fatalf("outer transaction commit failed: %v", err)
	}

	// Verify persistence: outer_record and second_outer_record must exist; inner_failed_record must not!
	outerRec, err := app.FindFirstRecordByFilter("demo2", "title = {:title}", dbx.Params{"title": "outer_record"})
	if err != nil || outerRec == nil {
		t.Fatalf("expected outer_record to be committed: %v", err)
	}

	secondRec, err := app.FindFirstRecordByFilter("demo2", "title = {:title}", dbx.Params{"title": "second_outer_record"})
	if err != nil || secondRec == nil {
		t.Fatalf("expected second_outer_record to be committed: %v", err)
	}

	innerRec, _ := app.FindFirstRecordByFilter("demo2", "title = {:title}", dbx.Params{"title": "inner_failed_record"})
	if innerRec != nil {
		t.Fatalf("expected inner_failed_record to have been rolled back by savepoint: %v", innerRec)
	}
}

func TestRunInTransactionWithContext_AfterFuncPromotion(t *testing.T) {
	app, _ := tests.NewTestApp()
	defer app.Cleanup()

	var order []string

	err := app.RunInTransactionWithContext(context.Background(), func(outerTxApp core.App) error {
		outerTxApp.TxInfo().OnComplete(func(txErr error) error {
			order = append(order, "outer_after")
			return nil
		})

		return outerTxApp.RunInTransactionWithContext(context.Background(), func(nestedTxApp core.App) error {
			nestedTxApp.TxInfo().OnComplete(func(txErr error) error {
				order = append(order, "nested_after")
				return nil
			})
			return nil
		})
	})

	if err != nil {
		t.Fatalf("transaction failed: %v", err)
	}

	if len(order) != 2 || order[0] != "outer_after" || order[1] != "nested_after" {
		t.Fatalf("unexpected afterFunc execution order: %v", order)
	}
}
