package field_test

import (
	"math"
	"reflect"
	"slices"
	"testing"

	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/store"
)

func TestOptionAndDefaultInputsAreOwned(t *testing.T) {
	options := []field.Option{{Value: "draft", Label: "Draft", LabelTranslations: map[string]string{"fr": "Brouillon"}}}
	declaration := field.Select("status").Options(options...).Default("draft")
	options[0].Value = "changed"
	options[0].LabelTranslations["fr"] = "changed"
	view := field.Snapshot(declaration)
	returned := view.Options()
	returned[0].Value = "changed"
	returned[0].LabelTranslations["fr"] = "changed"
	if got := view.Options()[0]; got.Value != "draft" || got.LabelTranslations["fr"] != "Brouillon" {
		t.Fatal("option input/output alias")
	}
	if value, ok := view.Default(); !ok || value.Kind() != field.DefaultString || value.String() != "draft" {
		t.Fatal(value, ok)
	}
	defaults := []string{"admin", "editor"}
	multi := field.Snapshot(field.MultiSelect("roles", "admin", "editor").Default(defaults...).Required())
	defaults[0] = "changed"
	returnedDefaults := multi.SelectDefaults()
	returnedDefaults[0] = "changed"
	if !multi.SelectHasMany() || !slices.Equal(multi.SelectDefaults(), []string{"admin", "editor"}) {
		t.Fatal("multi-select shape/default ownership")
	}
}

func TestDefaultTypesAndScalarConstraints(t *testing.T) {
	nodes := field.Fields{field.Text("title").Default("Untitled"), field.Number("score").Default(12.5), field.Checkbox("featured").Default(true)}
	kinds := []field.DefaultKind{field.DefaultString, field.DefaultNumber, field.DefaultBoolean}
	values := []string{"Untitled", "12.5", "true"}
	for i, node := range nodes {
		v, ok := field.Snapshot(node).Default()
		if !ok || v.Kind() != kinds[i] || v.String() != values[i] {
			t.Fatal(v, ok)
		}
	}
	title := field.Snapshot(field.Text("title").MinLength(2).MaxLength(12).Index())
	min, minOK := title.MinLength()
	max, maxOK := title.MaxLength()
	if min != 2 || !minOK || max != 12 || !maxOK || !title.Index() {
		t.Fatal("text constraints")
	}
	number := field.Snapshot(field.Number("score").Min(-2.5).Max(8).Step(.25))
	low, _ := number.Min()
	high, _ := number.Max()
	step, _ := number.Step()
	if low != -2.5 || high != 8 || step != .25 {
		t.Fatal("number constraints")
	}
	for _, node := range (field.Fields{field.Text("title").MinLength(4).MaxLength(2), field.Number("score").Min(4).Max(2), field.Number("score").Step(0), field.Number("score").Min(math.Inf(1)), field.Text("title").MinLength(2).Default("🙂"), field.Number("score").Max(2).Default(3)}) {
		if len(field.Snapshot(node).Issues()) == 0 {
			t.Fatalf("invalid constraints accepted: %s", node.Name())
		}
	}
}

func TestLayoutAndBlockInputsAreOwned(t *testing.T) {
	children := field.Fields{field.Text("title")}
	row := field.Snapshot(field.Row(children))
	tabs := field.Snapshot(field.Tabs(field.Fields{field.UnnamedTab("Content", children), field.NamedTab("seo", "SEO", field.Fields{field.Text("title")}).Required()}))
	block := field.Block{Slug: "copy", Labels: field.BlockLabels{SingularTranslations: map[string]string{"fr": "Texte"}}, Fields: children}
	blocks := field.Snapshot(field.Blocks("content", block).MinRows(1).MaxRows(4).Admin(field.Admin{RowLabel: field.Component("app:summary")}))
	children[0] = field.Text("changed")
	block.Labels.SingularTranslations["fr"] = "changed"
	got := row.Fields()
	got[0] = field.Text("changed")
	if row.Fields()[0].Name() != "title" || row.Category() != field.CategoryPresentation {
		t.Fatal("row alias or shape")
	}
	branches := tabs.Branches()
	if len(branches) != 1 || len(branches[0].Fields) != 2 {
		t.Fatal("tab graph scope")
	}
	named := field.Snapshot(branches[0].Fields[1])
	if named.Name() != "seo" || !named.IsNamedTab() || !named.Required() {
		t.Fatal("named tab lost object contract")
	}
	gotBlocks := blocks.Blocks()
	gotBlocks[0].Fields[0] = field.Text("changed")
	gotBlocks[0].Labels.SingularTranslations["fr"] = "changed"
	if blocks.Blocks()[0].Fields[0].Name() != "title" || blocks.Blocks()[0].Labels.SingularTranslations["fr"] != "Texte" || blocks.MinRows() != 1 || blocks.MaxRows() != 4 {
		t.Fatal("block alias or bounds")
	}
}

