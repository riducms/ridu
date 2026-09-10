package generate

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/typescript"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/plugins/graphql"
	"github.com/riducms/ridu/schema"
)

func TestDynamicDefaultsGenerateWithoutExecutingCallbacks(t *testing.T) {
	build := func() schema.Manifest {
		manifest, err := core.Resolve(core.Config{Name: "Dynamic contract", Collections: []core.Collection{{Slug: "posts", Fields: field.Fields{
			field.Text("title").Required().DefaultFrom(func(operation.DefaultContext) (operation.Value[string], error) {
				t.Fatal("configuration and generation must never execute a default callback")
				return operation.Present("request-specific-secret"), nil
			}),
			field.Text("optional").DefaultFrom(func(operation.DefaultContext) (operation.Value[string], error) {
				t.Fatal("optional default callback executed during generation")
				return operation.Empty[string](), nil
			}),
		}}}})
		if err != nil {
			t.Fatal(err)
		}
		return manifest
	}
	first, second := build(), build()
	if fields := first.Snapshot().Collections[0].Fields; !fields[0].DynamicDefault || fields[0].Default != nil {
		t.Fatalf("default metadata = %#v", fields[0])
	}
	for name, generate := range map[string]func(schema.Manifest) ([]byte, error){
		"manifest": func(m schema.Manifest) ([]byte, error) { return m.Bytes() },
		"Go":       goClient, "TypeScript": typescript.Client, "OpenAPI": openAPI,
		"GraphQL": func(m schema.Manifest) ([]byte, error) {
			value, err := graphql.GenerateSDL(m)
			return []byte(value), err
		},
	} {
		t.Run(name, func(t *testing.T) {
			a, err := generate(first)
			if err != nil {
				t.Fatal(err)
			}
			b, err := generate(second)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(a, b) {
				t.Fatal("equivalent callbacks changed generated output")
			}
			if bytes.Contains(a, []byte("request-specific-secret")) {
				t.Fatal("request-specific value leaked into generated output")
			}
			switch name {
			case "Go":
				if !strings.Contains(string(a), "Title *string") {
					t.Fatalf("create input must permit omitted dynamic default:\n%s", a)
				}
			case "TypeScript":
				if !strings.Contains(string(a), "\"title\"?: string") {
					t.Fatalf("create input must permit omitted dynamic default:\n%s", a)
				}
			case "OpenAPI":
				var document openAPIDocument
				if err := json.Unmarshal(a, &document); err != nil {
					t.Fatal(err)
				}
				if err := resolvedResourceSchema(t, document, "postsCreate").Validate(map[string]any{}); err != nil {
					t.Fatalf("dynamic create omission rejected: %v", err)
				}
				if err := resolvedResourceSchema(t, document, "postsCreate").Validate(map[string]any{"title": nil}); err == nil {
					t.Fatal("required dynamic title must reject explicit null")
				}
			case "GraphQL":
				if !strings.Contains(string(a), "title: String\n") {
					t.Fatalf("create input must permit omitted dynamic default:\n%s", a)
				}
			}
		})
	}
}
