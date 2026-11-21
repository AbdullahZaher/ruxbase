package core

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/AbdullahZaher/ruxbase/tools/security"
	"github.com/pocketbase/dbx"
)

const (
	MaxBulkOperationsLimit = 500
	DefaultPageSize        = 30
	MaxPageSize            = 500
)

var (
	ErrTableNotExposed      = errors.New("table is not exposed via API")
	ErrBulkLimitExceeded    = errors.New("bulk operation exceeds maximum limit of 500 items")
	ErrTableAccessForbidden = errors.New("caller is not authorized to query this table")
)

// TableQueryParams defines parameters for filtering, sorting, and paginating exposed table queries.
type TableQueryParams struct {
	Page    int
	PerPage int
	Sort    string
	Filter  string
	Params  map[string]any
}

// TablePaginatedResult defines the standard envelope for paginated table rows.
type TablePaginatedResult struct {
	Page       int              `json:"page"`
	PerPage    int              `json:"perPage"`
	TotalItems int              `json:"totalItems"`
	TotalPages int              `json:"totalPages"`
	Items      []map[string]any `json:"items"`
}

// BulkResult defines the summary result of a bounded bulk mutation.
type BulkResult struct {
	Affected int      `json:"affected"`
	Ids      []string `json:"ids"`
}

// QueryExposedTable performs a safe, paginated, RLS-governed query against an exposed native table.
func (app *BaseApp) QueryExposedTable(principal Principal, tableName string, queryParams TableQueryParams) (*TablePaginatedResult, error) {
	if principal == nil {
		principal = &AnonymousPrincipal{}
	}

	// 1. Verify table is explicitly exposed
	exp, err := app.GetTableExposure(tableName)
	if err != nil || !exp.IsExposed {
		return nil, ErrTableNotExposed
	}

	// 2. Validate authorization rule if not superuser
	if principal.Type() != PrincipalTypeSuperuser {
		if exp.AccessRule == nil {
			return nil, ErrTableAccessForbidden
		}
		// If empty string rule: public access
		if *exp.AccessRule != "" {
			// Require at least authenticated principal
			if principal.Type() == PrincipalTypeAnonymous {
				return nil, ErrTableAccessForbidden
			}
		}
	}

	// 3. Normalize pagination parameters
	page := queryParams.Page
	if page < 1 {
		page = 1
	}
	perPage := queryParams.PerPage
	if perPage < 1 {
		perPage = DefaultPageSize
	} else if perPage > MaxPageSize {
		perPage = MaxPageSize
	}

	offset := (page - 1) * perPage

	quotedTable := quoteRLSIdent(tableName)

	result := &TablePaginatedResult{
		Page:    page,
		PerPage: perPage,
		Items:   make([]map[string]any, 0),
	}

	// 4. Execute query inside scoped transaction with principal
	var runInTx func(func(txApp App) error) error
	if principal.Type() == PrincipalTypeSuperuser {
		runInTx = app.RunInTransaction
	} else {
		var userRecord *Record
		if principal.Type() == PrincipalTypeUser && principal.AuthCollection() != "" {
			userRecord, _ = app.FindRecordById(principal.AuthCollection(), principal.Id())
		}
		role := RLSAuthenticatedRole
		if principal.Type() == PrincipalTypeAnonymous {
			role = RLSAnonRole
		}
		scope := TransactionScope{
			Principal: userRecord,
			Role:      role,
			Rule:      RLSRuleList,
			TenantID:  principal.TenantId(),
			ReadOnly:  true,
		}
		runInTx = func(fn func(txApp App) error) error {
			return app.RunInScopedTransaction(scope, fn)
		}
	}

	err = runInTx(func(txApp App) error {
		// Count total items
		countQuery := fmt.Sprintf("SELECT COUNT(*) FROM %s", quotedTable)
		var total int
		if err := txApp.DB().NewQuery(countQuery).Row(&total); err != nil {
			return fmt.Errorf("failed to count rows: %w", err)
		}

		result.TotalItems = total
		result.TotalPages = int(math.Ceil(float64(total) / float64(perPage)))

		if total == 0 {
			return nil
		}

		// Build SELECT query
		sb := strings.Builder{}
		sb.WriteString(fmt.Sprintf("SELECT * FROM %s", quotedTable))

		// Apply safe sorting
		if queryParams.Sort != "" {
			parts := strings.Split(queryParams.Sort, ",")
			sortClauses := make([]string, 0, len(parts))
			for _, part := range parts {
				part = strings.TrimSpace(part)
				if part == "" {
					continue
				}
				dir := "ASC"
				col := part
				if strings.HasPrefix(part, "-") {
					dir = "DESC"
					col = strings.TrimPrefix(part, "-")
				} else if strings.HasPrefix(part, "+") {
					col = strings.TrimPrefix(part, "+")
				}
				sortClauses = append(sortClauses, fmt.Sprintf("%s %s", quoteRLSIdent(col), dir))
			}
			if len(sortClauses) > 0 {
				sb.WriteString(" ORDER BY " + strings.Join(sortClauses, ", "))
			}
		}

		sb.WriteString(fmt.Sprintf(" LIMIT %d OFFSET %d", perPage, offset))

		rows, err := txApp.DB().NewQuery(sb.String()).Rows()
		if err != nil {
			return fmt.Errorf("failed to execute table query: %w", err)
		}
		defer rows.Close()

		cols, err := rows.Columns()
		if err != nil {
			return err
		}

		for rows.Next() {
			rowValues := make([]any, len(cols))
			rowPointers := make([]any, len(cols))
			for i := range rowValues {
				rowPointers[i] = &rowValues[i]
			}

			if err := rows.Scan(rowPointers...); err != nil {
				return err
			}

			rowMap := make(map[string]any, len(cols))
			for i, col := range cols {
				val := rowValues[i]
				if b, ok := val.([]byte); ok {
					rowMap[col] = string(b)
				} else {
					rowMap[col] = val
				}
			}
			result.Items = append(result.Items, rowMap)
		}

		return rows.Err()
	})

	if err != nil {
		return nil, err
	}

	return result, nil
}

