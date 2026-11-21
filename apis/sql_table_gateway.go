package apis

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/tools/router"
)

// bindSQLTableGateway registers endpoints for querying exposed tables and performing bounded bulk mutations.
func bindSQLTableGateway(app core.App, rg *router.RouterGroup[*core.RequestEvent]) {
	subGroup := rg.Group("/ruxbase/sql/tables")
	subGroup.GET("/{table}", sqlTableQuery)
	subGroup.POST("/{table}/bulk", sqlTableBulkInsert)
	subGroup.DELETE("/{table}/bulk", sqlTableBulkDelete)
}

func sqlTableQuery(e *core.RequestEvent) error {
	tableName := e.Request.PathValue("table")
	if err := core.ValidateExposedTableName(tableName); err != nil {
		return e.BadRequestError(err.Error(), err)
	}

	page, _ := strconv.Atoi(e.Request.URL.Query().Get("page"))
	perPage, _ := strconv.Atoi(e.Request.URL.Query().Get("perPage"))
	sort := strings.TrimSpace(e.Request.URL.Query().Get("sort"))
	filter := strings.TrimSpace(e.Request.URL.Query().Get("filter"))

	principal := PrincipalFromEvent(e)

	params := core.TableQueryParams{
		Page:    page,
		PerPage: perPage,
		Sort:    sort,
		Filter:  filter,
	}

	res, err := e.App.QueryExposedTable(principal, tableName, params)
	if err != nil {
		if errors.Is(err, core.ErrTableNotExposed) {
			return e.NotFoundError("Table not found or not exposed.", err)
		}
		if errors.Is(err, core.ErrTableAccessForbidden) {
			if principal.Type() == core.PrincipalTypeAnonymous {
				return e.UnauthorizedError("Authentication required to access this table.", err)
			}
			return e.ForbiddenError("Access to this table is forbidden.", err)
		}
		return e.BadRequestError("Failed to query table: "+err.Error(), err)
	}

	return e.JSON(http.StatusOK, res)
}

func sqlTableBulkInsert(e *core.RequestEvent) error {
	tableName := e.Request.PathValue("table")
	if err := core.ValidateExposedTableName(tableName); err != nil {
		return e.BadRequestError(err.Error(), err)
	}

	var rows []map[string]any
	if err := e.BindBody(&rows); err != nil {
		return e.BadRequestError("Invalid bulk insert payload; expected JSON array of objects.", err)
	}

	principal := PrincipalFromEvent(e)

	res, err := e.App.BulkInsertExposedTable(principal, tableName, rows)
	if err != nil {
		if errors.Is(err, core.ErrTableNotExposed) {
			return e.NotFoundError("Table not found or not exposed.", err)
		}
		if errors.Is(err, core.ErrBulkLimitExceeded) {
			return e.BadRequestError(err.Error(), err)
		}
		if errors.Is(err, core.ErrTableAccessForbidden) {
			if principal.Type() == core.PrincipalTypeAnonymous {
				return e.UnauthorizedError("Authentication required for bulk operations.", err)
			}
			return e.ForbiddenError("Bulk operation forbidden on this table.", err)
		}
		return e.BadRequestError("Bulk insert failed: "+err.Error(), err)
	}

	return e.JSON(http.StatusOK, res)
}

func sqlTableBulkDelete(e *core.RequestEvent) error {
	tableName := e.Request.PathValue("table")
	if err := core.ValidateExposedTableName(tableName); err != nil {
		return e.BadRequestError(err.Error(), err)
	}

	var payload struct {
		Ids []string `json:"ids"`
	}
	if err := e.BindBody(&payload); err != nil {
		return e.BadRequestError("Invalid bulk delete payload; expected { \"ids\": [...] }.", err)
	}

	principal := PrincipalFromEvent(e)

	res, err := e.App.BulkDeleteExposedTable(principal, tableName, payload.Ids)
	if err != nil {
		if errors.Is(err, core.ErrTableNotExposed) {
			return e.NotFoundError("Table not found or not exposed.", err)
		}
		if errors.Is(err, core.ErrBulkLimitExceeded) {
			return e.BadRequestError(err.Error(), err)
		}
		if errors.Is(err, core.ErrTableAccessForbidden) {
			if principal.Type() == core.PrincipalTypeAnonymous {
				return e.UnauthorizedError("Authentication required for bulk operations.", err)
			}
			return e.ForbiddenError("Bulk operation forbidden on this table.", err)
		}
		return e.BadRequestError("Bulk delete failed: "+err.Error(), err)
	}

	return e.JSON(http.StatusOK, res)
}
