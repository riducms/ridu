package operation

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/config"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestReadOutputValidationChecksOnlyReturnedRoots(t *testing.T) {
	title := liveTestField("title", schema.FieldTypeText)
	title.Required = true
	summary := liveTestField("summary", schema.FieldTypeText)
	summary.Required = true
	tags := liveTestField("tags", schema.FieldTypeTextList)
	nestedTitle := liveTestField("seo.title", schema.FieldTypeText)
	nestedTitle.Required = true
	seo := liveTestField("seo", schema.FieldTypeGroup)
	seo.Nested = &schema.NestedField{Fields: []schema.Field{nestedTitle}}
	collection := liveTestCollection(title, summary, tags, seo)
	// A read hook replaces values between the store and the response. Here it
	// nulls the required fields that the store holds, and the store holds a
	// text list containing a number, which no write could produce.
	collection.Hooks.AfterRead = []Hook{func(ctx Context) error {
		if _, exists := ctx.Data["title"]; exists {
			ctx.Data["title"] = store.Null()
		}
		if _, exists := ctx.Data["seo"]; exists {
			ctx.Data["seo"] = store.Object(store.Values{"title": store.Null()})
		}
		return nil
	}}
	engine, id := liveTestEngine(t, collection, store.Values{
		"title": store.String("Stored"), "summary": store.String("kept"),
		"tags": store.List(store.Number(1)), "seo": store.Object(store.Values{"title": store.String("Stored")}),
	})
	read := func(paths ...string) error {
		t.Helper()
		var selected []query.Path // Nil returns every stored field.
		for _, path := range paths {
			parsed, err := query.ParsePath(path)
			if err != nil {
				t.Fatal(err)
			}
			selected = append(selected, parsed)
		}
		_, err := engine.Execute(t.Context(), Request{Operation: operation.Read, Collection: "products", ID: id, Select: selected, System: true})
		return err
	}
	if err := read("summary"); err != nil {
		t.Fatalf("unselected invalid roots were validated: %v", err)
	}
	for _, test := range []struct {
		selected string
		path     string
	}{
		{"title", `"title"`},
		{"seo", `"seo.title"`},
		{"tags", `"tags"`},
	} {
		err := read("summary", test.selected)
		var failure *Error
		if !errors.As(err, &failure) || failure.Code != "invalid_field_output" || !strings.Contains(failure.Message, test.path) {
			t.Fatalf("selecting %s returned %v, want invalid output at %s", test.selected, err, test.path)
		}
	}
	if err := read(); err == nil {
		t.Fatal("an unprojected read skipped read-output validation")
	}
}

// A field can become required after documents were stored without it. The
// migration audit refuses that change while values are missing, but a stored
// null is never a reason for a read to fail; only a read hook that returns
// null for a required field breaks the output contract.
func TestReadOutputAdmitsStoredRequiredNulls(t *testing.T) {
	title := liveTestField("title", schema.FieldTypeText)
	title.Required = true
	nestedTitle := liveTestField("seo.title", schema.FieldTypeText)
	nestedTitle.Required = true
	seo := liveTestField("seo", schema.FieldTypeGroup)
	seo.Nested = &schema.NestedField{Fields: []schema.Field{nestedTitle}}
	caption := liveTestField("items.caption", schema.FieldTypeText)
	caption.Required = true
	items := liveTestField("items", schema.FieldTypeArray)
	items.Nested = &schema.NestedField{Fields: []schema.Field{caption}}
	stored := store.Values{
		"title": store.Null(), "seo": store.Object(store.Values{"title": store.Null()}),
		"items": store.List(
			store.Object(store.Values{"_key": store.String("one"), "caption": store.Null()}),
			store.Object(store.Values{"_key": store.String("two"), "caption": store.String("Kept")}),
		),
	}
	for _, test := range []struct {
		name string
		hook Hook
		path string
	}{
		{name: "without read hooks"},
		{name: "with a hook that keeps stored nulls", hook: func(ctx Context) error {
			ctx.Data["title"] = store.Null()
			return nil
		}},
		{name: "with a hook that nulls a stored value", path: `"items.1.caption"`, hook: func(ctx Context) error {
			ctx.Data["items"] = store.List(
				store.Object(store.Values{"_key": store.String("one"), "caption": store.Null()}),
				store.Object(store.Values{"_key": store.String("two"), "caption": store.Null()}),
			)
			return nil
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			collection := liveTestCollection(title, seo, items)
			if test.hook != nil {
				collection.Hooks.AfterRead = []Hook{test.hook}
			}
			engine, id := liveTestEngine(t, collection, stored)
			result, err := engine.Execute(t.Context(), Request{Operation: operation.Read, Collection: "products", ID: id, System: true})
			if test.path != "" {
				var failure *Error
				if !errors.As(err, &failure) || failure.Code != "invalid_field_output" || !strings.Contains(failure.Message, test.path) {
					t.Fatalf("hook null returned %v, want invalid output at %s", err, test.path)
				}
				return
			}
			if err != nil || result.Document == nil || result.Document.Values["title"].Kind() != store.ValueNull {
				t.Fatalf("stored required nulls failed a read: %#v, %v", result.Document, err)
			}
			list, err := engine.Execute(t.Context(), Request{Operation: operation.Read, Collection: "products", Limit: 10, System: true})
			if err != nil || list.Page == nil || len(list.Page.Documents) != 1 {
				t.Fatalf("stored required nulls failed a list: %#v, %v", list.Page, err)
			}
		})
	}
}