func TestConditionsAreScopedAndOwned(t *testing.T) {
	children := []field.Condition{field.Equal(field.Root("status"), "published"), field.OneOf(field.Sibling("mode"), "public", "preview")}
	declaration := field.Text("title").Admin(field.Admin{VisibleWhen: field.All(children...)})
	children[0] = field.Equal(field.Root("other"), "changed")
	condition := field.Snapshot(declaration).AdminPolicy().VisibleWhen
	if condition.IsZero() || condition.Kind() != field.ConditionKindAll {
		t.Fatal("missing condition")
	}
	returned := condition.Conditions()
	returned[0] = field.Equal(field.Root("other"), "changed")
	if condition.Conditions()[0].Reference().Path() != "status" || condition.Conditions()[1].Reference().Scope() != field.SiblingScope {
		t.Fatal("condition alias or scope")
	}
	for _, value := range []field.Condition{field.All(), field.Not(field.Condition{}), field.Equal(field.Root(""), "x"), field.OneOf[string](field.Root("mode"))} {
		issues := field.Snapshot(field.Text("title").Admin(field.Admin{VisibleWhen: value})).Issues()
		if len(issues) == 0 {
			t.Fatal("invalid condition accepted")
		}
		for _, issue := range issues {
			if issue.Path == "" {
				t.Fatal("unlocated condition issue")
			}
		}
	}
}

func TestDateAndPlaceholderPoliciesValidateCurrentShape(t *testing.T) {
	for _, appearance := range []field.DateFormat{field.DateOnly, field.DateTime, field.TimeOnly} {
		d := field.Snapshot(field.Date("published").Format(appearance))
		if d.DateFormat() != appearance || len(d.Issues()) > 0 {
			t.Fatal(d.Issues())
		}
	}
	for _, node := range (field.Fields{field.Date("date").Format("invalid"), field.Checkbox("enabled").Admin(field.Admin{Placeholder: "Enabled"}), field.Text("title").Admin(field.Admin{PlaceholderTranslations: map[string]string{"fr": "Titre"}})}) {
		if len(field.Snapshot(node).Issues()) == 0 {
			t.Fatal("invalid presentation accepted")
		}
	}
}

func TestJoinAndReferencePoliciesAreOwnedAndValidated(t *testing.T) {
	columns := []string{"title", "status"}
	join := field.Snapshot(field.Join("comments", "comments", "post").Limit(25).DefaultColumns(columns...).DefaultSort("-title").AllowCreate(true))
	columns[0] = "changed"
	returned := join.JoinDefaultColumns()
	returned[0] = "changed"
	if join.JoinLimit() != 25 || join.JoinDefaultSort() != "-title" || !join.JoinAllowCreate() || join.JoinDefaultColumns()[0] != "title" {
		t.Fatal("join config alias")
	}
	rule := field.OptionFilterValue("active", field.FilterEquals, true)
	rules := []field.RelationshipFilterRule{rule}
	reference := field.Snapshot(field.Relationship("author", "users").FilterOptionRules(rules...).OnDelete(field.ReferenceDeleteRestrict))
	rules[0].TargetPath = "changed"
	returnedRules := reference.RelationshipFilters()
	returnedRules[0].TargetPath = "changed"
	if reference.RelationshipFilters()[0].TargetPath != "active" || reference.ReferenceDeleteAction() != field.ReferenceDeleteRestrict {
		t.Fatal("reference policy alias")
	}
	for _, node := range (field.Fields{field.Join("comments", "comments", "post").Limit(0), field.Join("comments", "comments", "post").DefaultColumns("title", "title"), field.Relationship("author", "users").OnDelete("bad"), field.Upload("image", "media").FilterOptionRules(field.OptionFilter("", "bad", ""))}) {
		if len(field.Snapshot(node).Issues()) == 0 {
			t.Fatal("invalid policy accepted")
		}
	}
	if d := field.Snapshot(field.Relationship("author", "users").OnDelete("bad").OnDelete(field.ReferenceDeleteRestrict)); len(d.Issues()) != 0 {
		t.Fatal("replacement retained invalid policy", d.Issues())
	}
}

