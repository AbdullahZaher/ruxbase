package apis

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/tools/router"
)

// bindSQLRPC registers the SQL RPC introspection and invocation endpoints.
func bindSQLRPC(app core.App, rg *router.RouterGroup[*core.RequestEvent]) {
	rpcGroup := rg.Group("/ruxbase/rpc")

	// Administrative endpoints
	adminGroup := rpcGroup.Group("").Bind(RequireSuperuserAuth())
	adminGroup.GET("/functions", rpcFunctionsList)
	adminGroup.GET("/functions/{name}", rpcFunctionDetail)
	adminGroup.POST("/expose", rpcExpose)
	adminGroup.POST("/unexpose", rpcUnexpose)

	// Caller-facing RPC execution endpoint
	rpcGroup.POST("/{name}", rpcExecute)
}

func rpcFunctionsList(e *core.RequestEvent) error {
	if !e.HasSuperuserAuth() {
		return e.ForbiddenError("Only administrators can list database functions.", nil)
	}

	schema := strings.TrimSpace(e.Request.URL.Query().Get("schema"))
	if schema == "" {
		schema = "public"
	}

	functions, err := e.App.ListRPCFunctions(schema)
	if err != nil {
		return e.BadRequestError("Failed to list RPC functions: "+err.Error(), err)
	}

	return e.JSON(http.StatusOK, functions)
}

func rpcFunctionDetail(e *core.RequestEvent) error {
	if !e.HasSuperuserAuth() {
		return e.ForbiddenError("Only administrators can inspect database functions.", nil)
	}

	name := e.Request.PathValue("name")
	fn, err := e.App.IntrospectRPCFunction(name)
	if err != nil {
		return e.NotFoundError(fmt.Sprintf("Function %q not found or not accessible.", name), err)
	}

	return e.JSON(http.StatusOK, fn)
}

func rpcExpose(e *core.RequestEvent) error {
	if !e.HasSuperuserAuth() {
		return e.ForbiddenError("Only administrators can expose database functions.", nil)
	}

	form := core.ExposeFunctionOptions{}
	if err := e.BindBody(&form); err != nil {
		return e.BadRequestError("Invalid request body.", err)
	}

	fn, err := e.App.ExposeRPCFunction(form)
	if err != nil {
		return e.BadRequestError("Failed to expose function: "+err.Error(), err)
	}

	return e.JSON(http.StatusOK, fn)
}

func rpcUnexpose(e *core.RequestEvent) error {
	if !e.HasSuperuserAuth() {
		return e.ForbiddenError("Only administrators can unexpose database functions.", nil)
	}

	var data struct {
		Name string `json:"name"`
	}
	if err := e.BindBody(&data); err != nil || data.Name == "" {
		return e.BadRequestError("Function name is required.", err)
	}

	if err := e.App.UnexposeRPCFunction(data.Name); err != nil {
		return e.BadRequestError("Failed to unexpose function: "+err.Error(), err)
	}

	return e.NoContent(http.StatusNoContent)
}

func rpcExecute(e *core.RequestEvent) error {
	name := e.Request.PathValue("name")

	fn, err := e.App.IntrospectRPCFunction(name)
	if err != nil || !fn.IsExposed {
		return e.NotFoundError(fmt.Sprintf("RPC function %q not found or not exposed.", name), err)
	}

	// Authorization check
	if !e.HasSuperuserAuth() {
		if fn.AccessRule == nil {
			return e.ForbiddenError("This RPC function requires superuser authorization.", nil)
		}
		if *fn.AccessRule != "" && e.Auth == nil {
			return e.UnauthorizedError("Authentication required to execute this function.", nil)
		}
	}

	args := map[string]any{}
	_ = e.BindBody(&args)

	res, err := e.App.ExecuteRPC(name, args, e.Auth, e.HasSuperuserAuth())
	if err != nil {
		return e.BadRequestError(err.Error(), err)
	}

	return e.JSON(http.StatusOK, map[string]any{
		"result": res,
	})
}
