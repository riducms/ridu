package field_test

import (
	"encoding/json"
	"math"
	"slices"
	"testing"

	"github.com/riducms/ridu/field"
)

func TestAdminComponentConfigIsImmutable(t *testing.T) {
	config := json.RawMessage(`{"generate":true}`)
	definition := field.Text("title", field.AdminComponent("seo", "title", config))
	config[2] = 'X'
	plugin, component, returned, exists := definition.AdminComponent()
	if !exists || plugin != "seo" || component != "title" || string(returned) != `{"generate":true}` {
		t.Fatalf("admin component = %q/%q/%s/%v", plugin, component, returned, exists)
	}
	returned[2] = 'Y'
	_, _, again, _ := definition.AdminComponent()
	if string(again) != `{"generate":true}` {
		t.Fatalf("mutated admin component config = %s", again)
	}
}

func TestAdminComponentRequiresObjectConfiguration(t *testing.T) {
	definition := field.Text("title", field.AdminComponent("seo", "title", json.RawMessage(`true`)))
	for _, issue := range definition.Issues() {
		if issue.Code == "invalid_admin_component_config" {
			return
		}
	}
	t.Fatalf("scalar admin component issues = %#v", definition.Issues())
}

func TestRowLabelComponentConfigIsImmutableForArraysAndBlocks(t *testing.T) {
	config := json.RawMessage(`{"key":"optionKey","label":"label"}`)
	array := field.Array(
		"options",
		field.RowLabel("label"),
		field.RowLabelComponent("curriculum", "questionOption", config),
		field.Fields(field.Text("optionKey"), field.Text("label")),
	)
	blocks := field.Blocks(
		"content",
		field.RowLabelComponent("curriculum", "blockSummary", config),
		field.BlockTypes(field.BlockType("copy", "Copy", field.Text("label"))),
	)
	config[2] = 'X'

	for _, definition := range []field.Definition{array, blocks} {
		plugin, component, returned, exists := definition.RowLabelComponent()
		if !exists || plugin != "curriculum" || component == "" || string(returned) != `{"key":"optionKey","label":"label"}` {
			t.Fatalf("%s row label component = %q/%q/%s/%v", definition.Kind(), plugin, component, returned, exists)
		}
		returned[2] = 'Y'
		_, _, again, _ := definition.RowLabelComponent()
		if string(again) != `{"key":"optionKey","label":"label"}` {
			t.Fatalf("%s row label component config was mutable: %s", definition.Kind(), again)
		}
	}
	if array.RowLabel() != "label" {
		t.Fatalf("legacy row label = %q, want label", array.RowLabel())
	}

	invalid := field.Array(
		"items",
		field.RowLabelComponent("curriculum", "summary", json.RawMessage(`true`)),
		field.Fields(field.Text("label")),
	)
	if !slices.ContainsFunc(invalid.Issues(), func(issue field.Issue) bool {
		return issue.Code == "invalid_row_label_component_config" && issue.Path == "options.rowLabelComponent.config"
	}) {
		t.Fatalf("invalid row label component issues = %#v", invalid.Issues())
	}
}

func TestDefinitionCopiesOptionInputs(t *testing.T) {
	choices := []field.Choice{{Value: "draft", Label: "Draft"}}
	definition := field.Select(
		"status",
		field.Choices(choices...),
		field.Default("draft"),
	)

	choices[0].Value = "mutated"
	returned := definition.Choices()
	returned[0].Value = "also-mutated"

	if got := definition.Choices()[0].Value; got != "draft" {
		t.Fatalf("definition choice = %q, want immutable draft", got)
	}
	if got, exists := definition.Default(); !exists || got.Kind() != field.DefaultString || got.String() != "draft" {
		t.Fatalf("definition default = %#v, %v; want string draft, true", got, exists)
	}
}

