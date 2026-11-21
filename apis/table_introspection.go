package apis

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/tools/router"
)

// bindTableIntrospection registers the PostgreSQL native table introspection and exposure endpoints.
func bindTableIntrospection(app core.App, rg *router.RouterGroup[*core.RequestEvent]) {
	subGroup := rg.Group("/ruxbase/tables").Bind(RequireSuperuserAuth())
	subGroup.GET("", tablesList)
	subGroup.GET("/{name}", tableDetail)
	subGroup.POST("/expose", tableExpose)
}

func tablesList(e *core.RequestEvent) error {
	if !e.HasSuperuserAuth() {
		return e.ForbiddenError("Only administrators can introspect database tables.", nil)
	}

	schema := strings.TrimSpace(e.Request.URL.Query().Get("schema"))
	if schema == "" {
		schema = "public"
	}

	tables, err := e.App.ListExposableTables(schema)
	if err != nil {
		return e.BadRequestError("Failed to list tables: "+err.Error(), err)
	}

	return e.JSON(http.StatusOK, tables)
}

type tableDetailResponse struct {
	Table               *core.NativeTable `json:"table"`
	SuggestedCollection *core.Collection  `json:"suggestedCollection"`
}

func tableDetail(e *core.RequestEvent) error {
	if !e.HasSuperuserAuth() {
		return e.ForbiddenError("Only administrators can introspect table details.", nil)
	}

	name := e.Request.PathValue("name")
	if err := core.ValidateExposedTableName(name); err != nil {
		return e.BadRequestError(err.Error(), err)
	}

	table, err := e.App.IntrospectTable(name)
	if err != nil {
		return e.NotFoundError(fmt.Sprintf("Table %q not found or not accessible.", name), err)
	}

	suggested, err := e.App.IntrospectAndBuildCollection(name)
	if err != nil {
		return e.BadRequestError("Failed to build collection schema: "+err.Error(), err)
	}

	return e.JSON(http.StatusOK, tableDetailResponse{
		Table:               table,
		SuggestedCollection: suggested,
	})
}

func tableExpose(e *core.RequestEvent) error {
	if !e.HasSuperuserAuth() {
		return e.ForbiddenError("Only administrators can expose database tables.", nil)
	}

	form := core.ExposeTableOptions{}
	if err := e.BindBody(&form); err != nil {
		return e.BadRequestError("Invalid request body.", err)
	}

	collection, err := e.App.ExposeNativeTable(form)
	if err != nil {
		return e.BadRequestError("Failed to expose table: "+err.Error(), err)
	}

	return e.JSON(http.StatusOK, collection)
}
