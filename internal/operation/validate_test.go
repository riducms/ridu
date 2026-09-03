package operation

import (
	"testing"

	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestRequiredFieldsRejectExplicitEmptyValues(t *testing.T) {
	tests := []struct {
		name  string
		field schema.Field
		value store.Value
	}{
		{name: "text", field: validationField(t, "title", schema.FieldTypeText, true), value: store.String("")},
		{name: "textarea", field: validationField(t, "summary", schema.FieldTypeTextarea, true), value: store.String("")},
		{name: "email", field: validationField(t, "email", schema.FieldTypeEmail, true), value: store.String("")},
		{name: "date", field: validationField(t, "publishedAt", schema.FieldTypeDate, true), value: store.String("")},
		{
			name:  "select",
			field: withSelect(validationField(t, "status", schema.FieldTypeSelect, true), schema.SelectChoice{Value: "draft", Label: "Draft"}),
			value: store.String(""),
		},
		{
			name:  "relationship",
			field: withRelationship(validationField(t, "author", schema.FieldTypeRelationship, true)),
			value: store.String(""),
		},
		{
			name:  "upload",
			field: withUpload(validationField(t, "cover", schema.FieldTypeUpload, true)),
			value: store.String(""),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, issues := validate([]schema.Field{test.field}, store.Values{test.field.Name: test.value}, true, nil)
			if len(issues) != 1 || issues[0].Code != "required" || issues[0].Path != test.field.Name {
				t.Fatalf("issues = %#v, want one required issue at %q", issues, test.field.Name)
			}
		})
	}
}

func TestOptionalStringBackedFieldsAcceptEmptyValues(t *testing.T) {
	fields := []schema.Field{
		validationField(t, "email", schema.FieldTypeEmail, false),
		validationField(t, "publishedAt", schema.FieldTypeDate, false),
		withSelect(validationField(t, "status", schema.FieldTypeSelect, false), schema.SelectChoice{Value: "draft", Label: "Draft"}),
		withRelationship(validationField(t, "author", schema.FieldTypeRelationship, false)),
		withUpload(validationField(t, "cover", schema.FieldTypeUpload, false)),
	}
	values := store.Values{}
	for _, field := range fields {
		values[field.Name] = store.String("")
	}

	if _, issues := validate(fields, values, true, nil); len(issues) != 0 {
		t.Fatalf("optional empty values produced issues: %#v", issues)
	}
}

func TestDatePickerAppearancesValidateTheirWireShapes(t *testing.T) {
	tests := []struct {
		name       string
		appearance schema.DatePickerAppearance
		valid      string
		invalid    string
	}{
		{name: "day only", appearance: schema.DatePickerDayOnly, valid: "2026-09-15", invalid: "2026-09-15T09:30:00Z"},
		{name: "day and time", appearance: schema.DatePickerDayAndTime, valid: "2026-09-15T09:30:00Z", invalid: "2026-09-15"},
		{name: "time only", appearance: schema.DatePickerTimeOnly, valid: "09:30", invalid: "2026-09-15T09:30:00Z"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			field := validationField(t, "when", schema.FieldTypeDate, true)
			field.Date = &schema.DateField{PickerAppearance: test.appearance}
			if _, issues := validate([]schema.Field{field}, store.Values{"when": store.String(test.valid)}, true, nil); len(issues) != 0 {
				t.Fatalf("valid value produced issues: %#v", issues)
			}
			if _, issues := validate([]schema.Field{field}, store.Values{"when": store.String(test.invalid)}, true, nil); len(issues) != 1 || issues[0].Code != "invalid_date" {
				t.Fatalf("invalid value issues = %#v, want invalid_date", issues)
			}
		})
	}
}

