package main

import (
	"testing"
)

func TestExtractUsesStableIDsAndReceiverNames(t *testing.T) {
	result, err := extract("../../../..", []string{"./field", "./migration/payload"})
	if err != nil {
		t.Fatal(err)
	}

	declarations := map[string]declaration{}
	for _, pkg := range result.Packages {
		for _, item := range pkg.Declarations {
			declarations[item.ID] = item
		}
	}
	for _, id := range []string{
		"go:github.com/riducms/ridu/field#Select",
		"go:github.com/riducms/ridu/field#TextField.Required",
		"go:github.com/riducms/ridu/field#View.Name",
		"go:github.com/riducms/ridu/migration/payload#ID",
	} {
		if _, ok := declarations[id]; !ok {
			t.Errorf("missing declaration %s", id)
		}
	}
	if got := declarations["go:github.com/riducms/ridu/field#Select"].Aliases; len(got) != 1 || got[0] != "field.Select" {
		t.Fatalf("unexpected exact aliases: %#v", got)
	}
	if got := declarations["go:github.com/riducms/ridu/field#Select"].Source.Path; got == "" || got[0] == '/' {
		t.Fatalf("source path must be repository-relative, got %q", got)
	}
}
