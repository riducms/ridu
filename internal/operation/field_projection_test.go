package operation

import (
	"reflect"
	"testing"

	"github.com/riducms/ridu/internal/localization"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestFieldProjectionReuseKeepsCurrentPriorAndDeferredSnapshotsDistinct(t *testing.T) {
	title := schema.Field{Name: "title", Path: mustPopulationPath(t, "title"), Type: schema.FieldTypeJSON, Localized: true}
	other := schema.Field{Name: "other", Path: mustPopulationPath(t, "other"), Type: schema.FieldTypeJSON, Localized: true}
	fields := []schema.Field{title, other, {Name: "marker", Type: schema.FieldTypeText}}
	canonical := store.Document{Values: store.Values{
		"title":  store.Object(store.Values{"en": store.String("old-en"), "fr": store.String("old-fr")}),
		"other":  store.Object(store.Values{"en": store.String("other-en"), "fr": store.String("other-fr")}),
		"marker": store.String("unchanged"),
	}}
	type retainedView struct {
		view  operation.View
		title string
	}
	var retained []retainedView
	var calls []string
	observe := func(ctx Context, expectedTitle string) {
		t.Helper()
		if ctx.AllLocales || ctx.Locale != "en" && ctx.Locale != "fr" {
			t.Fatalf("callback locale=%q all=%v", ctx.Locale, ctx.AllLocales)
		}
		if got, _ := ctx.Root.Get("title").StringValue(); got != expectedTitle {
			t.Fatalf("%s/%s root title=%q, want %q", ctx.BoundField().Path.String(), ctx.Locale, got, expectedTitle)
		}
		if got, _ := ctx.Root.Get("marker").StringValue(); got != "unchanged" {
			t.Fatalf("callback root marker=%q", got)
		}
		if got, _ := ctx.Prior.Get("title").StringValue(); got != "old-"+string(ctx.Locale) {
			t.Fatalf("prior title=%q for locale %s", got, ctx.Locale)
		}
		// Only the replaced own-field value is published; immutable root and
		// prior views cannot modify another callback or the cached projection.
		retained = append(retained, retainedView{view: operation.ObjectView(ctx.Root), title: expectedTitle})
		calls = append(calls, ctx.BoundField().Path.String()+"/"+string(ctx.Locale))
	}
	collection := Collection{Schema: schema.Collection{Fields: fields}, Bindings: []FieldBinding{
		{ID: "title", Field: title, Hooks: Hooks{
			BeforeChange: []Hook{
				func(ctx Context) error {
					observe(ctx, "old-"+string(ctx.Locale))
					ctx.ReplaceValue(store.String("new-" + string(ctx.Locale)))
					return nil
				},
				func(ctx Context) error {
					observe(ctx, "new-"+string(ctx.Locale))
					return nil
				},
			},
			AfterCommit: []Hook{func(ctx Context) error {
				if ctx.projections != nil {
					t.Fatal("deferred callback retained the working projector")
				}
				observe(ctx, "new-"+string(ctx.Locale))
				return nil
			}},
		}},
		{ID: "other", Field: other, Hooks: Hooks{BeforeChange: []Hook{func(ctx Context) error {
			observe(ctx, "new-"+string(ctx.Locale))
			return nil
		}}}},
	}}
	ctx := Context{
		Context: t.Context(), Operation: operation.Update, Collection: collection.Schema,
		Data: store.CloneValues(canonical.Values), originalCanonical: &canonical,
		AllLocales: true, Locales: []schema.LocaleCode{"en", "fr"}, projections: localization.NewProjector(fields),
	}
	if err := runFieldHooks(collection, ctx, func(hooks Hooks) []Hook { return hooks.BeforeChange }); err != nil {
		t.Fatal(err)
	}
	if want := []string{"title/en", "title/en", "title/fr", "title/fr", "other/en", "other/fr"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("hook order=%v, want %v", calls, want)
	}
	state := &transactionState{}
	queueBoundAfterCommit(state, collection, ctx)
	if len(state.afterCommit) != 2 || ctx.projections == nil {
		t.Fatalf("deferred hooks=%d, caller projector=%v", len(state.afterCommit), ctx.projections)
	}
	ctx.projections.Clear()
	ctx.Data["title"] = store.Object(store.Values{"en": store.String("later"), "fr": store.String("later")})
	for _, deferred := range state.afterCommit {
		if err := deferred.hook(deferred.context); err != nil {
			t.Fatal(err)
		}
	}
	for _, snapshot := range retained {
		if got, _ := snapshot.view.Get("title").StringValue(); got != snapshot.title {
			t.Fatalf("retained title=%q, want %q", got, snapshot.title)
		}
		if got, _ := snapshot.view.Get("marker").StringValue(); got != "unchanged" {
			t.Fatalf("retained marker=%q", got)
		}
	}
}

func TestPopulatedFieldAccessUsesTargetProjectionSchema(t *testing.T) {
	localized := schema.Field{Name: "other", Path: mustPopulationPath(t, "other"), Type: schema.FieldTypeJSON, Localized: true}
	target := Collection{Schema: schema.Collection{
		ID: "targets", Slug: "targets", Fields: []schema.Field{
			{Name: "title", Path: mustPopulationPath(t, "title"), Type: schema.FieldTypeText}, localized,
		},
	}}
	var locales []schema.LocaleCode
	target.Bindings = []FieldBinding{{ID: "other", Field: localized, Access: FieldRules{Read: func(ctx Context) (bool, error) {
		if title, _ := ctx.Root.Get("title").StringValue(); title != "target title" {
			t.Fatalf("target field policy root title=%q; source schema leaked into target projection", title)
		}
		locales = append(locales, ctx.Locale)
		return true, nil
	}}}}
	source := Collection{Schema: schema.Collection{
		ID: "sources", Slug: "sources", Fields: []schema.Field{
			{Name: "title", Path: mustPopulationPath(t, "title"), Type: schema.FieldTypeText, Localized: true},
			{Name: "related", Path: mustPopulationPath(t, "related"), Type: schema.FieldTypeRelationship,
				Relationship: &schema.RelationshipField{CollectionID: target.Schema.ID, CollectionSlug: target.Schema.Slug}},
		},
	}}
	document := store.Document{ID: "source", Values: store.Values{
		"title": store.Object(store.Values{"en": store.String("source title")}),
		"related": store.Populated(store.Document{ID: "target", Values: store.Values{
			"title": store.String("target title"),
			"other": store.Object(store.Values{"en": store.String("english"), "fr": store.String("french")}),
		}}),
	}}
	engine := &Engine{collections: map[string]Collection{string(target.Schema.ID): target}}
	ctx := Context{
		Context: t.Context(), Operation: operation.Read, Collection: source.Schema,
		AllLocales: true, Locales: []schema.LocaleCode{"en", "fr"}, projections: localization.NewProjector(source.Schema.Fields),
	}
	if err := engine.redactResult(source, ctx, &Result{Document: &document}); err != nil {
		t.Fatal(err)
	}
	if want := []schema.LocaleCode{"en", "fr"}; !reflect.DeepEqual(locales, want) {
		t.Fatalf("target field policy locales=%v, want %v", locales, want)
	}
}
