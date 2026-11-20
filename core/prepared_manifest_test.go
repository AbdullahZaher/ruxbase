package core

import "testing"

func TestParsePreparedManifest(t *testing.T) {
	valid := `{"main":{"role":"runtime","schemas":[{"role":"runtime","schema":"public","extended":true,"grants":[]}]},"auxiliary":{"role":"runtime","schemas":[{"role":"runtime","schema":"_aux","extended":true,"grants":[]}]}}`
	if _, err := parsePreparedManifest(valid, "runtime"); err != nil {
		t.Fatal(err)
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
	app := NewBaseApp(BaseAppConfig{PreparedManifestJSON: "{}"})
	if err := app.Bootstrap(); err == nil || app.IsBootstrapped() {
		t.Fatal("manifest without explicit identity fell through to legacy bootstrap")
	}
}