func TestMultipleSelectCardinalityAndDefaultsAreImmutable(t *testing.T) {
	defaults := []string{"admin", "editor"}
	definition := field.Select(
		"roles",
		field.OneOf("admin", "editor"),
		field.Multiple(),
		field.DefaultChoices(defaults...),
		field.Required(),
	)
	defaults[0] = "mutated"
	returned := definition.SelectDefaults()
	returned[0] = "also-mutated"

	if !definition.SelectHasMany() || !slices.Equal(definition.SelectDefaults(), []string{"admin", "editor"}) {
		t.Fatalf("multi-select = %v %#v", definition.SelectHasMany(), definition.SelectDefaults())
	}
}

func TestMultipleSelectRejectsIncompatibleDefaultsAndRadioCardinality(t *testing.T) {
	tests := []struct {
		name       string
		definition field.Definition
		code       string
		path       string
	}{
		{
			name:       "scalar default",
			definition: field.Select("roles", field.OneOf("admin"), field.Multiple(), field.Default("admin")),
			code:       "invalid_default", path: "options.default",
		},
		{
			name:       "list default without multiple",
			definition: field.Select("roles", field.OneOf("admin"), field.DefaultChoices("admin")),
			code:       "invalid_default", path: "options.default",
		},
		{
			name:       "radio multiple",
			definition: field.Radio("role", field.OneOf("admin"), field.Multiple()),
			code:       "invalid_radio_cardinality", path: "options.hasMany",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !slices.ContainsFunc(test.definition.Issues(), func(issue field.Issue) bool {
				return issue.Code == test.code && issue.Path == test.path
			}) {
				t.Fatalf("issues = %#v, want %s at %s", test.definition.Issues(), test.code, test.path)
			}
		})
	}
}

func TestScalarConstraintsAndIndexOptionsAreTypedAndValidated(t *testing.T) {
	title := field.Text("title", field.MinLength(2), field.MaxLength(12), field.Index())
	minimum, hasMinimum := title.MinLength()
	maximum, hasMaximum := title.MaxLength()
	if minimum != 2 || !hasMinimum || maximum != 12 || !hasMaximum || !title.Index() {
		t.Fatalf("text constraints/index = %d/%v %d/%v %v", minimum, hasMinimum, maximum, hasMaximum, title.Index())
	}
	score := field.Number("score", field.Min(-2.5), field.Max(8), field.Step(0.25))
	minimumNumber, hasMinimumNumber := score.Min()
	maximumNumber, hasMaximumNumber := score.Max()
	step, hasStep := score.Step()
	if minimumNumber != -2.5 || !hasMinimumNumber || maximumNumber != 8 || !hasMaximumNumber || step != 0.25 || !hasStep {
		t.Fatalf("number constraints = %v/%v %v/%v %v/%v", minimumNumber, hasMinimumNumber, maximumNumber, hasMaximumNumber, step, hasStep)
	}

	tests := []struct {
		definition field.Definition
		code       string
		path       string
	}{
		{field.Email("email", field.MinLength(2)), "incompatible_option", "options.minLength"},
		{field.Date("date", field.MaxLength(5)), "incompatible_option", "options.maxLength"},
		{field.Text("title", field.MinLength(4), field.MaxLength(2)), "invalid_length_bounds", "options.minLength"},
		{field.Number("score", field.Min(4), field.Max(2)), "invalid_number_bounds", "options.min"},
		{field.Number("score", field.Step(0)), "invalid_step", "options.step"},
		{field.Number("score", field.Min(math.Inf(1))), "invalid_min", "options.min"},
		{field.Text("title", field.MinLength(2), field.Default("🙂")), "invalid_default", "options.default"},
		{field.Number("score", field.Max(2), field.Default(3)), "invalid_default", "options.default"},
	}
	for _, test := range tests {
		if !slices.ContainsFunc(test.definition.Issues(), func(issue field.Issue) bool {
			return issue.Code == test.code && issue.Path == test.path
		}) {
			t.Errorf("%s issues = %#v, want %s at %s", test.definition.Name(), test.definition.Issues(), test.code, test.path)
		}
	}
}

