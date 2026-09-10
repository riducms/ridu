package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/riducms/ridu/field"
	configresolver "github.com/riducms/ridu/internal/config"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func graphRule(operation.ValidationContext, operation.Value[string]) ([]operation.Issue, error) {
	return nil, nil
}
func graphHook(operation.WriteContext, operation.Value[string]) (operation.Change[string], error) {
	return operation.Keep[string](), nil
}
func graphAllow(operation.AccessContext) (bool, error) { return true, nil }

func graphFixture() Config {
	code := field.Text("code").Required().Localized().Validate(graphRule).
		Hooks(field.Hooks[string]{BeforeChange: []field.Transform[string]{graphHook}}).
		Access(field.Access{Read: graphAllow}).
		Admin(field.Admin{VisibleWhen: field.Equal(field.Sibling("kind"), "external")}).
		Private("owner", store.String("retained"))
	children := field.Fields{field.Text("kind"), code}
	factory := func(name string) field.GroupField {
		return field.Group(name, children).Admin(field.Admin{VisibleWhen: field.Equal(field.Root("tenant"), "public")})
	}
	return Config{Name: "Graph", Localization: LocalizationConfig{Locales: []Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}}, DefaultLocale: "en"}, Collections: []Collection{{Slug: "pages", Fields: field.Fields{
		field.Text("tenant"), field.Text("kind"), code, factory("primary"), factory("secondary"), field.Array("rows", children),
		field.Blocks("body", field.Block{Slug: "card", Fields: children}), factory("translated").Localized(),
		field.NamedTab("details", "Details", children), field.Row(field.Fields{code.Rename("row_code")}), field.UnnamedTab("More", field.Fields{code.Rename("tab_code")}),
	}}}}
}

func TestFieldGraphOccurrenceReuseAndBindings(t *testing.T) {
	config := graphFixture()
	r, err := resolveTestFieldGraph(config)
	if err != nil {
		t.Fatal(err)
	}
	occurrences := r.Occurrences()
	byPath := map[string]configresolver.Occurrence{}
	ids := map[string]bool{}
	for _, o := range occurrences {
		if ids[o.ID] {
			t.Fatalf("duplicate occurrence %s", o.ID)
		}
		ids[o.ID] = true
		if o.Stored {
			byPath[o.ResolvedPath] = o
		}
	}
	paths := []string{"code", "primary.code", "secondary.code", "rows.code", "body.card.code", "translated.code", "details.code", "row_code", "tab_code"}
	for _, path := range paths {
		o, ok := byPath[path]
		if !ok {
			t.Fatalf("missing %s", path)
		}
		if o.SchemaID == "" || o.AuthoredPath == "" || o.LocaleOwner == "" {
			t.Fatalf("incomplete placement: %#v", o)
		}
		d, ok := r.graph.Binding(o.ID)
		if !ok {
			t.Fatalf("missing private binding: %s", path)
		}
		x, err := field.AsText(d)
		if err != nil {
			t.Fatal(err)
		}
		if len(x.Validators()) != 1 || len(x.HookPolicy().BeforeChange) != 1 || x.AccessPolicy().Read == nil {
			t.Fatalf("lost behavior at %s", path)
		}
		v, ok := d.Private("owner")
		if text, _ := v.StringValue(); !ok || text != "retained" {
			t.Fatalf("lost private attachment at %s", path)
		}
		if len(o.References) != 1 {
			t.Fatalf("missing bound reference at %s", path)
		}
		wantSibling := strings.TrimSuffix(path, "code") + "kind"
		if path == "row_code" || path == "tab_code" {
			wantSibling = "kind"
		}
		if o.References[0].ResolvedPath != wantSibling || o.References[0].TargetID != byPath[wantSibling].ID {
			t.Fatalf("sibling binding %s = %#v; want %s", path, o.References, wantSibling)
		}
		if ref := x.AdminPolicy().VisibleWhen.Reference(); ref.Scope() != field.SiblingScope || ref.Path() != "kind" {
			t.Fatal("authoring reference was flattened")
		}
	}
	for _, path := range []string{"primary", "secondary", "translated"} {
		if byPath[path].References[0].TargetID != byPath["tenant"].ID {
			t.Fatal("root reference did not bind to root")
		}
	}
	if byPath["translated.code"].LocaleOwner != byPath["translated"].ID {
		t.Fatal("localized container did not own exact locale dimension")
	}
	if a := byPath["rows.code"].Repeated; len(a) != 1 || a[0].Identity != "_key" || a[0].OccurrenceID != byPath["rows"].ID {
		t.Fatalf("array identity axis %#v", a)
	}
	if a := byPath["body.card.code"].Repeated; len(a) != 1 || a[0].Identity != "_key" || a[0].Case != "card" {
		t.Fatalf("block identity axis %#v", a)
	}
	if byPath["details"].Boundary != "named_tab" || byPath["details.code"].ScopeID != byPath["details"].ID {
		t.Fatal("named tab did not establish an object scope")
	}
	for _, o := range occurrences {
		if o.Boundary == "layout" || o.Boundary == "unnamed_tab" {
			if o.Stored || o.SchemaID != "" {
				t.Fatal("layout was treated as a stored value")
			}
		}
	}
	manifest := r.Manifest().Snapshot()
	var named bool
	for _, f := range manifest.Collections[0].Fields {
		if f.Name == "details" {
			named = f.Admin.NamedTab
		}
		if f.Name == "code" && (f.Admin.Condition == nil || f.Admin.Condition.Predicate.Path.String() != "kind") {
			t.Fatal("finite admin expression failed static lowering")
		}
	}
	if !named {
		t.Fatal("named tab presentation was lost in manifest")
	}
}

