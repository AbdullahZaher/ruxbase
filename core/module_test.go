package core_test

import (
	"context"
	"errors"
	"testing"

	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/tests"
)

type testModule struct {
	name         string
	initErr      error
	shutdownErr  error
	initCalled   bool
	shutdownDone bool
	initOrder    *[]string
	shutOrder    *[]string
}

func (m *testModule) Name() string {
	return m.name
}

func (m *testModule) Init(app core.App) error {
	m.initCalled = true
	if m.initOrder != nil {
		*m.initOrder = append(*m.initOrder, m.name)
	}
	return m.initErr
}

func (m *testModule) Shutdown(ctx context.Context) error {
	m.shutdownDone = true
	if m.shutOrder != nil {
		*m.shutOrder = append(*m.shutOrder, m.name)
	}
	return m.shutdownErr
}

func TestModuleRegistration(t *testing.T) {
	app, _ := tests.NewTestApp()
	defer app.Cleanup()

	// nil module
	if err := app.RegisterModule(nil); err == nil {
		t.Fatal("expected error registering nil module, got nil")
	}

	// empty name module
	mEmpty := &testModule{name: ""}
	if err := app.RegisterModule(mEmpty); !errors.Is(err, core.ErrModuleNameEmpty) {
		t.Fatalf("expected ErrModuleNameEmpty, got %v", err)
	}

	// valid module
	m1 := &testModule{name: "mod1"}
	if err := app.RegisterModule(m1); err != nil {
		t.Fatalf("unexpected error registering module: %v", err)
	}

	// duplicate module
	m1Dup := &testModule{name: "mod1"}
	if err := app.RegisterModule(m1Dup); !errors.Is(err, core.ErrModuleDuplicate) {
		t.Fatalf("expected ErrModuleDuplicate, got %v", err)
	}

	// query module
	found, ok := app.Module("mod1")
	if !ok || found.Name() != "mod1" {
		t.Fatalf("expected to find mod1, got %v (ok=%v)", found, ok)
	}

	_, ok = app.Module("nonexistent")
	if ok {
		t.Fatal("expected nonexistent module to not be found")
	}
}

func TestModuleLifecycleOrder(t *testing.T) {
	app := core.NewBaseApp(core.BaseAppConfig{
		DataDir:       t.TempDir(),
		EncryptionEnv: "pb_test_env",
	})
	defer app.ClearBootstrap()

	var initOrder []string
	var shutOrder []string

	m1 := &testModule{name: "mod_alpha", initOrder: &initOrder, shutOrder: &shutOrder}
	m2 := &testModule{name: "mod_beta", initOrder: &initOrder, shutOrder: &shutOrder}
	m3 := &testModule{name: "mod_gamma", initOrder: &initOrder, shutOrder: &shutOrder}

	if err := app.RegisterModule(m1); err != nil {
		t.Fatal(err)
	}
	if err := app.RegisterModule(m2); err != nil {
		t.Fatal(err)
	}
	if err := app.RegisterModule(m3); err != nil {
		t.Fatal(err)
	}

	modules := app.Modules()
	if len(modules) != 3 {
		t.Fatalf("expected 3 modules, got %d", len(modules))
	}
	if modules[0].Name() != "mod_alpha" || modules[1].Name() != "mod_beta" || modules[2].Name() != "mod_gamma" {
		t.Fatalf("modules not in registration order: %v, %v, %v", modules[0].Name(), modules[1].Name(), modules[2].Name())
	}

	// Bootstrap initializes in order
	if err := app.Bootstrap(); err != nil {
		t.Fatalf("bootstrap failed: %v", err)
	}

	if len(initOrder) != 3 || initOrder[0] != "mod_alpha" || initOrder[1] != "mod_beta" || initOrder[2] != "mod_gamma" {
		t.Fatalf("unexpected init order: %v", initOrder)
	}

	// ClearBootstrap shuts down in reverse order
	if err := app.ClearBootstrap(); err != nil {
		t.Fatalf("clear bootstrap failed: %v", err)
	}

	if len(shutOrder) != 3 || shutOrder[0] != "mod_gamma" || shutOrder[1] != "mod_beta" || shutOrder[2] != "mod_alpha" {
		t.Fatalf("unexpected shutdown order: %v", shutOrder)
	}
}

func TestModuleInitFailureRollback(t *testing.T) {
	app := core.NewBaseApp(core.BaseAppConfig{
		DataDir:       t.TempDir(),
		EncryptionEnv: "pb_test_env",
	})
	defer app.ClearBootstrap()

	var initOrder []string
	var shutOrder []string

	m1 := &testModule{name: "mod_first", initOrder: &initOrder, shutOrder: &shutOrder}
	m2 := &testModule{name: "mod_fail", initErr: errors.New("failing module"), initOrder: &initOrder, shutOrder: &shutOrder}

	if err := app.RegisterModule(m1); err != nil {
		t.Fatal(err)
	}
	if err := app.RegisterModule(m2); err != nil {
		t.Fatal(err)
	}

	err := app.Bootstrap()
	if err == nil {
		t.Fatal("expected bootstrap to fail due to mod_fail init error, got nil")
	}

	if !errors.Is(err, core.ErrModuleInitFailed) {
		t.Fatalf("expected ErrModuleInitFailed, got %v", err)
	}

	// mod_first should have been shut down during rollback
	if !m1.shutdownDone {
		t.Fatal("expected mod_first to be shut down on rollback")
	}
}

func TestPostBootstrapModuleRegistration(t *testing.T) {
	app, _ := tests.NewTestApp()
	defer app.Cleanup()

	mPost := &testModule{name: "post_boot"}
	if err := app.RegisterModule(mPost); err != nil {
		t.Fatalf("unexpected error registering module after bootstrap: %v", err)
	}

	if !mPost.initCalled {
		t.Fatal("expected post-bootstrap registered module to be initialized immediately")
	}

	// Post-bootstrap registration with failure
	mFail := &testModule{name: "post_fail", initErr: errors.New("immediate fail")}
	err := app.RegisterModule(mFail)
	if err == nil || !errors.Is(err, core.ErrModuleInitFailed) {
		t.Fatalf("expected ErrModuleInitFailed, got %v", err)
	}

	// Ensure failing module was not kept in registry
	if _, ok := app.Module("post_fail"); ok {
		t.Fatal("expected failed post-bootstrap module to be removed from registry")
	}
}
