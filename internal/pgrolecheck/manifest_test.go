package pgrolecheck

import (
	"context"
	"reflect"
	"testing"
)

func TestCompareSchemaInventory(t *testing.T) {
	missing, unexpected := CompareSchemaInventory([]string{"public", "_aux"}, []string{"public", "other"})
	if !reflect.DeepEqual(missing, []string{"_aux"}) || !reflect.DeepEqual(unexpected, []string{"other"}) {
		t.Fatalf("unexpected inventory comparison: %v %v", missing, unexpected)
	}
	missing, unexpected = CompareSchemaInventory([]string{"public", "_aux"}, []string{"_aux", "public"})
	if len(missing) != 0 || len(unexpected) != 0 {
		t.Fatal("matching inventory rejected")
	}
}

func TestManifestValidation(t *testing.T) {
	schema := GrantContract{Role: "runtime", Schema: "public", Extended: true, Grants: []Grant{}}
	manifest := GrantManifest{Role: "runtime", Schemas: []GrantContract{schema}}
	if err := manifest.validate(); err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []GrantManifest{
		{},
		{Role: "other", Schemas: []GrantContract{schema}},
		{Role: "runtime", Schemas: []GrantContract{schema, schema}},
		{Role: "runtime", Schemas: []GrantContract{{Role: "runtime", Schema: "public", Grants: []Grant{}}}},
		{Role: "runtime", Schemas: []GrantContract{{Role: "runtime", Schema: "pg_catalog", Extended: true, Grants: []Grant{}}}},
	} {
		if err := candidate.validate(); err == nil {
			t.Fatal("invalid manifest accepted")
		}
	}
	if _, err := manifest.Check(context.Background(), nil); err == nil {
		t.Fatal("nil manifest connection accepted")
	}
}