func TestRowCopiesChildrenAndIsPresentationOnly(t *testing.T) {
	children := []field.Definition{field.Text("title"), field.Select("status")}
	row := field.Row(children...)
	children[0] = field.Text("mutated")

	returned := row.Fields()
	returned[0] = field.Text("alsoMutated")

	if got := row.Fields()[0].Name(); got != "title" {
		t.Fatalf("row child name = %q, want immutable title", got)
	}
	if row.Name() != "" || row.Category() != field.CategoryPresentation {
		t.Fatalf("row name/category = %q/%q, want unnamed presentation field", row.Name(), row.Category())
	}
}

func TestTabsCopyChildrenAndDistinguishStoredNames(t *testing.T) {
	children := []field.Definition{field.Text("title")}
	tabs := field.Tabs(
		field.UnnamedTab("Content", children...),
		field.NamedTab("seo", "SEO", field.Text("title")),
	)
	children[0] = field.Text("mutated")
	returned := tabs.Tabs()
	returned[0].Fields[0] = field.Text("alsoMutated")

	got := tabs.Tabs()
	if got[0].Name != "" || got[0].Fields[0].Name() != "title" {
		t.Fatalf("unnamed tab = %#v, want presentation-only content tab", got[0])
	}
	if got[1].Name != "seo" || got[1].Label != "SEO" {
		t.Fatalf("named tab = %#v, want seo/SEO", got[1])
	}
	if tabs.Name() != "" || tabs.Category() != field.CategoryPresentation {
		t.Fatalf("tabs name/category = %q/%q, want unnamed presentation field", tabs.Name(), tabs.Category())
	}
}

func TestMismatchedDefaultTypesArePathAware(t *testing.T) {
	definition := field.Number("score", field.Default("invalid"))

	issues := definition.Issues()
	paths := make([]string, len(issues))
	for index, issue := range issues {
		paths[index] = issue.Path
	}
	if !slices.Contains(paths, "options.default") {
		t.Fatalf("issue paths = %v, want invalid default path", paths)
	}
}

func TestDefaultsPreserveScalarTypes(t *testing.T) {
	tests := []struct {
		definition field.Definition
		kind       field.DefaultKind
		value      string
	}{
		{field.Text("title", field.Default("Untitled")), field.DefaultString, "Untitled"},
		{field.Number("score", field.Default(12.5)), field.DefaultNumber, "12.5"},
		{field.Checkbox("featured", field.Default(true)), field.DefaultBoolean, "true"},
	}

	for _, test := range tests {
		got, exists := test.definition.Default()
		if !exists || got.Kind() != test.kind || got.String() != test.value {
			t.Errorf("%s default = %#v, %v; want %s %q", test.definition.Name(), got, exists, test.kind, test.value)
		}
	}
}

func TestConditionsAreTypedScopedAndImmutable(t *testing.T) {
	condition := field.All(
		field.Document("status", field.ConditionOneOf, "draft", "published"),
		field.Not(field.Sibling("archived", field.ConditionEquals, true)),
	)
	definition := field.Text("summary", field.ShowWhenCondition(condition))
	resolved := definition.Condition()
	if resolved == nil || resolved.Kind() != field.ConditionKindAll {
		t.Fatalf("condition = %#v, want all expression", resolved)
	}
	children := resolved.Conditions()
	if len(children) != 2 || children[0].Scope() != field.ConditionScopeDocument || children[0].Operator() != field.ConditionOneOf {
		t.Fatalf("condition children = %#v", children)
	}
	values := children[0].Values()
	if len(values) != 2 || values[0].Kind() != field.DefaultString || values[1].String() != "published" {
		t.Fatalf("condition values = %#v", values)
	}
	children[0] = field.Sibling("mutated", field.ConditionEquals, "yes")
	values[0] = field.DefaultValue{}
	if again := definition.Condition().Conditions(); again[0].Path() != "status" || again[0].Values()[0].String() != "draft" {
		t.Fatalf("condition mutated through accessor = %#v", again)
	}

	concise := field.Text("venue", field.ShowWhen("online", "false")).Condition()
	if concise == nil || concise.Scope() != field.ConditionScopeSibling || concise.Operator() != field.ConditionEquals || concise.Values()[0].String() != "false" {
		t.Fatalf("ShowWhen condition = %#v", concise)
	}
}

