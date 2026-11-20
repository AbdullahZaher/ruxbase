package core

import (
	"strings"
	"testing"
)

func TestParsePreparedManifest(t *testing.T) {
	valid := `{"main":{"role":"runtime","schemas":[{"role":"runtime","schema":"public","extended":true,"grants":[]}]},"auxiliary":{"role":"runtime","schemas":[{"role":"runtime","schema":"_aux","extended":true,"grants":[]}]}}`
	if _, err := parsePreparedManifest(valid, "runtime"); err != nil {
		t.Fatal(err)
	}
	withIdentities := strings.TrimSuffix(valid, "}") + `,"identities":{"runtime":"runtime","migration":"migration","provisioning":"provisioning","replication":"wal"}}`
	if _, err := parsePreparedManifest(withIdentities, "runtime"); err != nil {
		t.Fatal(err)
	}
	if _, err := parsePreparedManifest(strings.Replace(withIdentities, `"migration":"migration"`, `"migration":"runtime"`, 1), "runtime"); err == nil {
		t.Fatal("shared manifest identities accepted")
	}
	for _, raw := range []string{
		"",
		"{}",
		`{"main":{"role":"runtime","schemas":[]}}`,
		valid + " {}",
		`{"main":{"role":"other","schemas":[{}]},"auxiliary":{"role":"runtime","schemas":[{}]}}`,
		`{"main":{"role":"runtime","schemas":[{}]},"auxiliary":{"role":"runtime","schemas":[{}]},"unrecognized":true}`,
	} {
		if _, err := parsePreparedManifest(raw, "runtime"); err == nil {
			t.Fatalf("invalid manifest accepted: %s", raw)
		}
	}
}

func TestPreparedManifestCannotFallBackToLegacy(t *testing.T) {
	app := NewBaseApp(BaseAppConfig{DatabaseURL: "unused-test-config", PreparedManifestJSON: "{}"})
	if err := app.Bootstrap(); err == nil || app.IsBootstrapped() {
		t.Fatal("manifest without explicit identity fell through to legacy bootstrap")
	}
}
