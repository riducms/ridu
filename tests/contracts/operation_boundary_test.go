package contracts_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/core"
	operationengine "github.com/riducms/ridu/internal/operation"
	"github.com/riducms/ridu/operation"
)

// Resource callbacks, field callbacks and execution must share one named type,
// so applications can reuse operation predicates without conversions.
func TestOperationVocabularyCrossesEveryCallbackBoundary(t *testing.T) {
	allowUpdate := func(kind operation.Kind) bool { return kind == operation.Update }
	resource := ridu.AccessContext{Operation: operation.Update}
	hook := core.HookContext{Operation: resource.Operation}
	field := operation.AccessContext{Operation: hook.Operation}
	effect := core.AfterCommitEffect{Operation: field.Operation}
	request := operationengine.Request{Operation: effect.Operation}
	for _, kind := range []operation.Kind{resource.Operation, hook.Operation, field.Operation, effect.Operation, request.Operation} {
		if !allowUpdate(kind) {
			t.Fatalf("callback changed operation: %s", kind)
		}
	}
}

func TestPublicCoordinationDoesNotExposeResolverMetadataOrDuplicateOperations(t *testing.T) {
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../.."))
	retired := map[string]bool{
		"Operation":             true,
		"ResolveGraph":          true,
		"FieldGraphResolution":  true,
		"FieldOccurrence":       true,
		"FieldRepeatedAxis":     true,
		"FieldReferenceBinding": true,
	}
	for _, name := range []string{"Create", "Duplicate", "Admin", "Read", "ReadVersions", "Update", "Delete", "RestoreDeleted", "DeletePermanent", "Publish", "Unpublish", "Unlock"} {
		retired["Operation"+name] = true
	}
	for _, directory := range []string{root, filepath.Join(root, "core")} {
		entries, err := os.ReadDir(directory)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}
			path := filepath.Join(directory, entry.Name())
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			check := func(name string) {
				if retired[name] {
					t.Errorf("%s exports retired coordination symbol %s", path, name)
				}
			}
			for _, declaration := range file.Decls {
				switch declaration := declaration.(type) {
				case *ast.FuncDecl:
					if declaration.Recv == nil {
						check(declaration.Name.Name)
					}
				case *ast.GenDecl:
					for _, spec := range declaration.Specs {
						switch spec := spec.(type) {
						case *ast.TypeSpec:
							check(spec.Name.Name)
						case *ast.ValueSpec:
							for _, name := range spec.Names {
								check(name.Name)
							}
						}
					}
				}
			}
		}
	}
}