func TestPublicDisplayPoliciesOwnTranslations(t *testing.T) {
	translations := map[string]string{"fr": "Titre"}
	labels := field.RowLabels{Singular: "Entry", Plural: "Entries", SingularTranslations: translations}
	declaration := field.Array("rows", field.Fields{field.Text("title")}).LabelTranslations(translations).Admin(field.Admin{LabelTranslations: translations, Description: "Help", DescriptionTranslations: translations, Placeholder: "Choose", PlaceholderTranslations: translations, RowLabels: labels})
	translations["fr"] = "changed"
	view := field.Snapshot(declaration)
	a := view.AdminPolicy()
	a.DescriptionTranslations["fr"] = "changed"
	a.RowLabels.SingularTranslations["fr"] = "changed"
	if view.LabelTranslations()["fr"] != "Titre" || view.DescriptionTranslations()["fr"] != "Titre" || view.RowLabels().SingularTranslations["fr"] != "Titre" {
		t.Fatal("translations alias")
	}
}

func TestConcreteFacadeCategoriesAndCallbackShapes(t *testing.T) {
	cases := []struct {
		node     field.Node
		category field.Category
	}{
		{field.Text("text"), field.CategoryScalar},
		{field.Code("code"), field.CategoryScalar},
		{field.Textarea("textarea"), field.CategoryScalar},
		{field.Email("email"), field.CategoryScalar},
		{field.Date("date"), field.CategoryScalar},
		{field.Number("number"), field.CategoryScalar},
		{field.Checkbox("checkbox"), field.CategoryScalar},
		{field.JSON("json"), field.CategoryScalar},
		{field.Point("point"), field.CategoryScalar},
		{field.Select("select"), field.CategoryScalar},
		{field.Radio("radio"), field.CategoryScalar},
		{field.MultiSelect("many"), field.CategoryScalar},
		{field.Relationship("ref", "users"), field.CategoryRelationship},
		{field.Relationships("refs", "users"), field.CategoryRelationship},
		{field.PolymorphicRelationship("poly", "users", "teams"), field.CategoryRelationship},
		{field.Upload("image", "media"), field.CategoryUpload},
		{field.Uploads("images", "media"), field.CategoryUpload},
		{field.Group("group", nil), field.CategoryNested},
		{field.Array("array", nil), field.CategoryNested},
		{field.Blocks("blocks"), field.CategoryNested},
		{field.Row(nil), field.CategoryPresentation},
		{field.UI("ui"), field.CategoryPresentation},
		{field.Join("inverse", "posts", "author"), field.CategoryPresentation},
		{field.Plugin("rich", "rich", nil), field.CategoryPlugin},
		{field.Virtual("computed", field.ValueString, func(operation.Context) (operation.Value[store.Value], error) {
			return operation.Present(store.String("computed")), nil
		}), field.CategoryPresentation},
	}
	for _, test := range cases {
		if got := field.Snapshot(test.node).Category(); got != test.category {
			t.Errorf("%T category = %q, want %q", test.node, got, test.category)
		}
	}
	if _, err := field.AsRelationship(field.Relationships("many", "users")); err == nil {
		t.Fatal("singular facade accepted a list")
	}
	if _, err := field.AsSelect(field.MultiSelect("many")); err == nil {
		t.Fatal("singular option facade accepted a list")
	}
	if reflect.TypeOf(field.Number("n").Label("N").Min(1)) != reflect.TypeOf(field.Number("n")) {
		t.Fatal("fluent concrete type erased")
	}
}

func TestPolymorphicConstructorsRequireDistinctTargets(t *testing.T) {
	for _, node := range (field.Fields{field.PolymorphicRelationship("owner"), field.PolymorphicRelationship("owner", "users"), field.PolymorphicRelationships("owners", "users", "users")}) {
		if !slices.ContainsFunc(field.Snapshot(node).Issues(), func(issue field.Issue) bool { return issue.Code == "invalid_polymorphic_targets" }) {
			t.Fatal("ambiguous polymorphic declaration accepted")
		}
	}
}

func TestBlockSnapshotIsDetached(t *testing.T) {
	labels := field.BlockLabels{Singular: "Banner", Plural: "Banners", SingularTranslations: map[string]string{"fr": "Bannière"}, PluralTranslations: map[string]string{"fr": "Bannières"}}
	original := field.Block{TypeName: "Hero", Slug: "hero", Labels: labels, Fields: field.Fields{field.Text("heading")}}
	clone := original.Snapshot()
	if clone.TypeName != original.TypeName || clone.Slug != original.Slug {
		t.Fatal("snapshot changed block identity")
	}
	clone.Fields[0] = field.Text("changed")
	if original.Fields[0].Name() != "heading" {
		t.Fatal("snapshot aliases the authored field slice")
	}
	clone.Labels.SingularTranslations["fr"] = "Changed"
	clone.Labels.PluralTranslations["fr"] = "Changed"
	if original.Labels.SingularTranslations["fr"] != "Bannière" || original.Labels.PluralTranslations["fr"] != "Bannières" {
		t.Fatal("snapshot aliases authored translations")
	}
}