func TestCollectionPlanOrdersChecksAndBindingRoots(t *testing.T) {
	first := liveTestField("first", schema.FieldTypeText)
	first.Required = true
	nested := liveTestField("group.inner", schema.FieldTypeNumberList)
	group := liveTestField("group", schema.FieldTypeGroup)
	group.Nested = &schema.NestedField{Fields: []schema.Field{nested}}
	last := liveTestField("last", schema.FieldTypeText)
	last.Required = true
	collection := liveTestCollection(first, group, last)
	collection.Bindings = []FieldBinding{{Field: nested}, {Field: last}, {Field: liveTestField("missing", schema.FieldTypeText)}}
	plan := newCollectionPlan(collection, nil)
	var paths []string
	var describe func(*outputPlan, string)
	describe = func(plan *outputPlan, prefix string) {
		for _, entry := range plan.entries {
			if entry.check {
				paths = append(paths, joinFieldPath(prefix, entry.field.Name))
			}
			if entry.children != nil {
				describe(entry.children, joinFieldPath(prefix, entry.field.Name))
			}
		}
	}
	describe(plan.readOutput, "")
	if got := strings.Join(paths, ","); got != "first,group.inner,last" {
		t.Fatalf("read-output checks=%s", got)
	}
	if got := fmt.Sprint(plan.bindingRoots); got != "[[group] [last] []]" {
		t.Fatalf("binding roots=%s", got)
	}
	if plan.bindingRootFields[0] || !plan.bindingRootFields[1] || plan.bindingRootFields[2] {
		t.Fatalf("binding root fields=%v", plan.bindingRootFields)
	}
}

// A plan indexes the bindings it was derived from. A copy with replaced fields
// or bindings must never see that plan, however the copy was made.
func TestCollectionPlanNeverDescribesAnotherCollection(t *testing.T) {
	sku := liveTestField("sku", schema.FieldTypeText)
	owner := liveTestField("body", schema.FieldTypeGroup)
	parent := liveTestCollection(owner)
	parent.Bindings = []FieldBinding{{Field: owner}, {Block: "card", Field: sku}}
	parent.plan = newCollectionPlan(parent, nil)
	if parent.runtimePlan() != parent.plan {
		t.Fatal("the collection did not reuse its own plan")
	}
	// A copy that replaced its fields and bindings derives its own plan. A
	// collection narrowed to a block definition's fields, placed beneath its
	// canonical placement, locates that definition's bindings, never the
	// resource's own.
	copied := parent
	copied.Schema.Fields, copied.Bindings = []schema.Field{sku}, []FieldBinding{{Field: sku}}
	narrowed := parent.narrowed([]schema.Field{sku}, fieldPlacement{canonical: []string{"body", "widgets", "widget", "card"}, shared: true})
	for name, test := range map[string]struct {
		collection Collection
		binding    int
	}{"copied": {copied, 0}, "narrowed": {narrowed, 1}} {
		plan := test.collection.runtimePlan()
		if plan == parent.plan || fmt.Sprint(plan.bindingRoots[test.binding]) != "[sku]" {
			t.Fatalf("%s plan roots=%v", name, plan.bindingRoots)
		}
	}
	if roots := narrowed.runtimePlan().bindingRoots[0]; len(roots) != 0 {
		t.Fatalf("a narrowed collection located a resource binding beneath %v", roots)
	}
	if narrowed.runtimePlan() != narrowed.plan {
		t.Fatal("a narrowed collection did not reuse the plan derived for it")
	}
}

// Read-output plans are keyed by field list, and every placement of a
// registered block, in any collection, shares its definition's list. The
// planner therefore derives one plan per definition, however the graph nests.
func TestOutputPlansFollowDefinitions(t *testing.T) {
	manifest, err := config.Resolve(config.Input{
		Name: "Shared plans",
		Blocks: []field.Block{
			{Slug: "leaf", Fields: field.Fields{field.Text("heading").Required()}},
			{Slug: "card", Fields: field.Fields{field.Text("title").Required(), field.Blocks("children").References("leaf")}},
			{Slug: "section", Fields: field.Fields{field.Blocks("cards").References("card", "leaf")}},
		},
		Collections: []config.Collection{
			{Slug: "pages", Fields: field.Fields{field.Blocks("layout").References("section", "card"), field.Blocks("sidebar").References("card")}},
			{Slug: "posts", Fields: field.Fields{field.Blocks("layout").References("card")}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := manifest.Snapshot()
	planner := newOutputPlanner()
	pages := newCollectionPlan(Collection{Schema: snapshot.Collections[0]}, planner).readOutput
	posts := newCollectionPlan(Collection{Schema: snapshot.Collections[1]}, planner).readOutput
	variant := func(plan *outputPlan, entry int, slug string) *outputPlan {
		t.Helper()
		if plan == nil || entry >= len(plan.entries) || plan.entries[entry].variants[slug] == nil {
			t.Fatalf("missing %s plan", slug)
		}
		return plan.entries[entry].variants[slug]
	}
	card := variant(pages, 0, "card")
	section := variant(pages, 0, "section")
	for name, plan := range map[string]*outputPlan{"sidebar": variant(pages, 1, "card"), "section": variant(section, 0, "card"), "posts": variant(posts, 0, "card")} {
		if plan != card {
			t.Fatalf("%s placement of card has its own plan", name)
		}
	}
	if variant(card, 1, "leaf") != variant(section, 0, "leaf") {
		t.Fatal("leaf placements have separate plans")
	}
	// Two resource roots and three definitions.
	if len(planner.plans) != 5 {
		t.Fatalf("planner derived %d plans", len(planner.plans))
	}
}
