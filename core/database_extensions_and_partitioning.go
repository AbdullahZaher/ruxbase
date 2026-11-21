package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/pocketbase/dbx"
)

// ApprovedExtensions defines the whitelist of reviewed PostgreSQL extensions
// permitted to be managed dynamically by Ruxbase.
var ApprovedExtensions = map[string]bool{
	"vector":        true, // pgvector for semantic search & AI embeddings
	"pg_trgm":       true, // Trigram text similarity & fast fuzzy search
	"btree_gin":     true, // GIN indexing for scalar types
	"btree_gist":    true, // GiST indexing for scalar types & exclusion constraints
	"uuid-ossp":     true, // Standard UUID functions
	"pgcrypto":      true, // Cryptographic hash & cipher functions
	"unaccent":      true, // Accent-insensitive text normalization
	"fuzzystrmatch": true, // Phonetic matching algorithms (Levenshtein, Soundex)
}

var identifierRegex = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// NativeExtension represents PostgreSQL extension information from catalog.
type NativeExtension struct {
	Name             string `db:"name" json:"name"`
	DefaultVersion   string `db:"default_version" json:"defaultVersion"`
	InstalledVersion string `db:"installed_version" json:"installedVersion"`
	Comment          string `db:"comment" json:"comment"`
	Installed        bool   `db:"-" json:"installed"`
}

// PartitionInfo represents declarative partitioning metadata from PostgreSQL catalog.
type PartitionInfo struct {
	TableName  string   `json:"tableName"`
	Strategy   string   `json:"strategy"` // "RANGE", "LIST", "HASH"
	KeyColumns []string `json:"keyColumns"`
	Partitions []string `json:"partitions"`
}

// CreateIndexOptions defines configuration parameters for reviewed index creation.
type CreateIndexOptions struct {
	TableName      string
	IndexName      string
	Method         string // "btree", "gin", "gist", "hnsw", "ivfflat"
	Columns        []string
	Unique         bool
	Predicate      string // Optional WHERE clause for partial index
	VectorDistance string // Optional operator class (e.g. "vector_cosine_ops")
}

// ListAvailableExtensions returns all PostgreSQL extensions available in the cluster.
func (app *BaseApp) ListAvailableExtensions() ([]*NativeExtension, error) {
	extensions := []*NativeExtension{}

	err := app.ConcurrentDB().NewQuery(`
		SELECT
			[[name]],
			COALESCE([[default_version]], '') AS [[default_version]],
			COALESCE([[installed_version]], '') AS [[installed_version]],
			COALESCE([[comment]], '') AS [[comment]]
		FROM {{pg_available_extensions}}
		ORDER BY [[name]]
	`).All(&extensions)
	if err != nil {
		return nil, fmt.Errorf("failed to list available extensions: %w", err)
	}

	for _, ext := range extensions {
		ext.Installed = ext.InstalledVersion != ""
	}

	return extensions, nil
}

// ListInstalledExtensions returns only currently active/installed extensions.
func (app *BaseApp) ListInstalledExtensions() ([]*NativeExtension, error) {
	all, err := app.ListAvailableExtensions()
	if err != nil {
		return nil, err
	}

	installed := []*NativeExtension{}
	for _, ext := range all {
		if ext.Installed {
			installed = append(installed, ext)
		}
	}

	return installed, nil
}

// EnableExtension activates an approved extension in PostgreSQL.
func (app *BaseApp) EnableExtension(name string) error {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if !identifierRegex.MatchString(normalized) && normalized != "uuid-ossp" {
		return fmt.Errorf("invalid extension identifier: %q", name)
	}

	if !ApprovedExtensions[normalized] {
		return fmt.Errorf("unsupported or unapproved extension: %q (must be in approved whitelist)", name)
	}

	query := fmt.Sprintf("CREATE EXTENSION IF NOT EXISTS %s", quoteSimpleIdentifier(normalized))
	if _, err := app.DB().NewQuery(query).Execute(); err != nil {
		return fmt.Errorf("failed to enable extension %q: %w", name, err)
	}

	return nil
}

// DisableExtension drops an approved extension from PostgreSQL.
func (app *BaseApp) DisableExtension(name string) error {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if !identifierRegex.MatchString(normalized) && normalized != "uuid-ossp" {
		return fmt.Errorf("invalid extension identifier: %q", name)
	}

	if !ApprovedExtensions[normalized] {
		return fmt.Errorf("unsupported or unapproved extension: %q", name)
	}

	query := fmt.Sprintf("DROP EXTENSION IF EXISTS %s CASCADE", quoteSimpleIdentifier(normalized))
	if _, err := app.DB().NewQuery(query).Execute(); err != nil {
		return fmt.Errorf("failed to disable extension %q: %w", name, err)
	}

	return nil
}