type graphEditPlugin struct {
	key       string
	transform func(FieldGraphContext, field.Fields) (field.Fields, error)
}

func (p graphEditPlugin) Key() string { return p.key }
func (p graphEditPlugin) TransformFields(c FieldGraphContext, f field.Fields) (field.Fields, error) {
	return p.transform(c, f)
}

func TestFieldGraphPluginsComposeOnceWithoutAliasingOrLoss(t *testing.T) {
	var trace []string
	var escaped *field.ChildrenDraft
	base := field.Text("code").Validate(graphRule).Hooks(field.Hooks[string]{BeforeChange: []field.Transform[string]{graphHook}}).
		Access(field.Access{Read: graphAllow, Update: graphAllow}).
		Admin(field.Admin{Description: "original", Editor: field.Component("app:text", store.Object(nil)), Extensions: map[string]store.Value{"hint": store.String("keep")}}).
		Private("private", store.String("keep"))
	plugins := []Plugin{
		graphEditPlugin{key: "first", transform: func(_ FieldGraphContext, f field.Fields) (field.Fields, error) {
			trace = append(trace, "first")
			return f.Edit(func(d *field.ChildrenDraft) error {
				escaped = d
				return d.EditText("code", func(x field.TextField) field.TextField {
					return x.MaxLength(120).EditAdmin(func(a *field.Admin) { a.Description = "edited" }).Validate(graphRule).AppendHooks(field.Hooks[string]{BeforeChange: []field.Transform[string]{graphHook}}).RestrictAccess(field.Access{Update: func(operation.AccessContext) (bool, error) { return false, nil }})
				})
			})
		}},
		graphEditPlugin{key: "second", transform: func(_ FieldGraphContext, f field.Fields) (field.Fields, error) {
			trace = append(trace, "second")
			return f.Edit(func(d *field.ChildrenDraft) error { return d.Insert(1, field.Number("priority").Min(0)) })
		}},
	}
	config := Config{Name: "Plugins", Collections: []Collection{{Slug: "pages", Fields: field.Fields{base}}}, Plugins: plugins}
	r, err := resolveTestFieldGraph(config)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := escaped.Replace("code", field.Number("code")); err != nil {
		t.Fatal(err)
	}
	d, _ := r.graph.Binding(r.Occurrences()[0].ID)
	x, err := field.AsText(d)
	if err != nil {
		t.Fatal("escaped edit draft mutated committed graph", err)
	}
	if len(x.Validators()) != 2 || len(x.HookPolicy().BeforeChange) != 2 || x.AccessPolicy().Read == nil || x.AdminPolicy().Editor.Key != "app:text" || x.AdminPolicy().Description != "edited" {
		t.Fatalf("plugin edit lost or duplicated policies: %#v", d.BehaviorSummary())
	}
	if allowed, _ := x.AccessPolicy().Update(operation.AccessContext{}); allowed {
		t.Fatal("access restriction was lost")
	}
	if _, ok := d.Private("private"); !ok {
		t.Fatal("plugin lost private attachment")
	}
	if len(base.Validators()) != 1 || len(base.HookPolicy().BeforeChange) != 1 || base.AdminPolicy().Description != "original" {
		t.Fatal("plugin mutated input")
	}
	for i := 0; i < 3; i++ {
		again, err := resolveTestFieldGraph(config)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(again)
		if err != nil || !bytes.Equal(canonical, encoded) {
			t.Fatalf("resolution drifted: %v", err)
		}
	}
	if want := []string{"first", "second", "first", "second", "first", "second", "first", "second"}; !reflect.DeepEqual(trace, want) {
		t.Fatalf("plugin order/call count = %v", trace)
	}
	if got := r.Occurrences()[0].Provenance; !reflect.DeepEqual(got, []string{"plugin:first", "plugin:second"}) {
		t.Fatalf("provenance=%v", got)
	}
	mutated := r.Occurrences()
	mutated[0].Provenance[0] = "changed"
	mutated[0].Extensions["hint"] = store.String("changed")
	after, _ := json.Marshal(r)
	if !bytes.Equal(canonical, after) {
		t.Fatal("occurrence metadata exposed mutable graph state")
	}
}

