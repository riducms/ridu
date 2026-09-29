package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

func TestTypeSignatureOmitsPrivateFields(t *testing.T) {
	set := token.NewFileSet()
	file, err := parser.ParseFile(set, "example.go", `package example
type Definition struct {
	Name string
	key string
	run func() error
}
`, 0)
	if err != nil {
		t.Fatal(err)
	}
	group := file.Decls[0].(*ast.GenDecl)
	loaded := &packages.Package{
		PkgPath: "example", Name: "example",
		Types:     types.NewPackage("example", "example"),
		TypesInfo: &types.Info{Types: map[ast.Expr]types.TypeAndValue{}},
	}
	result := typeDeclaration(".", set, loaded, group, group.Specs[0].(*ast.TypeSpec))
	if !strings.Contains(result.Signature, "Name string") || !strings.Contains(result.Signature, "unexported fields") {
		t.Fatalf("signature must keep exported fields and indicate opaque state: %s", result.Signature)
	}
	if strings.Contains(result.Signature, "key") || strings.Contains(result.Signature, "run") {
		t.Fatalf("signature exposes implementation details: %s", result.Signature)
	}
}

func TestConstraintSignatureKeepsItsTypeSet(t *testing.T) {
	set := token.NewFileSet()
	file, err := parser.ParseFile(set, "example.go", `package example
type Path struct{ segments []string }
type FieldPath interface {
	~string | Path
}
`, 0)
	if err != nil {
		t.Fatal(err)
	}
	group := file.Decls[1].(*ast.GenDecl)
	loaded := &packages.Package{
		PkgPath: "example", Name: "example",
		Types:     types.NewPackage("example", "example"),
		TypesInfo: &types.Info{Types: map[ast.Expr]types.TypeAndValue{}},
	}
	result := typeDeclaration(".", set, loaded, group, group.Specs[0].(*ast.TypeSpec))
	if !strings.Contains(result.Signature, "~string | Path") || strings.Contains(result.Signature, "unexported") {
		t.Fatalf("constraint signature lost its type set: %s", result.Signature)
	}
	if len(result.Members) != 0 {
		t.Fatalf("constraint members = %#v, want none", result.Members)
	}
}

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
