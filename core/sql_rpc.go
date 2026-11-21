package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/AbdullahZaher/ruxbase/core/cluster"
	"github.com/pocketbase/dbx"
)

const DefaultRPCFunctionsTable = "_rpc_functions"

// RPCParam defines an individual argument for a PostgreSQL stored procedure.
type RPCParam struct {
	Name     string `json:"name"`
	DataType string `json:"dataType"`
}

// RPCFunction represents an introspected PostgreSQL stored procedure / function.
type RPCFunction struct {
	Name        string      `db:"name" json:"name"`
	Schema      string      `db:"schema" json:"schema"`
	ReturnType  string      `db:"return_type" json:"returnType"`
	ReturnsSet  bool        `db:"returns_set" json:"returnsSet"`
	Volatility  string      `db:"volatility" json:"volatility"` // "VOLATILE", "STABLE", "IMMUTABLE"
	Arguments   string      `db:"arguments" json:"arguments"`
	Parameters  []RPCParam  `db:"-" json:"parameters"`
	IsExposed   bool        `db:"-" json:"isExposed"`
	AccessRule  *string     `db:"access_rule" json:"accessRule"` // nil for superuser only, "" for public
	ParamsJSON  string      `db:"params_json" json:"-"`
	ArgTypes    string      `db:"arg_types" json:"-"`
	ArgNamesStr string      `db:"arg_names" json:"-"`
}

// ExposeFunctionOptions defines configuration for exposing an RPC function.
type ExposeFunctionOptions struct {
	FunctionName string  `json:"functionName"`
	Schema       string  `json:"schema"`
	AccessRule   *string `json:"accessRule"`
}

func (app *BaseApp) initRPCFunctionsTable() error {
	return app.RunInTransaction(func(txApp App) error {
		if err := cluster.XactLock(txApp.DB(), "rpc:"+DefaultRPCFunctionsTable); err != nil {
			return err
		}

		query := fmt.Sprintf(`
			CREATE TABLE IF NOT EXISTS {{%s}} (
				[[name]] text PRIMARY KEY,
				[[schema]] text NOT NULL DEFAULT 'public',
				[[access_rule]] text,
				[[created]] bigint NOT NULL,
				[[updated]] bigint NOT NULL
			);
		`, DefaultRPCFunctionsTable)

		_, err := txApp.DB().NewQuery(query).Execute()
		return err
	})
}

// ListRPCFunctions lists all candidate user-defined functions in the specified PostgreSQL schema.
func (app *BaseApp) ListRPCFunctions(schema string) ([]*RPCFunction, error) {
	if schema == "" {
		schema = "public"
	}

	if err := app.initRPCFunctionsTable(); err != nil {
		return nil, err
	}

	functions := []*RPCFunction{}

	err := app.ConcurrentDB().NewQuery(`
		SELECT
			p.[[proname]] AS [[name]],
			n.[[nspname]] AS [[schema]],
			format_type(p.[[prorettype]], null) AS [[return_type]],
			p.[[proretset]] AS [[returns_set]],
			CASE p.[[provolatile]]
				WHEN 'i' THEN 'IMMUTABLE'
				WHEN 's' THEN 'STABLE'
				ELSE 'VOLATILE'
			END AS [[volatility]],
			COALESCE(pg_get_function_arguments(p.[[oid]]), '') AS [[arguments]],
			COALESCE(array_to_json(p.[[proargnames]])::text, '[]') AS [[arg_names]],
			COALESCE((
				SELECT json_agg(format_type(t.[[oid]], null))
				FROM unnest(p.[[proargtypes]]) WITH ORDINALITY AS u([[oid]], [[ord]])
				JOIN {{pg_type}} t ON t.[[oid]] = u.[[oid]]
			)::text, '[]') AS [[arg_types]],
			e.[[access_rule]]
		FROM {{pg_proc}} p
		JOIN {{pg_namespace}} n ON n.[[oid]] = p.[[pronamespace]]
		LEFT JOIN {{` + DefaultRPCFunctionsTable + `}} e ON e.[[name]] = p.[[proname]] AND e.[[schema]] = n.[[nspname]]
		WHERE n.[[nspname]] = {:schema}
			AND p.[[prokind]] IN ('f', 'p')
			AND NOT starts_with(p.[[proname]], 'pg_')
			AND NOT starts_with(p.[[proname]], '_')
		ORDER BY p.[[proname]]
	`).Bind(dbx.Params{"schema": schema}).All(&functions)

	if err != nil {
		return nil, fmt.Errorf("failed to list RPC functions: %w", err)
	}

	for _, fn := range functions {
		parseRPCParameters(fn)
		// Function is exposed if record exists in _rpc_functions
		var count int
		_ = app.ConcurrentDB().Select("count(*)").
			From(DefaultRPCFunctionsTable).
			Where(dbx.HashExp{"name": fn.Name, "schema": fn.Schema}).
			Row(&count)
		fn.IsExposed = count > 0
	}

	return functions, nil
}

