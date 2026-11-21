package pgrolecheck

import (
	"context"
	"reflect"
	"testing"
)

func TestCompareGrants(t *testing.T) {
	read := Grant{"relation", "items", "SELECT"}
	write := Grant{"relation", "items", "INSERT"}
	option := Grant{"relation", "items", "SELECT WITH GRANT OPTION"}
	drift := CompareGrants([]Grant{read, write}, []Grant{read, option})
	if !reflect.DeepEqual(drift.Missing, []Grant{write}) ||
		!reflect.DeepEqual(drift.Unexpected, []Grant{option}) {
		t.Fatalf("unexpected drift: %+v", drift)
	}
	drift = CompareGrants([]Grant{read, write}, []Grant{write, read, read})
	if len(drift.Missing) != 0 || len(drift.Unexpected) != 0 {
		t.Fatalf("equal grant sets reported drift: %+v", drift)
	}
	if _, err := (GrantContract{}).Check(context.Background(), nil); err == nil {
		t.Fatal("incomplete contract accepted")
	}
}