func TestRequiredCollectionsRejectZeroItems(t *testing.T) {
	rowField := validationField(t, "items", schema.FieldTypeArray, true)
	rowField.Nested = &schema.NestedField{Fields: []schema.Field{validationField(t, "items.title", schema.FieldTypeText, true)}}
	blockField := validationField(t, "content", schema.FieldTypeBlocks, true)
	blockField.Blocks = &schema.BlocksField{Types: []schema.BlockType{{Key: "heading", Label: "Heading"}}}

	_, issues := validate(
		[]schema.Field{rowField, blockField},
		store.Values{"items": store.List(), "content": store.List()},
		true,
		nil,
	)
	if len(issues) != 2 || issues[0].Path != "items" || issues[1].Path != "content" {
		t.Fatalf("issues = %#v, want required collection issues", issues)
	}
}

func TestArrayBoundsPointAndPresentationValidation(t *testing.T) {
	items := validationField(t, "items", schema.FieldTypeArray, false)
	items.Nested = &schema.NestedField{Fields: []schema.Field{validationField(t, "items.name", schema.FieldTypeText, true)}, MinRows: 1, MaxRows: 2}
	point := validationField(t, "location", schema.FieldTypePoint, false)
	ui := validationField(t, "guide", schema.FieldTypeUI, false)
	ui.Category = schema.FieldCategoryPresentation

	_, issues := validate([]schema.Field{items, point, ui}, store.Values{
		"items":    store.List(),
		"location": store.List(store.Number(181), store.Number(45)),
		"guide":    store.String("must not persist"),
	}, true, nil)
	codes := make(map[string]bool, len(issues))
	for _, issue := range issues {
		codes[issue.Code] = true
	}
	if len(issues) != 3 || !codes["unknown_field"] || !codes["min_rows"] || !codes["invalid_point"] {
		t.Fatalf("issues = %#v", issues)
	}

	validated, issues := validate([]schema.Field{items, point, ui}, store.Values{
		"items":    store.List(store.Object(store.Values{"name": store.String("One")})),
		"location": store.List(store.Number(-0.1276), store.Number(51.5072)),
	}, true, nil)
	if len(issues) != 0 {
		t.Fatalf("valid values produced issues: %#v", issues)
	}
	if _, exists := validated["guide"]; exists {
		t.Fatal("presentation-only value survived validation")
	}
}

func TestNestedValidationUsesRuntimePathsAndRequiresCompleteSuppliedRows(t *testing.T) {
	copyField := validationField(t, "answers.copy", schema.FieldTypeText, true)
	answers := validationField(t, "answers", schema.FieldTypeArray, false)
	answers.Nested = &schema.NestedField{Fields: []schema.Field{copyField}}

	_, issues := validate(
		[]schema.Field{answers},
		store.Values{"answers": store.List(store.Object(store.Values{"extra": store.String("value")}))},
		false,
		nil,
	)
	if len(issues) != 2 {
		t.Fatalf("issues = %#v, want unknown and required nested issues", issues)
	}
	if issues[0].Path != "answers.0.extra" || issues[1].Path != "answers.0.copy" {
		t.Fatalf("runtime issue paths = %#v", issues)
	}

	if _, issues := validate([]schema.Field{answers}, store.Values{}, false, nil); len(issues) != 0 {
		t.Fatalf("omitted top-level update field produced issues: %#v", issues)
	}
}

func TestBlockAndPluginIssuesUseConcreteRuntimePaths(t *testing.T) {
	pluginField := validationField(t, "content.callout.accent", schema.FieldTypePlugin, true)
	pluginField.Plugin = &schema.PluginField{Key: "color"}
	content := validationField(t, "content", schema.FieldTypeBlocks, false)
	content.Blocks = &schema.BlocksField{Types: []schema.BlockType{{
		Key: "callout", Label: "Callout", Fields: []schema.Field{pluginField},
	}}}
	validators := map[string]PluginValidator{
		"color": func(_ schema.Field, _ store.Value, runtimePath string) []schema.Issue {
			return []schema.Issue{{Code: "invalid_color", Path: runtimePath, Message: "invalid color"}}
		},
	}

	_, issues := validate(
		[]schema.Field{content},
		store.Values{"content": store.List(store.Object(store.Values{
			"blockType": store.String("callout"), "accent": store.String("purple"),
		}))},
		true,
		validators,
	)
	if len(issues) != 1 || issues[0].Path != "content.0.accent" {
		t.Fatalf("plugin issues = %#v, want concrete block path", issues)
	}
}

