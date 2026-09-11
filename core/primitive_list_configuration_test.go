package core_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/tests/contracts/primitivelists"
)

func TestPrimitiveListConfigurationUsesOneDeterministicValueField(t *testing.T) {
	config := primitivelists.Config()
	manifest, err := core.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := manifest.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	second, err := core.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	again, err := second.Bytes()
	if err != nil || !bytes.Equal(encoded, again) {
		t.Fatalf("nondeterministic list manifest: %v", err)
	}
	parsed, err := schema.Parse(encoded)
	if err != nil {
		t.Fatal(err)
	}
	again, err = parsed.Bytes()
	if err != nil || !bytes.Equal(encoded, again) {
		t.Fatalf("list manifest roundtrip: %v", err)
	}
	fields := manifest.Snapshot().Collections[0].Fields
	points, sizes := fields[1], fields[2]
	if points.Type != schema.FieldTypeTextList || points.Category != schema.FieldCategoryScalar || points.List.MinRows != 1 || points.List.MaxRows != 8 || *points.Text.MaxLength != 120 || points.Nested != nil || points.Select != nil {
		t.Fatalf("text list model: %#v", points)
	}
	if sizes.Type != schema.FieldTypeNumberList || sizes.List.MaxRows != 20 || *sizes.Number.Min != 0 || sizes.Nested != nil {
		t.Fatalf("number list model: %#v", sizes)
	}
	points.List.MinRows = 999
	*points.Text.MaxLength = 999
	sizes.List.MaxRows = 999
	*sizes.Number.Min = 999
	if actual, _ := manifest.Bytes(); !bytes.Equal(encoded, actual) {
		t.Fatal("list manifest retained mutable metadata aliases")
	}
	for _, nested := range [][]schema.Field{fields[5].Nested.ResolvedFields(), fields[6].Nested.ResolvedFields(), fields[7].Blocks.ResolvedTypes()[0].ResolvedFields(), schema.EmbeddedBlocks(fields[8])[0].Blocks.ResolvedTypes()[0].ResolvedFields()} {
		if len(nested) != 2 || nested[0].Type != schema.FieldTypeTextList || nested[1].Type != schema.FieldTypeNumberList {
			t.Fatalf("nested list contracts: %#v", nested)
		}
	}
}

func TestPrimitiveListDefaultsAndEditorsResolveWithoutExecutingCallbacks(t *testing.T) {
	calls := 0
	config := core.Config{Name: "Product defaults", Collections: []core.Collection{{Slug: "products", Fields: field.Fields{
		field.TextList("sellingPoints").Default("", "Oak", "Oak").Admin(field.Admin{Editor: field.Component("app:points")}),
		field.NumberList("sizes").DefaultFrom(func(operation.Context) (operation.Value[[]float64], error) {
			calls++
			return operation.Present([]float64{0, 8}), nil
		}).Admin(field.Admin{Editor: field.Component("app:sizes")}),
	}}}}
	manifest, err := core.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	fields := manifest.Snapshot().Collections[0].Fields
	if calls != 0 || *fields[0].Default != `["","Oak","Oak"]` || !fields[1].DynamicDefault || fields[1].Default != nil || fields[1].Admin.Editor.Reference != "app:sizes" {
		t.Fatalf("defaults/editor metadata: %#v", fields)
	}
	encoded, _ := manifest.Bytes()
	if _, err := schema.Parse(encoded); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("DefaultContext")) || bytes.Contains(encoded, []byte("validators")) {
		t.Fatal("callback details leaked into schema")
	}
}

func TestPrimitiveListRejectsScalarIndexPromises(t *testing.T) {
	for _, node := range (field.Fields{field.TextList("values"), field.NumberList("values")}) {
		_, err := core.Resolve(core.Config{Name: "Invalid index", Collections: []core.Collection{{Slug: "products",
			Fields: field.Fields{node, field.Text("title")}, Indexes: []core.CollectionIndex{{Fields: []string{"values", "title"}}},
		}}})
		if err == nil || !strings.Contains(err.Error(), "indexes[0].fields[0]") || !strings.Contains(err.Error(), "unsupported") {
			t.Fatalf("list compound index should be actionable: %v", err)
		}
	}
}

func TestPrimitiveListManifestRejectsMalformedMetadataAndDefaults(t *testing.T) {
	manifest, err := core.Resolve(core.Config{Name: "Lists", Collections: []core.Collection{{Slug: "products", Fields: field.Fields{
		field.TextList("points").MaxLength(3).Default("Oak"), field.NumberList("sizes").Min(0).Default(0),
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	setDefault := func(field *schema.Field, raw string) { field.Default = &raw }
	for name, mutate := range map[string]func(*schema.Field){
		"missing list metadata":  func(f *schema.Field) { f.List = nil },
		"wrong category":         func(f *schema.Field) { f.Category = schema.FieldCategoryNested },
		"object rows":            func(f *schema.Field) { f.Nested = &schema.NestedField{} },
		"selection":              func(f *schema.Field) { f.Select = &schema.SelectField{} },
		"plugin behavior":        func(f *schema.Field) { f.Plugin = &schema.PluginField{} },
		"date semantics":         func(f *schema.Field) { f.Date = &schema.DateField{} },
		"code semantics":         func(f *schema.Field) { f.Code = &schema.CodeField{} },
		"relationship semantics": func(f *schema.Field) { f.Relationship = &schema.RelationshipField{} },
		"upload semantics":       func(f *schema.Field) { f.Upload = &schema.UploadField{} },
		"computed semantics":     func(f *schema.Field) { f.Virtual = &schema.VirtualField{} },
		"negative count":         func(f *schema.Field) { f.List.MinRows = -1 },
		"reversed counts":        func(f *schema.Field) { f.List.MinRows, f.List.MaxRows = 3, 2 },
		"scalar default":         func(f *schema.Field) { setDefault(f, `"oak"`) },
		"null container default": func(f *schema.Field) { setDefault(f, `null`) },
		"null element default":   func(f *schema.Field) { setDefault(f, `[null]`) },
		"wrong element default":  func(f *schema.Field) { setDefault(f, `[1]`) },
		"invalid item length":    func(f *schema.Field) { setDefault(f, `["long"]`) },
		"required empty default": func(f *schema.Field) { f.Required = true; setDefault(f, `[]`) },
		"minimum default":        func(f *schema.Field) { f.List.MinRows = 2 },
		"index":                  func(f *schema.Field) { f.Index = true },
		"unique":                 func(f *schema.Field) { f.Unique = true },
	} {
		t.Run(name, func(t *testing.T) {
			snapshot := manifest.Snapshot()
			mutate(&snapshot.Collections[0].Fields[0])
			encoded, err := json.Marshal(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := schema.Parse(encoded); err == nil {
				t.Fatal("invalid manifest accepted")
			}
		})
	}
	for _, raw := range []string{`[null]`, `["0"]`, `[{}]`, `[1e999]`, `[-1]`} {
		snapshot := manifest.Snapshot()
		setDefault(&snapshot.Collections[0].Fields[1], raw)
		encoded, _ := json.Marshal(snapshot)
		if _, err := schema.Parse(encoded); err == nil {
			t.Fatalf("invalid number-list default accepted: %s", raw)
		}
	}
	snapshot := manifest.Snapshot()
	step := 1.0
	snapshot.Collections[0].Fields[1].Number.Step = &step
	encoded, _ := json.Marshal(snapshot)
	if _, err := schema.Parse(encoded); err == nil || !strings.Contains(err.Error(), "step") {
		t.Fatalf("unsupported scalar increment on a list: %v", err)
	}
}