func TestInvalidConditionExpressionsProducePathAwareIssues(t *testing.T) {
	tests := []struct {
		definition field.Definition
		code       string
		path       string
	}{
		{field.Text("title", field.ShowWhenCondition(field.All(field.Document("status", field.ConditionEquals, "draft")))), "invalid_condition_group", "options.condition.conditions"},
		{field.Text("title", field.ShowWhenCondition(field.Document("status", field.ConditionEquals, "draft", "published"))), "invalid_condition_values", "options.condition.values"},
		{field.Text("title", field.ShowWhenCondition(field.Document("status", field.ConditionOneOf, "draft", "draft"))), "duplicate_condition_value", "options.condition.values[1]"},
		{field.Text("title", field.ShowWhenCondition(field.Document("score", field.ConditionEquals, math.NaN()))), "invalid_condition_value", "options.condition.values[0]"},
	}
	for _, test := range tests {
		if !slices.ContainsFunc(test.definition.Issues(), func(issue field.Issue) bool {
			return issue.Code == test.code && issue.Path == test.path
		}) {
			t.Errorf("condition issues = %#v, want %s at %s", test.definition.Issues(), test.code, test.path)
		}
	}
}

func TestDatePickerAppearanceIsTypedAndValidated(t *testing.T) {
	date := field.Date("startsAt", field.PickerAppearance(field.DatePickerDayAndTime))
	if got := date.DatePickerAppearance(); got != field.DatePickerDayAndTime {
		t.Fatalf("date picker appearance = %q, want %q", got, field.DatePickerDayAndTime)
	}
	if got := field.Date("birthday").DatePickerAppearance(); got != field.DatePickerDayOnly {
		t.Fatalf("default date picker appearance = %q, want %q", got, field.DatePickerDayOnly)
	}
	invalid := field.Text("title", field.PickerAppearance(field.DatePickerTimeOnly))
	if !slices.ContainsFunc(invalid.Issues(), func(issue field.Issue) bool {
		return issue.Code == "incompatible_option" && issue.Path == "options.pickerAppearance"
	}) {
		t.Fatalf("incompatible picker issues = %#v", invalid.Issues())
	}
}

func TestJoinTableOptionsAreTypedAndCopied(t *testing.T) {
	columns := []string{"title", "status"}
	join := field.Join(
		"posts",
		"posts",
		"category",
		field.JoinColumns(columns...),
		field.JoinDefaultSort("-updatedAt"),
		field.JoinAllowCreate(false),
	)
	columns[0] = "mutated"
	returned := join.JoinDefaultColumns()
	returned[0] = "also-mutated"

	if got := join.JoinDefaultColumns(); !slices.Equal(got, []string{"title", "status"}) {
		t.Fatalf("join columns = %v", got)
	}
	if join.JoinDefaultSort() != "-updatedAt" || join.JoinAllowCreate() {
		t.Fatalf("join sort/create = %q/%v", join.JoinDefaultSort(), join.JoinAllowCreate())
	}
	if !field.Join("posts", "posts", "category").JoinAllowCreate() {
		t.Fatal("join create should default to enabled")
	}
}