// IntrospectPartitionInfo inspects declarative partitioning metadata for a table.
func (app *BaseApp) IntrospectPartitionInfo(tableName string) (*PartitionInfo, error) {
	if err := ValidateExposedTableName(tableName); err != nil {
		return nil, err
	}

	type partRow struct {
		PartStrat string `db:"partstrat"`
		KeysJSON  string `db:"keys_json"`
	}

	var row partRow
	err := app.ConcurrentDB().NewQuery(`
		SELECT
			p.[[partstrat]],
			COALESCE((
				SELECT json_agg(a.[[attname]] ORDER BY u.[[ord]])
				FROM UNNEST(p.[[partattrs]]) WITH ORDINALITY AS u([[attnum]], [[ord]])
				JOIN {{pg_attribute}} a ON a.[[attrelid]] = c.[[oid]] AND a.[[attnum]] = u.[[attnum]]
			)::text, '[]') AS [[keys_json]]
		FROM {{pg_class}} c
		JOIN {{pg_partitioned_table}} p ON p.[[partrelid]] = c.[[oid]]
		WHERE c.[[oid]] = to_regclass(quote_ident({:tableName}))
	`).Bind(dbx.Params{"tableName": tableName}).One(&row)

	if err != nil {
		return nil, fmt.Errorf("table %q is not a declarative partitioned table: %w", tableName, err)
	}

	strategy := "UNKNOWN"
	switch row.PartStrat {
	case "r":
		strategy = "RANGE"
	case "l":
		strategy = "LIST"
	case "h":
		strategy = "HASH"
	}

	var keyCols []string
	if row.KeysJSON != "" {
		_ = json.Unmarshal([]byte(row.KeysJSON), &keyCols)
	}

	// Fetch child partitions
	var childPartitions []string
	err = app.ConcurrentDB().NewQuery(`
		SELECT c.[[relname]]
		FROM {{pg_inherits}} i
		JOIN {{pg_class}} c ON c.[[oid]] = i.[[inhrelid]]
		WHERE i.[[inhparent]] = to_regclass(quote_ident({:tableName}))
		ORDER BY c.[[relname]]
	`).Bind(dbx.Params{"tableName": tableName}).Column(&childPartitions)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch child partitions for %q: %w", tableName, err)
	}

	return &PartitionInfo{
		TableName:  tableName,
		Strategy:   strategy,
		KeyColumns: keyCols,
		Partitions: childPartitions,
	}, nil
}

// CreateNativeIndex creates a reviewed index on a table using PostgreSQL native DDL.
func (app *BaseApp) CreateNativeIndex(opts CreateIndexOptions) error {
	if err := ValidateExposedTableName(opts.TableName); err != nil {
		return err
	}

	if !identifierRegex.MatchString(opts.IndexName) {
		return fmt.Errorf("invalid index name: %q", opts.IndexName)
	}

	if len(opts.Columns) == 0 {
		return errors.New("at least one column must be specified for index")
	}

	method := strings.ToLower(strings.TrimSpace(opts.Method))
	if method == "" {
		method = "btree"
	}

	switch method {
	case "btree", "gin", "gist", "hnsw", "ivfflat":
		// approved methods
	default:
		return fmt.Errorf("unsupported index method: %q (allowed: btree, gin, gist, hnsw, ivfflat)", opts.Method)
	}

	var colsBuilder strings.Builder
	for i, col := range opts.Columns {
		if !identifierRegex.MatchString(col) {
			return fmt.Errorf("invalid column identifier in index: %q", col)
		}
		if i > 0 {
			colsBuilder.WriteString(", ")
		}
		colsBuilder.WriteString(fmt.Sprintf("%q", col))
		if opts.VectorDistance != "" && (method == "hnsw" || method == "ivfflat") {
			colsBuilder.WriteString(" " + opts.VectorDistance)
		}
	}

	uniqueClause := ""
	if opts.Unique {
		if method != "btree" {
			return fmt.Errorf("unique index is only supported with btree method, got %q", method)
		}
		uniqueClause = "UNIQUE "
	}

	whereClause := ""
	if opts.Predicate != "" {
		whereClause = " WHERE " + opts.Predicate
	}

	query := fmt.Sprintf(
		"CREATE %sINDEX IF NOT EXISTS %q ON %q USING %s (%s)%s",
		uniqueClause,
		opts.IndexName,
		opts.TableName,
		method,
		colsBuilder.String(),
		whereClause,
	)

	if _, err := app.DB().NewQuery(query).Execute(); err != nil {
		return fmt.Errorf("failed to create index %q: %w", opts.IndexName, err)
	}

	return nil
}

// DropNativeIndex safely drops a named index.
func (app *BaseApp) DropNativeIndex(indexName string) error {
	if !identifierRegex.MatchString(indexName) {
		return fmt.Errorf("invalid index name: %q", indexName)
	}

	query := fmt.Sprintf("DROP INDEX IF EXISTS %q", indexName)
	if _, err := app.DB().NewQuery(query).Execute(); err != nil {
		return fmt.Errorf("failed to drop index %q: %w", indexName, err)
	}

	return nil
}

func quoteSimpleIdentifier(ident string) string {
	return `"` + strings.ReplaceAll(ident, `"`, `""`) + `"`
}