func TestValidationIssueCollectionIsBounded(t *testing.T) {
	items := validationField(t, "items", schema.FieldTypeArray, false)
	items.Nested = &schema.NestedField{Fields: []schema.Field{validationField(t, "items.name", schema.FieldTypeText, true)}}
	invalid := make([]store.Value, MaxValidationIssues*4)
	for index := range invalid {
		invalid[index] = store.String("not an object")
	}
	_, issues := validate([]schema.Field{items}, store.Values{"items": store.List(invalid...)}, true, nil)
	if len(issues) != MaxValidationIssues {
		t.Fatalf("validation issue count = %d, want cap %d", len(issues), MaxValidationIssues)
	}
	if issues[0].Path != "items.0" || issues[len(issues)-1].Path != "items.127" {
		t.Fatalf("unexpected bounded issue paths: first=%q last=%q", issues[0].Path, issues[len(issues)-1].Path)
	}
}

func TestMultiSelectValidationAndDefaultsPreserveOrder(t *testing.T) {
	roles := validationField(t, "roles", schema.FieldTypeSelect, true)
	roles.Select = &schema.SelectField{
		HasMany:       true,
		Choices:       []schema.SelectChoice{{Value: "admin", Label: "Admin"}, {Value: "editor", Label: "Editor"}},
		DefaultValues: []string{"admin"},
	}

	validated, issues := validate([]schema.Field{roles}, store.Values{}, true, nil)
	if len(issues) != 0 {
		t.Fatalf("default issues = %#v", issues)
	}
	defaults, valid := validated["roles"].Values()
	defaultRole := ""
	if len(defaults) != 0 {
		defaultRole, _ = defaults[0].StringValue()
	}
	if !valid || len(defaults) != 1 || defaultRole != "admin" {
		t.Fatalf("validated defaults = %#v", validated["roles"])
	}

	validated, issues = validate([]schema.Field{roles}, store.Values{"roles": store.List(store.String("editor"), store.String("admin"))}, true, nil)
	if len(issues) != 0 {
		t.Fatalf("ordered values issues = %#v", issues)
	}
	values, _ := validated["roles"].Values()
	first, _ := values[0].StringValue()
	second, _ := values[1].StringValue()
	if first != "editor" || second != "admin" {
		t.Fatalf("ordered values = %#v", values)
	}

	_, issues = validate([]schema.Field{roles}, store.Values{"roles": store.List(store.String("admin"), store.String("admin"), store.String("owner"), store.Number(2))}, true, nil)
	want := map[string]string{
		"roles.1": "duplicate_choice",
		"roles.2": "invalid_choice",
		"roles.3": "invalid_type",
	}
	for _, issue := range issues {
		if want[issue.Path] == issue.Code {
			delete(want, issue.Path)
		}
	}
	if len(want) != 0 {
		t.Fatalf("multi-select issues = %#v, missing %#v", issues, want)
	}

	_, issues = validate([]schema.Field{roles}, store.Values{"roles": store.List()}, true, nil)
	if len(issues) != 1 || issues[0].Code != "required" || issues[0].Path != "roles" {
		t.Fatalf("empty required issues = %#v", issues)
	}
}