func TestRelationshipOptionRulesAreTypedAndCopied(t *testing.T) {
	rules := []field.RelationshipFilterRule{
		field.OptionFilter("seo.title", field.FilterNotEquals, "seo.title"),
		field.OptionFilterFor("pages", "navigation.showInHeader", field.FilterEquals, "featured"),
		field.OptionFilterValueFor("posts", "_status", field.FilterEquals, "published"),
	}
	relationship := field.Relationship("related", field.ToAny("posts", "pages"), field.FilterOptionRules(rules...))
	rules[0].TargetPath = "mutated"
	returned := relationship.RelationshipFilters()
	returned[0].TargetPath = "also-mutated"

	got := relationship.RelationshipFilters()
	if got[0].TargetPath != "seo.title" || got[0].Operator != field.FilterNotEquals || got[1].Collection != "pages" || got[2].Literal == nil || got[2].Literal.String() != "published" {
		t.Fatalf("relationship option rules = %#v", got)
	}
	got[2].Literal = nil
	if relationship.RelationshipFilters()[2].Literal == nil {
		t.Fatal("relationship literal filter was mutated through accessor")
	}
	invalid := field.Relationship("related", field.FilterOptionRules(field.OptionFilter("title", "unknown", "title")))
	if !slices.ContainsFunc(invalid.Issues(), func(issue field.Issue) bool {
		return issue.Code == "invalid_relationship_filter_operator"
	}) {
		t.Fatalf("invalid relationship option rules = %#v", invalid.Issues())
	}
	upload := field.Upload("hero", field.To("media"), field.FilterOptionRules(field.OptionFilter("mimeType", field.FilterEquals, "assetType")), field.FilterOptionRules(
		field.OptionFilter("filename", field.FilterLike, "assetName"),
	))
	if filters := upload.RelationshipFilters(); len(filters) != 2 || filters[0].TargetPath != "mimeType" || filters[0].Operator != field.FilterEquals || filters[1].TargetPath != "filename" || filters[1].Operator != field.FilterLike {
		t.Fatalf("upload option filters = %#v", filters)
	}
}

func TestReferenceDeleteOptionsAreTypedAndRejectInvalidOrDuplicateActions(t *testing.T) {
	relationship := field.Relationship("owner", field.To("users"), field.OnDelete(field.ReferenceDeleteRestrict))
	if got := relationship.ReferenceDeleteAction(); got != field.ReferenceDeleteRestrict {
		t.Fatalf("relationship on-delete action = %q", got)
	}
	upload := field.Upload("asset", field.To("media"), field.OnDelete(field.ReferenceDeleteNullify))
	if got := upload.ReferenceDeleteAction(); got != field.ReferenceDeleteNullify {
		t.Fatalf("upload on-delete action = %q", got)
	}
	invalid := field.Relationship("owner", field.To("users"),
		field.OnDelete(field.ReferenceDeleteAction("cascade")),
		field.OnDelete(field.ReferenceDeleteRestrict),
	)
	for _, code := range []string{"invalid_reference_delete_action", "duplicate_option"} {
		if !slices.ContainsFunc(invalid.Issues(), func(issue field.Issue) bool {
			return issue.Code == code && issue.Path == "options.onDelete"
		}) {
			t.Errorf("missing %s issue in %#v", code, invalid.Issues())
		}
	}
}

func TestFieldCategories(t *testing.T) {
	tests := []struct {
		definition field.Definition
		want       field.Category
	}{
		{field.Text("title"), field.CategoryScalar},
		{field.Select("status"), field.CategoryScalar},
		{field.Code("source", field.Language("go")), field.CategoryScalar},
		{field.Radio("priority", field.OneOf("low", "high")), field.CategoryScalar},
		{field.Point("location"), field.CategoryScalar},
		{field.Relationship("author"), field.CategoryRelationship},
		{field.Group("seo"), field.CategoryNested},
		{field.Tabs(field.UnnamedTab("Content", field.Text("title"))), field.CategoryPresentation},
		{field.Row(field.Text("summary")), field.CategoryPresentation},
		{field.UI("guide"), field.CategoryPresentation},
		{field.Collapsible("details", true, field.Text("summary")), field.CategoryPresentation},
	}

	for _, test := range tests {
		if got := test.definition.Category(); got != test.want {
			t.Errorf("%s category = %q, want %q", test.definition.Kind(), got, test.want)
		}
	}
}

func TestArrayBoundsAndLabelsAreImmutable(t *testing.T) {
	definition := field.Array("team", field.MinRows(1), field.MaxRows(3), field.RowLabel("name"), field.Fields(field.Text("name")))
	if definition.MinRows() != 1 || definition.MaxRows() != 3 || definition.RowLabel() != "name" {
		t.Fatalf("array metadata = %d %d %q", definition.MinRows(), definition.MaxRows(), definition.RowLabel())
	}
	invalid := field.Array("items", field.MinRows(4), field.MaxRows(2), field.Fields(field.Text("name")))
	if !slices.ContainsFunc(invalid.Issues(), func(issue field.Issue) bool { return issue.Code == "invalid_row_bounds" }) {
		t.Fatalf("issues = %#v, want invalid_row_bounds", invalid.Issues())
	}
}