// BulkInsertExposedTable performs a bounded, atomic multi-row insert into an exposed native table.
func (app *BaseApp) BulkInsertExposedTable(principal Principal, tableName string, rows []map[string]any) (*BulkResult, error) {
	if principal == nil {
		return nil, ErrTableAccessForbidden
	}

	if len(rows) == 0 {
		return &BulkResult{Affected: 0, Ids: nil}, nil
	}

	if len(rows) > MaxBulkOperationsLimit {
		return nil, ErrBulkLimitExceeded
	}

	// Verify table is exposed
	exp, err := app.GetTableExposure(tableName)
	if err != nil || !exp.IsExposed {
		return nil, ErrTableNotExposed
	}

	// Verify authorization
	if principal.Type() != PrincipalTypeSuperuser {
		if exp.AccessRule == nil || *exp.AccessRule == "" && principal.Type() == PrincipalTypeAnonymous {
			return nil, ErrTableAccessForbidden
		}
	}

	result := &BulkResult{
		Ids: make([]string, 0, len(rows)),
	}

	var runInTx func(func(txApp App) error) error
	if principal.Type() == PrincipalTypeSuperuser {
		runInTx = app.RunInTransaction
	} else {
		var userRecord *Record
		if principal.Type() == PrincipalTypeUser && principal.AuthCollection() != "" {
			userRecord, _ = app.FindRecordById(principal.AuthCollection(), principal.Id())
		}
		scope := TransactionScope{
			Principal: userRecord,
			Role:      RLSAuthenticatedRole,
			Rule:      RLSRuleCreate,
			TenantID:  principal.TenantId(),
		}
		runInTx = func(fn func(txApp App) error) error {
			return app.RunInScopedTransaction(scope, fn)
		}
	}

	err = runInTx(func(txApp App) error {
		// Set transaction timeout limits
		if _, err := txApp.DB().NewQuery("SET LOCAL statement_timeout = '15s'; SET LOCAL lock_timeout = '3s';").Execute(); err != nil {
			return err
		}

		quotedTable := quoteRLSIdent(tableName)

		for _, row := range rows {
			// If id not specified, autogenerate
			idVal, ok := row["id"].(string)
			if !ok || idVal == "" {
				idVal = security.RandomString(15)
				row["id"] = idVal
			}

			// If tenant_id not specified and principal belongs to a tenant, set it
			if principal.TenantId() != "" {
				if tVal, hasT := row["tenant_id"].(string); !hasT || tVal == "" {
					row["tenant_id"] = principal.TenantId()
				}
			}

			cols := make([]string, 0, len(row))
			paramNames := make([]string, 0, len(row))
			params := dbx.Params{}

			i := 0
			for col, val := range row {
				cols = append(cols, quoteRLSIdent(col))
				pName := fmt.Sprintf("p_%d", i)
				paramNames = append(paramNames, "{:"+pName+"}")
				params[pName] = val
				i++
			}

			insertSQL := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
				quotedTable,
				strings.Join(cols, ", "),
				strings.Join(paramNames, ", "),
			)

			_, err := txApp.DB().NewQuery(insertSQL).Bind(params).Execute()
			if err != nil {
				return err
			}

			result.Affected++
			result.Ids = append(result.Ids, idVal)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return result, nil
}

// BulkDeleteExposedTable performs a bounded, atomic multi-row deletion on an exposed native table.
func (app *BaseApp) BulkDeleteExposedTable(principal Principal, tableName string, ids []string) (*BulkResult, error) {
	if principal == nil {
		return nil, ErrTableAccessForbidden
	}

	if len(ids) == 0 {
		return &BulkResult{Affected: 0, Ids: nil}, nil
	}

	if len(ids) > MaxBulkOperationsLimit {
		return nil, ErrBulkLimitExceeded
	}

	exp, err := app.GetTableExposure(tableName)
	if err != nil || !exp.IsExposed {
		return nil, ErrTableNotExposed
	}

	if principal.Type() != PrincipalTypeSuperuser {
		if exp.DeleteRule == nil {
			return nil, ErrTableAccessForbidden
		}
	}

	result := &BulkResult{
		Ids: ids,
	}

	var runInTx func(func(txApp App) error) error
	if principal.Type() == PrincipalTypeSuperuser {
		runInTx = app.RunInTransaction
	} else {
		var userRecord *Record
		if principal.Type() == PrincipalTypeUser && principal.AuthCollection() != "" {
			userRecord, _ = app.FindRecordById(principal.AuthCollection(), principal.Id())
		}
		scope := TransactionScope{
			Principal: userRecord,
			Role:      RLSAuthenticatedRole,
			Rule:      RLSRuleDelete,
			TenantID:  principal.TenantId(),
		}
		runInTx = func(fn func(txApp App) error) error {
			return app.RunInScopedTransaction(scope, fn)
		}
	}

	err = runInTx(func(txApp App) error {
		if _, err := txApp.DB().NewQuery("SET LOCAL statement_timeout = '15s'; SET LOCAL lock_timeout = '3s';").Execute(); err != nil {
			return err
		}

		quotedTable := quoteRLSIdent(tableName)

		idParams := make([]string, len(ids))
		params := dbx.Params{}
		for i, id := range ids {
			pName := fmt.Sprintf("id_%d", i)
			idParams[i] = "{:" + pName + "}"
			params[pName] = id
		}

		delSQL := fmt.Sprintf("DELETE FROM %s WHERE id IN (%s)", quotedTable, strings.Join(idParams, ", "))

		res, err := txApp.DB().NewQuery(delSQL).Bind(params).Execute()
		if err != nil {
			return err
		}

		ra, _ := res.RowsAffected()
		result.Affected = int(ra)
		return nil
	})

	if err != nil {
		return nil, err
	}

	return result, nil
}