func TestNestedDefaultsMaterializeForAbsentGroupsAndRepeatingRows(t *testing.T) {
	defaultTheme := "dark"
	defaultGroup := func(path string) schema.Field {
		theme := validationField(t, path+".theme", schema.FieldTypeText, false)
		theme.Default = &defaultTheme
		group := validationField(t, path, schema.FieldTypeGroup, false)
		group.Nested = &schema.NestedField{Fields: []schema.Field{theme}}
		return group
	}

	settings := defaultGroup("settings")
	items := validationField(t, "items", schema.FieldTypeArray, false)
	items.Nested = &schema.NestedField{Fields: []schema.Field{defaultGroup("items.settings")}}
	content := validationField(t, "content", schema.FieldTypeBlocks, false)
	content.Blocks = &schema.BlocksField{Types: []schema.BlockType{{
		Key: "hero", Label: "Hero", Fields: []schema.Field{defaultGroup("content.hero.settings")},
	}}}

	validated, issues := validate(
		[]schema.Field{settings, items, content},
		store.Values{
			"items": store.List(store.Object(store.Values{"_key": store.String("item-1")})),
			"content": store.List(store.Object(store.Values{
				"_key": store.String("block-1"), "blockType": store.String("hero"),
			})),
		},
		true,
		nil,
	)
	if len(issues) != 0 {
		t.Fatalf("nested default issues = %#v", issues)
	}
	assertTheme := func(label string, values store.Values) {
		t.Helper()
		group, valid := values["settings"].ObjectValue()
		if !valid {
			t.Fatalf("%s settings = %#v, want object", label, values["settings"])
		}
		theme, valid := group["theme"].StringValue()
		if !valid || theme != defaultTheme {
			t.Fatalf("%s theme = %#v, want %q", label, group["theme"], defaultTheme)
		}
	}
	assertTheme("root", validated)
	itemValues, _ := validated["items"].Values()
	item, _ := itemValues[0].ObjectValue()
	assertTheme("array row", item)
	blockValues, _ := validated["content"].Values()
	block, _ := blockValues[0].ObjectValue()
	assertTheme("block row", block)
}

func TestConditionalAbsentGroupMaterializesDefaultsAndStillRequiresNestedFields(t *testing.T) {
	defaultKind := "reference"
	kind := validationField(t, "redirect.kind", schema.FieldTypeRadio, false)
	kind.Default = &defaultKind
	kind.Select = &schema.SelectField{Choices: []schema.SelectChoice{{Value: defaultKind, Label: "Reference"}}}
	reference := validationField(t, "redirect.reference", schema.FieldTypeRelationship, true)
	reference.Relationship = &schema.RelationshipField{}
	redirect := validationField(t, "redirect", schema.FieldTypeGroup, false)
	redirect.Admin.Condition = &schema.FieldCondition{Kind: schema.FieldConditionKindPredicate}
	redirect.Nested = &schema.NestedField{Fields: []schema.Field{kind, reference}}

	validated, issues := validate([]schema.Field{redirect}, store.Values{}, true, nil)
	if len(issues) != 1 || issues[0].Code != "required" || issues[0].Path != "redirect.reference" {
		t.Fatalf("conditional absent group issues = %#v, want required redirect.reference", issues)
	}
	group, valid := validated["redirect"].ObjectValue()
	if !valid {
		t.Fatalf("conditional absent group was not materialized: %#v", validated["redirect"])
	}
	value, valid := group["kind"].StringValue()
	if !valid || value != defaultKind {
		t.Fatalf("conditional group default = %#v, want %q", group["kind"], defaultKind)
	}
	_, issues = validate([]schema.Field{redirect}, validated, true, nil)
	if len(issues) != 1 || issues[0].Code != "required" || issues[0].Path != "redirect.reference" {
		t.Fatalf("revalidated conditional defaults issues = %#v, want required redirect.reference", issues)
	}
}

func validationField(t *testing.T, path string, fieldType schema.FieldType, required bool) schema.Field {
	t.Helper()
	parsed, err := query.ParsePath(path)
	if err != nil {
		t.Fatal(err)
	}
	name := parsed.Segments()[len(parsed.Segments())-1]
	return schema.Field{
		ID: schema.StableID("test-" + name), Name: name, Path: parsed, Type: fieldType,
		Required: required, Admin: schema.FieldAdmin{Label: name},
	}
}

func withSelect(field schema.Field, choices ...schema.SelectChoice) schema.Field {
	field.Select = &schema.SelectField{Choices: choices}
	return field
}

func withRelationship(field schema.Field) schema.Field {
	field.Relationship = &schema.RelationshipField{}
	return field
}

func withUpload(field schema.Field) schema.Field {
	field.Upload = &schema.UploadField{}
	return field
}
