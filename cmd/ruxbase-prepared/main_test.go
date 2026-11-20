package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadExplicitPreparedConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{
		"RUXBASE_PREPARED_DATABASE_URL":  "fixture-main",
		"RUXBASE_PREPARED_LOGS_URL":      "fixture-aux",
		"RUXBASE_PREPARED_ROLE":          "runtime",
		"RUXBASE_PREPARED_DATA_DIR":      t.TempDir(),
		"RUXBASE_PREPARED_MANIFEST_FILE": path,
	}
	get := func(key string) string { return env[key] }
	args := []string{"--port=8091", "--auth-collection=users", "--record-collections=items,notes"}
	config, routes, port, err := loadConfig(args, get)
	if err != nil || port != 8091 || config.PreparedRuntimeRole != "runtime" ||
		config.PreparedManifestJSON != "{}" || config.RealtimeSource != "hooks" ||
		routes.AuthCollection != "users" || len(routes.RecordCollections) != 2 {
		t.Fatalf("explicit fixture configuration rejected: %v", err)
	}
	for _, key := range []string{
		"RUXBASE_PREPARED_DATABASE_URL",
		"RUXBASE_PREPARED_LOGS_URL",
		"RUXBASE_PREPARED_ROLE",
		"RUXBASE_PREPARED_DATA_DIR",
		"RUXBASE_PREPARED_MANIFEST_FILE",
	} {
		value := env[key]
		delete(env, key)
		if _, _, _, err := loadConfig(args, get); err == nil {
			t.Fatal("missing explicit configuration accepted")
		}
		env[key] = value
	}
	if _, _, _, err := loadConfig([]string{"--port=0", "--auth-collection=users", "--record-collections=items"}, get); err == nil {
		t.Fatal("implicit port accepted")
	}
}
