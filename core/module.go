package core

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Standard platform kernel module errors.
var (
	ErrModuleNameEmpty       = errors.New("module name cannot be empty")
	ErrModuleDuplicate       = errors.New("module already registered")
	ErrModuleNotFound        = errors.New("module not found")
	ErrModuleInitFailed      = errors.New("module initialization failed")
	ErrModuleShutdownTimeout = errors.New("module shutdown timed out")
)

// Module represents a typed platform subsystem or extension component.
// Modules are registered via App.RegisterModule and initialized in registration order.
type Module interface {
	// Name returns the unique identifier for the module.
	Name() string

	// Init initializes the module with the provided application instance.
	Init(app App) error
}

// ShutdownModule is an optional interface that modules can implement
// to release resources during application shutdown (ClearBootstrap).
type ShutdownModule interface {
	Module

	// Shutdown gracefully terminates module resources within the provided context.
	Shutdown(ctx context.Context) error
}

type moduleEntry struct {
	module      Module
	initialized bool
}

type moduleRegistry struct {
	mu      sync.RWMutex
	modules map[string]*moduleEntry
	order   []string
}

func newModuleRegistry() *moduleRegistry {
	return &moduleRegistry{
		modules: make(map[string]*moduleEntry),
		order:   make([]string, 0),
	}
}

// RegisterModule registers an internal platform module with the application.
// If the application is already bootstrapped, Init is called immediately.
// Otherwise, module initialization is deferred to Bootstrap().
func (app *BaseApp) RegisterModule(module Module) error {
	if module == nil {
		return errors.New("module cannot be nil")
	}

	name := module.Name()
	if name == "" {
		return ErrModuleNameEmpty
	}

	app.modules.mu.Lock()
	if _, exists := app.modules.modules[name]; exists {
		app.modules.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrModuleDuplicate, name)
	}

	entry := &moduleEntry{
		module:      module,
		initialized: false,
	}
	app.modules.modules[name] = entry
	app.modules.order = append(app.modules.order, name)
	isBootstrapped := app.IsBootstrapped()
	app.modules.mu.Unlock()

	// If app is already bootstrapped, initialize the module immediately
	if isBootstrapped {
		if err := module.Init(app); err != nil {
			app.modules.mu.Lock()
			delete(app.modules.modules, name)
			for i, n := range app.modules.order {
				if n == name {
					app.modules.order = append(app.modules.order[:i], app.modules.order[i+1:]...)
					break
				}
			}
			app.modules.mu.Unlock()
			return fmt.Errorf("%w: %s: %v", ErrModuleInitFailed, name, err)
		}
		app.modules.mu.Lock()
		entry.initialized = true
		app.modules.mu.Unlock()
	}

	return nil
}

// Modules returns all registered modules in registration order.
func (app *BaseApp) Modules() []Module {
	app.modules.mu.RLock()
	defer app.modules.mu.RUnlock()

	result := make([]Module, 0, len(app.modules.order))
	for _, name := range app.modules.order {
		if entry, ok := app.modules.modules[name]; ok {
			result = append(result, entry.module)
		}
	}
	return result
}

// Module returns the registered module with the given name, if found.
func (app *BaseApp) Module(name string) (Module, bool) {
	app.modules.mu.RLock()
	defer app.modules.mu.RUnlock()

	if entry, ok := app.modules.modules[name]; ok {
		return entry.module, true
	}
	return nil, false
}

// initModules initializes all uninitialized registered modules in order.
// If any module fails, it rolls back previously initialized modules (calling Shutdown if implemented)
// and returns an error.
func (app *BaseApp) initModules() error {
	app.modules.mu.Lock()
	defer app.modules.mu.Unlock()

	var initializedNames []string

	for _, name := range app.modules.order {
		entry := app.modules.modules[name]
		if entry == nil || entry.initialized {
			continue
		}

		if err := entry.module.Init(app); err != nil {
			// Rollback initialized modules in reverse order
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			for i := len(initializedNames) - 1; i >= 0; i-- {
				revName := initializedNames[i]
				if revEntry, ok := app.modules.modules[revName]; ok && revEntry.initialized {
					if sm, ok := revEntry.module.(ShutdownModule); ok {
						_ = sm.Shutdown(ctx)
					}
					revEntry.initialized = false
				}
			}

			return fmt.Errorf("%w: %s: %v", ErrModuleInitFailed, name, err)
		}

		entry.initialized = true
		initializedNames = append(initializedNames, name)
	}

	return nil
}

// shutdownModules shuts down all initialized modules in reverse registration order.
func (app *BaseApp) shutdownModules(ctx context.Context) error {
	app.modules.mu.Lock()
	defer app.modules.mu.Unlock()

	var errs []error

	// Reverse order (LIFO)
	for i := len(app.modules.order) - 1; i >= 0; i-- {
		name := app.modules.order[i]
		entry := app.modules.modules[name]
		if entry == nil || !entry.initialized {
			continue
		}

		if sm, ok := entry.module.(ShutdownModule); ok {
			if err := sm.Shutdown(ctx); err != nil {
				errs = append(errs, fmt.Errorf("module %s shutdown error: %w", name, err))
			}
		}

		entry.initialized = false
	}

	return errors.Join(errs...)
}