// IntrospectRPCFunction inspects a single named stored procedure.
func (app *BaseApp) IntrospectRPCFunction(name string) (*RPCFunction, error) {
	if !identifierRegex.MatchString(name) {
		return nil, fmt.Errorf("invalid function name: %q", name)
	}

	if err := app.initRPCFunctionsTable(); err != nil {
		return nil, err
	}

	fn := &RPCFunction{}
	err := app.ConcurrentDB().NewQuery(`
		SELECT
			p.[[proname]] AS [[name]],
			n.[[nspname]] AS [[schema]],
			format_type(p.[[prorettype]], null) AS [[return_type]],
			p.[[proretset]] AS [[returns_set]],
			CASE p.[[provolatile]]
				WHEN 'i' THEN 'IMMUTABLE'
				WHEN 's' THEN 'STABLE'
				ELSE 'VOLATILE'
			END AS [[volatility]],
			COALESCE(pg_get_function_arguments(p.[[oid]]), '') AS [[arguments]],
			COALESCE(array_to_json(p.[[proargnames]])::text, '[]') AS [[arg_names]],
			COALESCE((
				SELECT json_agg(format_type(t.[[oid]], null))
				FROM unnest(p.[[proargtypes]]) WITH ORDINALITY AS u([[oid]], [[ord]])
				JOIN {{pg_type}} t ON t.[[oid]] = u.[[oid]]
			)::text, '[]') AS [[arg_types]],
			e.[[access_rule]]
		FROM {{pg_proc}} p
		JOIN {{pg_namespace}} n ON n.[[oid]] = p.[[pronamespace]]
		LEFT JOIN {{` + DefaultRPCFunctionsTable + `}} e ON e.[[name]] = p.[[proname]]
		WHERE p.[[proname]] = {:name}
			AND n.[[nspname]] NOT IN ('pg_catalog', 'information_schema')
		LIMIT 1
	`).Bind(dbx.Params{"name": name}).One(fn)

	if err != nil {
		return nil, fmt.Errorf("function %q not found or not accessible: %w", name, err)
	}

	parseRPCParameters(fn)

	var count int
	_ = app.ConcurrentDB().Select("count(*)").
		From(DefaultRPCFunctionsTable).
		Where(dbx.HashExp{"name": fn.Name}).
		Row(&count)
	fn.IsExposed = count > 0

	return fn, nil
}

func parseRPCParameters(fn *RPCFunction) {
	var names []string
	var types []string

	if fn.ArgNamesStr != "" {
		_ = json.Unmarshal([]byte(fn.ArgNamesStr), &names)
	}
	if fn.ArgTypes != "" {
		_ = json.Unmarshal([]byte(fn.ArgTypes), &types)
	}

	inputCount := len(types)
	fn.Parameters = []RPCParam{}
	for i := 0; i < inputCount; i++ {
		name := fmt.Sprintf("p%d", i)
		if i < len(names) {
			name = names[i]
		}
		t := types[i]
		fn.Parameters = append(fn.Parameters, RPCParam{
			Name:     name,
			DataType: t,
		})
	}
}