func TestAdminDisplayTranslationsAreImmutable(t *testing.T) {
	translations := map[string]string{"fr": "Titre"}
	descriptionTranslations := map[string]string{"fr": "Titre public"}
	placeholderTranslations := map[string]string{"fr": "Saisissez un titre"}
	tabTranslations := map[string]string{"fr": "Contenu"}
	definition := field.Text(
		"title",
		field.LabelTranslations(translations),
		field.DescriptionTranslations(descriptionTranslations),
		field.Placeholder("Enter a title"),
		field.PlaceholderTranslations(placeholderTranslations),
		field.Hidden(),
		field.Sidebar(),
		field.Tab("Content"),
		field.TabTranslations(tabTranslations),
	)
	translations["fr"] = "mutated"
	descriptionTranslations["fr"] = "mutated"
	placeholderTranslations["fr"] = "mutated"
	tabTranslations["fr"] = "mutated"
	returned := definition.LabelTranslations()
	returned["fr"] = "also-mutated"
	if definition.LabelTranslations()["fr"] != "Titre" || definition.DescriptionTranslations()["fr"] != "Titre public" || definition.PlaceholderTranslations()["fr"] != "Saisissez un titre" || definition.TabTranslations()["fr"] != "Contenu" {
		t.Fatalf("field translations were mutable: labels=%#v descriptions=%#v placeholders=%#v tabs=%#v", definition.LabelTranslations(), definition.DescriptionTranslations(), definition.PlaceholderTranslations(), definition.TabTranslations())
	}
	if definition.Placeholder() != "Enter a title" || !definition.Hidden() || !definition.Sidebar() {
		t.Fatalf("field admin metadata = placeholder %q hidden=%v sidebar=%v", definition.Placeholder(), definition.Hidden(), definition.Sidebar())
	}

	rowLabels := field.RowLabels{
		Singular: "Row", Plural: "Rows",
		SingularTranslations: map[string]string{"fr": "Ligne"},
		PluralTranslations:   map[string]string{"fr": "Lignes"},
	}
	array := field.Array("items", field.ArrayRowLabels(rowLabels), field.Fields(field.Text("name")))
	rowLabels.SingularTranslations["fr"] = "mutated"
	returnedRows := array.RowLabels()
	returnedRows.PluralTranslations["fr"] = "also-mutated"
	if got := array.RowLabels(); got.SingularTranslations["fr"] != "Ligne" || got.PluralTranslations["fr"] != "Lignes" {
		t.Fatalf("array row labels were mutable: %#v", got)
	}

	choice := field.Choice{Value: "draft", Label: "Draft"}.WithLabelTranslations(map[string]string{"fr": "Brouillon"})
	block := field.BlockType("hero", "Hero", field.Text("heading")).WithLabelTranslations(map[string]string{"fr": "Bannière"})
	tab := field.UnnamedTab("Content", field.Text("body")).WithLabelTranslations(map[string]string{"fr": "Contenu"})
	if choice.LabelTranslations["fr"] != "Brouillon" || block.LabelTranslations["fr"] != "Bannière" || tab.LabelTranslations["fr"] != "Contenu" {
		t.Fatalf("translated authoring definitions = %#v %#v %#v", choice, block, tab)
	}
}

func TestPlaceholderRejectsUnsupportedControlsAndMissingFallback(t *testing.T) {
	tests := []field.Definition{
		field.Checkbox("featured", field.Placeholder("Choose")),
		field.Text("title", field.PlaceholderTranslations(map[string]string{"fr": "Titre"})),
	}
	for _, definition := range tests {
		if !slices.ContainsFunc(definition.Issues(), func(issue field.Issue) bool {
			return issue.Code == "incompatible_option" || issue.Code == "invalid_placeholder"
		}) {
			t.Errorf("%s issues = %#v, want placeholder compatibility issue", definition.Name(), definition.Issues())
		}
	}
}