func TestFieldGraphResolutionRejectsConflictsAndInvalidReferences(t *testing.T) {
	for name, node := range map[string]field.Node{
		"missing": field.Text("code").Admin(field.Admin{VisibleWhen: field.Equal(field.Sibling("missing"), "x")}),
		"path":    field.Text("code").Admin(field.Admin{VisibleWhen: field.Equal(field.Root("a.b"), "x")}),
		"object":  field.Text("code").Admin(field.Admin{VisibleWhen: field.Equal(field.Sibling("object"), "x")}),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := resolveTestFieldGraph(Config{Name: "Invalid", Collections: []Collection{{Slug: "pages", Fields: field.Fields{field.Group("object", field.Fields{field.Text("child")}), node}.WithProvenance("factory:Link")}}})
			if err == nil || !strings.Contains(err.Error(), "code") || !strings.Contains(err.Error(), "factory:Link") {
				t.Fatalf("missing authored/resolved/provenance diagnostic: %v", err)
			}
		})
	}
	calls := 0
	p := graphEditPlugin{key: "duplicate", transform: func(_ FieldGraphContext, f field.Fields) (field.Fields, error) { calls++; return f, nil }}
	_, err := resolveTestFieldGraph(Config{Name: "Duplicate", Collections: []Collection{{Slug: "pages", Fields: field.Fields{field.Text("title")}}}, Plugins: []Plugin{p, p}})
	if err == nil || calls != 0 {
		t.Fatalf("duplicate plugin transformed graph: %v, calls=%d", err, calls)
	}
	_, err = resolveTestFieldGraph(Config{Name: "Kind", Collections: []Collection{{Slug: "pages", Fields: field.Fields{field.Number("code")}}}, Plugins: []Plugin{graphEditPlugin{key: "text-only", transform: func(_ FieldGraphContext, f field.Fields) (field.Fields, error) {
		return f.Edit(func(d *field.ChildrenDraft) error {
			return d.EditText("code", func(x field.TextField) field.TextField { return x.Required() })
		})
	}}}})
	if err == nil || !strings.Contains(err.Error(), "incompatible_kind") {
		t.Fatalf("incompatible plugin edit accepted: %v", err)
	}
}