// ExposeRPCFunction registers a function in the exposure ledger with an access rule.
func (app *BaseApp) ExposeRPCFunction(opts ExposeFunctionOptions) (*RPCFunction, error) {
	fn, err := app.IntrospectRPCFunction(opts.FunctionName)
	if err != nil {
		return nil, err
	}

	schema := opts.Schema
	if schema == "" {
		schema = fn.Schema
	}

	now := time.Now().UnixMicro()

	err = app.RunInTransaction(func(txApp App) error {
		// Upsert into _rpc_functions
		var exists int
		_ = txApp.DB().Select("(1)").
			From(DefaultRPCFunctionsTable).
			Where(dbx.HashExp{"name": fn.Name}).
			Row(&exists)

		if exists > 0 {
			_, err := txApp.DB().Update(DefaultRPCFunctionsTable, dbx.Params{
				"schema":      schema,
				"access_rule": opts.AccessRule,
				"updated":     now,
			}, dbx.HashExp{"name": fn.Name}).Execute()
			return err
		}

		_, err := txApp.DB().Insert(DefaultRPCFunctionsTable, dbx.Params{
			"name":        fn.Name,
			"schema":      schema,
			"access_rule": opts.AccessRule,
			"created":     now,
			"updated":     now,
		}).Execute()
		return err
	})

	if err != nil {
		return nil, fmt.Errorf("failed to expose RPC function %q: %w", fn.Name, err)
	}

	fn.IsExposed = true
	fn.AccessRule = opts.AccessRule
	return fn, nil
}

// UnexposeRPCFunction revokes API exposure for a function.
func (app *BaseApp) UnexposeRPCFunction(name string) error {
	if err := app.initRPCFunctionsTable(); err != nil {
		return err
	}

	_, err := app.DB().Delete(DefaultRPCFunctionsTable, dbx.HashExp{"name": name}).Execute()
	return err
}

// ExecuteRPC executes an exposed PostgreSQL function with parameter binding and authorization.
func (app *BaseApp) ExecuteRPC(name string, args map[string]any, authRecord *Record, hasSuperuserAuth bool) (any, error) {
	fn, err := app.IntrospectRPCFunction(name)
	if err != nil {
		return nil, err
	}

	if !fn.IsExposed {
		return nil, fmt.Errorf("function %q is not exposed for RPC execution", name)
	}

	// 1. Authorization evaluation
	if !hasSuperuserAuth {
		if fn.AccessRule == nil {
			return nil, errors.New("unauthorized: function requires superuser privileges")
		}
		if *fn.AccessRule != "" {
			// Requires authentication
			if authRecord == nil {
				return nil, errors.New("unauthorized: authentication required to execute this function")
			}
		}
	}

	// 2. Build parameterized query
	params := dbx.Params{}
	callArgs := make([]string, len(fn.Parameters))

	for i, p := range fn.Parameters {
		val, exists := args[p.Name]
		if !exists {
			return nil, fmt.Errorf("missing argument %q for function %q", p.Name, name)
		}
		placeholder := fmt.Sprintf("arg_%d", i)
		params[placeholder] = val
		callArgs[i] = "{:" + placeholder + "}"
	}

	var querySQL string
	if fn.ReturnsSet {
		querySQL = fmt.Sprintf("SELECT * FROM %q(%s)", fn.Name, strings.Join(callArgs, ", "))
		var rawRows []dbx.NullStringMap
		err = app.DB().NewQuery(querySQL).Bind(params).All(&rawRows)
		if err != nil {
			return nil, fmt.Errorf("function execution failed: %w", err)
		}
		rows := make([]map[string]any, len(rawRows))
		for i, r := range rawRows {
			m := make(map[string]any, len(r))
			for k, v := range r {
				if v.Valid {
					m[k] = v.String
				} else {
					m[k] = nil
				}
			}
			rows[i] = m
		}
		return rows, nil
	}

	// Scalar or composite single row
	querySQL = fmt.Sprintf("SELECT %q(%s) AS result", fn.Name, strings.Join(callArgs, ", "))
	var result any
	err = app.DB().NewQuery(querySQL).Bind(params).Row(&result)
	if err != nil {
		return nil, fmt.Errorf("function execution failed: %w", err)
	}

	return result, nil
}
