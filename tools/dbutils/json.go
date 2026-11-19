package dbutils

import (
	"fmt"
	"strings"
)

// ruxbase: the SQLite json functions are replaced by jsonb and the ruxbase_* SQL functions
// note: the helpers rely on the ruxbase_* SQL functions created by the
// ruxbase_functions system migration, which accept text, jsonb and any other
// column type and normalize the non-json values the same way as PocketBase.

// JSONEach returns a set returning SQL expression with a single "value"
// text column for each element of a json array column.
//
// Non-array and non-json column values are treated as a single element array.
//
// It is intended to be used as a FROM/JOIN item, eg.:
//
//	"LEFT JOIN " + JSONEach("tbl.col") + " je ON TRUE"
//
// ruxbase: jsonb_array_elements_text instead of json_each
func JSONEach(column string) string {
	return fmt.Sprintf(`jsonb_array_elements_text(ruxbase_json_array([[%s]]))`, column)
}

// JSONArrayLength returns a SQL expression with the number of elements
// of a json array column.
//
// It works with both json and non-json column values.
//
// Returns 0 for empty string or NULL column values.
//
// ruxbase: ruxbase_json_array_length instead of json_array_length
func JSONArrayLength(column string) string {
	return fmt.Sprintf(`ruxbase_json_array_length([[%s]])`, column)
}

// JSONExtract returns a SQL expression that extracts the jsonb value at the
// specified path of a json column (or the whole normalized jsonb value when path is empty).
//
// The path uses the PocketBase notation (eg. "a.b[0].c").
//
// NULL is returned for missing paths and for the json null value
// (the same as the SQLite JSON_EXTRACT).
//
// ruxbase: jsonb_path_query_first (see jsonPath) instead of JSON_EXTRACT
func JSONExtract(column string, path string) string {
	if path == "" {
		return fmt.Sprintf(`NULLIF(ruxbase_jsonb([[%s]]), 'null'::jsonb)`, column)
	}

	return fmt.Sprintf(
		`NULLIF(jsonb_path_query_first(ruxbase_jsonb([[%s]]), '%s', '{}', true), 'null'::jsonb)`,
		column,
		strings.ReplaceAll(jsonPath(path), "'", "''"),
	)
}

// jsonPath converts a PocketBase json path (eg. "a.b[0].c") to a strict
// Postgres SQL/JSON path expression (eg. strict $."a"."b"[0]."c").
func jsonPath(path string) string {
	var result strings.Builder
	result.WriteString("strict $")

	for _, part := range strings.Split(path, ".") {
		if part == "" {
			continue
		}

		key, rest, _ := strings.Cut(part, "[")
		if key != "" {
			result.WriteString(`."`)
			result.WriteString(strings.ReplaceAll(strings.ReplaceAll(key, `\`, `\\`), `"`, `\"`))
			result.WriteString(`"`)
		}

		if rest != "" {
			result.WriteString("[")
			result.WriteString(rest)
		}
	}

	return result.String()
}