func TestFieldGraphFunctionsDoNotChangeSchemaIdentityOrExecute(t *testing.T) {
	calls := 0
	build := func(label string) Config {
		return Config{Name: "Private", Collections: []Collection{{Slug: "pages", Fields: field.Fields{field.Text("code").Validate(func(operation.ValidationContext, operation.Value[string]) ([]operation.Issue, error) {
			calls++
			return nil, errors.New(label)
		}).Private("secret", store.String(label))}}}}
	}
	a, err := resolveTestFieldGraph(build("a-secret"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := resolveTestFieldGraph(build("b-secret"))
	if err != nil {
		t.Fatal(err)
	}
	manifestBytes, err := a.Manifest().Bytes()
	if err != nil {
		t.Fatal(err)
	}
	for _, occurrence := range a.Occurrences() {
		if bytes.Contains(manifestBytes, []byte(occurrence.ID)) {
			t.Fatal("private placement identity entered the public schema manifest")
		}
	}
	aJSON, _ := json.Marshal(a)
	bJSON, _ := json.Marshal(b)
	if !bytes.Equal(aJSON, bJSON) || bytes.Contains(aJSON, []byte("secret")) {
		t.Fatal("callback closure or private attachment affected serializable identity")
	}
	_, err = New(build("never execute"), nil)
	if err == nil || !strings.Contains(err.Error(), "store") || calls != 0 {
		t.Fatalf("application construction executed a field callback: %v calls=%d", err, calls)
	}
}

func TestFieldGraphComputedOwnershipAndUnsupportedPolicies(t *testing.T) {
	calls := 0
	computed := field.Virtual("summary", field.ValueString, func(operation.ReadContext) (operation.Value[store.Value], error) {
		calls++
		return operation.Present(store.String("computed")), nil
	}).Access(field.Access{Read: graphAllow})
	config := Config{Name: "Computed", Collections: []Collection{{Slug: "pages", Fields: field.Fields{field.Text("title"), computed}}}}
	r, err := resolveTestFieldGraph(config)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range r.Occurrences() {
		if o.Name == "summary" {
			d, ok := r.graph.Binding(o.ID)
			if !ok || !d.BehaviorSummary().Resolver || o.Stored || o.Boundary != "output" {
				t.Fatalf("computed ownership lost: %#v", o)
			}
		}
	}
	if calls != 0 {
		t.Fatal("resolution executed computed resolver")
	}
	for name, test := range map[string]struct {
		config     Config
		code, path string
	}{
		"output write access":  {config: Config{Name: "Bad", Collections: []Collection{{Slug: "pages", Fields: field.Fields{computed.Access(field.Access{Update: graphAllow})}}}}, code: "incompatible_field_policy", path: "collections[0].fields[0].access.update"},
		"global create access": {config: Config{Name: "Bad", Collections: []Collection{{Slug: "pages", Fields: field.Fields{field.Text("title")}}}, Globals: []Global{{Slug: "settings", Fields: field.Fields{field.Text("name").Access(field.Access{Create: graphAllow})}}}}, code: "incompatible_field_policy", path: "globals[0].fields[0].access.create"},
		"global delete hook":   {config: Config{Name: "Bad", Collections: []Collection{{Slug: "pages", Fields: field.Fields{field.Text("title")}}}, Globals: []Global{{Slug: "settings", Fields: field.Fields{field.Text("name").Hooks(field.Hooks[string]{BeforeDelete: []field.Observer[string]{func(operation.EventContext, operation.Value[string]) error { return nil }}})}}}}, code: "incompatible_field_policy", path: "globals[0].fields[0].hooks.beforeDelete"},
		"empty operand set":    {config: Config{Name: "Bad", Collections: []Collection{{Slug: "pages", Fields: field.Fields{field.Text("title").Admin(field.Admin{VisibleWhen: field.OneOf[string](field.Sibling("title"))})}}}}, code: "invalid_condition_values", path: "collections[0].fields[0].admin.visibleWhen.values"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := resolveTestFieldGraph(test.config)
			var validationError *schema.ValidationError
			if !errors.As(err, &validationError) {
				t.Fatalf("unsupported policy error = %v", err)
			}
			if len(validationError.Issues) != 1 || validationError.Issues[0].Code != test.code || validationError.Issues[0].Path != test.path {
				t.Fatalf("policy diagnostic = %#v; want %s at %s", validationError.Issues, test.code, test.path)
			}
			if strings.Contains(err.Error(), ".behavior") {
				t.Fatalf("obsolete policy path: %v", err)
			}
		})
	}
}

func TestFieldGraphStaticContract(t *testing.T) {
	manifest, err := Resolve(Config{Name: "Fields", Collections: []Collection{{Slug: "pages", Fields: field.Fields{field.Text("title").Required().Label("Title").MaxLength(120), field.Group("meta", field.Fields{field.Number("order").Min(0)})}}}})
	if err != nil {
		t.Fatal(err)
	}
	fields := manifest.Snapshot().Collections[0].Fields
	if len(fields) != 2 || !fields[0].Required || fields[0].Admin.Label != "Title" || fields[0].Text.MaxLength == nil || *fields[0].Text.MaxLength != 120 || fields[1].Nested.ResolvedFields()[0].Number.Min == nil || *fields[1].Nested.ResolvedFields()[0].Number.Min != 0 {
		t.Fatalf("field contract: %#v", fields)
	}
}

func TestFieldGraphOccurrenceIDsIgnorePointerReuse(t *testing.T) {
	shared := field.Text("code")
	build := func(a, b field.Node) Config {
		return Config{Name: "Pointers", Collections: []Collection{{Slug: "pages", Fields: field.Fields{field.Group("a", field.Fields{a}), field.Group("b", field.Fields{b})}}}}
	}
	a, err := resolveTestFieldGraph(build(&shared, &shared))
	if err != nil {
		t.Fatal(err)
	}
	b, err := resolveTestFieldGraph(build(field.Text("code"), field.Text("code")))
	if err != nil {
		t.Fatal(err)
	}
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	if !bytes.Equal(x, y) {
		t.Fatal("pointer reuse affected occurrence identity")
	}
	shared = shared.Rename("changed")
	z, _ := json.Marshal(a)
	if !bytes.Equal(x, z) {
		t.Fatal("pointer facade escaped resolution snapshot")
	}
}

// This plugin intentionally has no runtime field validator. The test proves
// descriptor-owned child graph resolution, not plugin codec execution.
type graphEmbeddedPlugin struct{}

func (graphEmbeddedPlugin) Key() string { return "graph-embedded" }
func (graphEmbeddedPlugin) Descriptor() PluginDescriptor {
	return PluginDescriptor{Version: "1.0.0", GoPackage: "example.com/graph", APIVersion: PluginAPIVersion, Ridu: RiduCompatibility{Minimum: FrameworkVersion}, FieldTypes: []PluginFieldType{{Key: "graph-embedded", TypeScriptPackage: "@example/graph", TypeScriptOutput: "Output", TypeScriptInput: "Input", EmbeddedTypes: []string{"widgets.widget"}, JSONSchema: []byte(`{"type":"object"}`)}}}
}

func TestFieldGraphPluginEmbeddedSchemasRetainBehaviorAndIdentity(t *testing.T) {
	children := field.Fields{field.Text("kind"), field.Text("code").Validate(graphRule).Admin(field.Admin{VisibleWhen: field.Equal(field.Sibling("kind"), "card")}).Private("embedded", store.String("kept"))}
	plugin := field.Plugin("body", "graph-embedded", json.RawMessage(`{}`)).EmbeddedTrees(field.EmbeddedTree{Key: "widgets", Root: []string{"outline"}, Children: "items", Tag: "kind", Cases: []field.EmbeddedTreeCase{{TagValue: "widget", Payload: "content", Discriminator: "schema", Identity: "uid", Types: []field.Block{{Slug: "card", Fields: children}}}}})
	r, err := resolveTestFieldGraph(Config{Name: "Embedded", Plugins: []Plugin{graphEmbeddedPlugin{}}, Collections: []Collection{{Slug: "pages", Fields: field.Fields{plugin}}}})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, o := range r.Occurrences() {
		if o.ResolvedPath != "body.widgets.widget.card.code" {
			continue
		}
		found = true
		d, ok := r.graph.Binding(o.ID)
		if !ok || d.BehaviorSummary().Validators != 1 {
			t.Fatal("embedded behavior lost")
		}
		if len(o.Repeated) != 1 || o.Repeated[0].Identity != "uid" || o.Repeated[0].Case != "card" {
			t.Fatalf("embedded identity=%#v", o.Repeated)
		}
		if o.References[0].ResolvedPath != "body.widgets.widget.card.kind" || o.SchemaID == schema.StableID("") {
			t.Fatalf("embedded occurrence=%#v", o)
		}
	}
	if !found {
		t.Fatal("plugin-declared child schema not traversed")
	}
}

func TestFieldGraphLayoutMetadataAlwaysValidates(t *testing.T) {
	hosts := map[string]func() field.LayoutField{
		"row":         func() field.LayoutField { return field.Row(field.Fields{field.Text("child")}) },
		"tabs":        func() field.LayoutField { return field.UnnamedTab("Content", field.Fields{field.Text("child")}) },
		"collapsible": func() field.LayoutField { return field.Collapsible("details", field.Fields{field.Text("child")}) },
	}
	resolve := func(node field.Node) (testFieldGraphResolution, error) {
		return resolveTestFieldGraph(Config{Name: "Layout metadata", Collections: []Collection{{Slug: "pages", Fields: field.Fields{node}}}})
	}
	for host, build := range hosts {
		t.Run(host, func(t *testing.T) {
			for name, node := range map[string]field.Node{
				"nonfinite extension":       build().Admin(field.Admin{Extensions: map[string]store.Value{"bad": store.Number(math.NaN())}}),
				"invalid component":         build().Admin(field.Admin{Editor: field.Component("wrong", store.Object(nil))}),
				"invalid private namespace": build().Private("", store.String("private")),
			} {
				t.Run(name, func(t *testing.T) {
					if _, err := resolve(node); err == nil {
						t.Fatal("invalid layout metadata resolved")
					}
				})
			}
			result, err := resolve(build().Admin(field.Admin{Extensions: map[string]store.Value{"info": store.String("public")}}).Private("owner", store.String("private")))
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(encoded, []byte(`"info":"public"`)) || bytes.Contains(encoded, []byte(`"owner"`)) {
				t.Fatal("layout extension/private boundary changed")
			}
			manifest, err := result.Manifest().Bytes()
			if err != nil || !bytes.Contains(manifest, []byte(`"info": "public"`)) || bytes.Contains(manifest, []byte(`"owner"`)) {
				t.Fatalf("layout public extensions must reach the canonical manifest without private attachments: %v\n%s", err, manifest)
			}
		})
	}
}

func TestFieldGraphLayoutRejectsUnsupportedAdminInsteadOfDiscardingIt(t *testing.T) {
	policies := map[string]field.Admin{
		"hidden": {Hidden: true}, "readOnly": {ReadOnly: true}, "description": {Description: "silently lost"},
		"columns": {Columns: 2}, "editor": {Editor: field.Component("app:Editor", store.Object(nil))},
		"rowLabel": {RowLabel: field.Component("app:RowLabel", store.Object(nil))}, "rowLabelPath": {RowLabelPath: "child"},
		"visibleWhen": {VisibleWhen: field.Equal(field.Sibling("kind"), "x")},
	}
	for name, admin := range policies {
		t.Run(name, func(t *testing.T) {
			for _, layout := range []field.LayoutField{field.Row(field.Fields{field.Text("child")}), field.UnnamedTab("Content", field.Fields{field.Text("child")}), field.Collapsible("details", field.Fields{field.Text("child")})} {
				_, err := resolveTestFieldGraph(Config{Name: "Unsupported layout", Collections: []Collection{{Slug: "pages", Fields: field.Fields{field.Text("kind"), layout.Admin(admin)}}}})
				var validationError *schema.ValidationError
				if !errors.As(err, &validationError) {
					t.Fatalf("%s accepted or lost %s: %v", layout.Kind(), name, err)
				}
				found := false
				for _, issue := range validationError.Issues {
					found = found || issue.Code == "unsupported_layout_admin" && issue.Path == "collections[0].fields[1].admin."+name
				}
				if !found {
					t.Fatalf("%s missing actionable %s diagnostic: %#v", layout.Kind(), name, validationError.Issues)
				}
			}
		})
	}
	for _, layout := range []field.LayoutField{field.Row(field.Fields{field.Text("child")}), field.Tabs(field.Fields{field.UnnamedTab("Content", field.Fields{field.Text("child")})})} {
		_, err := resolveTestFieldGraph(Config{Name: "Unsupported label", Collections: []Collection{{Slug: "pages", Fields: field.Fields{layout.Label("Lost")}}}})
		if err == nil {
			t.Fatal("unsupported wrapper label was silently discarded")
		}
	}
	result, err := resolveTestFieldGraph(Config{Name: "Collapse", Collections: []Collection{{Slug: "pages", Fields: field.Fields{field.Collapsible("details", field.Fields{field.Text("child")}).Label("Advanced").Admin(field.Admin{InitiallyCollapsed: true})}}}})
	if err != nil {
		t.Fatal(err)
	}
	if c := result.Manifest().Snapshot().Collections[0].Fields[0].Admin.Collapsible; c == nil || c.Label != "Advanced" || !c.InitiallyCollapsed {
		t.Fatalf("supported collapse presentation lost: %#v", c)
	}
}

func TestFieldGraphLayoutContract(t *testing.T) {
	manifest, err := Resolve(Config{Name: "Layouts", Collections: []Collection{{Slug: "pages", Fields: field.Fields{field.Row(field.Fields{field.Text("first")}), field.Collapsible("details", field.Fields{field.Text("second")}).Admin(field.Admin{InitiallyCollapsed: true}), field.Tabs(field.Fields{field.UnnamedTab("More", field.Fields{field.Text("third")})})}}}})
	if err != nil {
		t.Fatal(err)
	}
	fields := manifest.Snapshot().Collections[0].Fields
	if len(fields) != 3 || fields[0].Admin.Row == nil || fields[1].Admin.Collapsible == nil || !fields[1].Admin.Collapsible.InitiallyCollapsed || fields[2].Admin.Tab != "More" {
		t.Fatalf("layout contract: %#v", fields)
	}
}
