package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadExplicitPreparedConfig(t *testing.T) {
	const valid = `{"main":{"role":"runtime","schemas":[{"role":"runtime","schema":"public","extended":true,"grants":[]}]},"auxiliary":{"role":"runtime","schemas":[{"role":"runtime","schema":"_aux","extended":true,"grants":[]}]},"identities":{"runtime":"runtime","migration":"migration","provisioning":"provisioner"}}`
	path := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(path, []byte(valid), 0600); err != nil {
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
		config.PreparedManifestJSON != valid || config.RealtimeSource != "hooks" ||
		routes.AuthCollection != "users" || len(routes.RecordCollections) != 2 {
		t.Fatalf("explicit fixture configuration rejected: %v", err)
	}
	checkArgs := []string{"--check-only", "--auth-collection=users", "--record-collections=items"}
	if _, _, port, err := loadConfig(checkArgs, get); err != nil || port != 0 {
		t.Fatalf("listener-free readiness configuration rejected: %v", err)
	}
	if _, _, _, err := loadConfig(append(checkArgs, "--port=8091"), get); err == nil {
		t.Fatal("check-only accepted a serving port")
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
	for _, raw := range []string{
		"{}",
		strings.Replace(valid, `,"identities":{"runtime":"runtime","migration":"migration","provisioning":"provisioner"}`, "", 1),
		strings.Replace(valid, `"migration":"migration"`, `"migration":"runtime"`, 1),
	} {
		if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := loadConfig(args, get); err == nil {
			t.Fatal("invalid or absent identity manifest accepted")
		}
	}
	if err := os.WriteFile(path, []byte(valid), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := loadConfig([]string{"--port=0", "--auth-collection=users", "--record-collections=items"}, get); err == nil {
		t.Fatal("implicit port accepted")
	}
}
