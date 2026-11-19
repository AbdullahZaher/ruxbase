package apis_test

import (
	"testing"

	"github.com/AbdullahZaher/ruxbase/apis"
	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/tests"
)

// TestMCPRouteWithStaticFiles checks that the MCP route doesn't conflict
// with a catch-all static files route (eg. the pb_public one of the binary).
func TestMCPRouteWithStaticFiles(t *testing.T) {
	t.Parallel()

	app, _ := tests.NewTestApp()
	defer app.Cleanup()

	pbRouter, err := apis.NewRouter(app)
	if err != nil {
		t.Fatal(err)
	}

	pbRouter.GET("/{path...}", func(e *core.RequestEvent) error { return nil })

	if _, err := pbRouter.BuildMux(); err != nil {
		t.Fatalf("Expected the mux to build, got %v", err)
	}
}
