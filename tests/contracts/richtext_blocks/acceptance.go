package richtextblocks

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// Run exercises identical assertions against a real adapter, including its
// transaction, reference index, aggregate persistence and version storage.
func Run(t *testing.T, factory Factory) {
	t.Helper()
	t.Run("ordinary-semantics-and-rollback", func(t *testing.T) { ordinarySemantics(t, factory) })
	t.Run("locales-identities-and-reference-actions", func(t *testing.T) { localesAndReferences(t, factory) })
	t.Run("parent-lifecycle-and-release-layout", func(t *testing.T) { parentLifecycle(t, factory) })
	t.Run("retired-schema-recovery", func(t *testing.T) { retiredSchema(t, factory) })
}

func ordinarySemantics(t *testing.T, factory Factory) {
	seen := &observations{}
	_, app := factory(t, configuration(t, seen))
	ctx := t.Context()
	for _, fixture := range []struct {
		name string
		node store.Value
		path string
	}{
		{"required", Block("callout", "", store.Values{}), "body.root.children.0.fields.title"},
		{"unknown-variant", Block("retired", "", store.Values{"title": store.String("Old")}), "body.root.children.0.fields.blockType"},
		{"unknown-field", Block("callout", "", store.Values{"title": store.String("Title"), "undeclared": store.String("no")}), "body.root.children.0.fields.undeclared"},
		{"array-required", Block("callout", "", store.Values{"title": store.String("Title"), "links": store.List(store.Object(store.Values{"_key": store.String("link")}))}), "body.root.children.0.fields.links.0.label"},
		{"custom-validation", Block("callout", "", store.Values{"title": store.String("invalid")}), "body.root.children.0.fields.title"},
		{"nested-validation", Block("callout", "", store.Values{"title": store.String("Title"), "detail": Document(Block("cta", "", store.Values{}))}), "body.root.children.0.fields.detail.root.children.0.fields.label"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			_, err := app.Local().Create(ctx, "articles", store.Values{"title": store.String("Article"), "body": Document(fixture.node)}, nil)
			issue(t, err, fixture.path)
		})
	}
	_, err := app.Local().Create(ctx, "articles", store.Values{"title": store.String("Article"), "body": Document(Block("callout", "same", store.Values{"title": store.String("First")}), Block("callout", "same", store.Values{"title": store.String("Second")}))}, nil)
	issue(t, err, "body.root.children.1.fields._key")
	seen.occurrences = nil
	created, err := app.Local().Create(ctx, "articles", store.Values{"title": store.String("Article"), "body": Document(
		paragraph("Before"),
		Block("callout", "", store.Values{"title": store.String("First"), "secret": store.String("never disclose"), "links": store.List(store.Object(store.Values{"label": store.String("Read"), "href": store.String("/read")})), "detail": Document(Block("cta", "shared-inner", store.Values{"label": store.String("Inner one")})), "aside": Document(Block("cta", "shared-inner", store.Values{"label": store.String("Inner two")}))}),
		Block("callout", "", store.Values{"title": store.String("Second")}), paragraph("After"),
	)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(seen.occurrences) != 2 || seen.occurrences[0] == "" || seen.occurrences[1] == "" || seen.occurrences[0] == seen.occurrences[1] {
		t.Fatalf("hook dispatch: %v", seen.occurrences)
	}
	first, second := payload(t, created.Values["body"], 1), payload(t, created.Values["body"], 2)
	if stringValue(first["caption"]) != "A helpful note" {
		t.Fatal("missing child default")
	}
	appearance, _ := first["appearance"].CopyObject()
	if stringValue(appearance["tone"]) != "neutral" {
		t.Fatal("missing group default")
	}
	if _, present := first["secret"]; present {
		t.Fatal("read access leaked secret")
	}
	keys := []string{stringValue(first["_key"]), stringValue(second["_key"])}
	if keys[0] == "" || keys[1] == "" || keys[0] == keys[1] {
		t.Fatal("missing occurrence identities")
	}
	links, _ := first["links"].CopyList()
	link, _ := links[0].CopyObject()
	if stringValue(link["_key"]) == "" {
		t.Fatal("ordinary array default identity missing")
	}
	seen.occurrences, seen.original = nil, map[string]string{}
	updated, err := app.Local().UpdateRevision(ctx, "articles", created.ID, store.Values{"body": Document(Block("callout", keys[1], store.Values{"title": store.String("Second edited")}), Block("callout", keys[0], store.Values{"title": store.String("First edited")}))}, created.Revision, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(seen.occurrences) != 2 || seen.original[keys[0]] != "First" || seen.original[keys[1]] != "Second" {
		t.Fatalf("previous identity correlation: %v / %v", seen.occurrences, seen.original)
	}
	if stringValue(payload(t, updated.Values["body"], 1)["_key"]) != keys[0] {
		t.Fatal("reordered identity changed")
	}
	if stringValue(payload(t, payload(t, updated.Values["body"], 1)["aside"], 0)["label"]) != "Inner two" {
		t.Fatal("scoped patch lost inner editor")
	}
	_, err = app.Local().UpdateRevision(ctx, "articles", created.ID, store.Values{"title": store.String("Stale")}, created.Revision, nil)
	code(t, err, "conflict")
	seen.rollback = true
	_, err = app.Local().UpdateRevision(ctx, "articles", created.ID, store.Values{"body": Document(Block("callout", keys[0], store.Values{"title": store.String("Failed update")}))}, updated.Revision, nil)
	if err == nil {
		t.Fatal("hook failure accepted")
	}
	draft := true
	after, err := app.Local().FindWithOptions(ctx, "articles", created.ID, ridu.FindOptions{Draft: &draft})
	if err != nil {
		t.Fatal(err)
	}
	if after.Revision != updated.Revision || stringValue(payload(t, after.Values["body"], 0)["title"]) != "Second edited" {
		t.Fatal("failed transaction changed aggregate")
	}
	audit, err := app.Local().List(ctx, "audit-events", ridu.ListOptions{})
	if err != nil || audit.Total != 0 {
		t.Fatalf("nested side effect escaped rollback: %v", err)
	}
	versions, err := app.Local().Versions(ctx, "articles", created.ID, nil)
	if err != nil || len(versions) != 2 {
		t.Fatalf("rollback changed versions: %d %v", len(versions), err)
	}
}

func localesAndReferences(t *testing.T, factory Factory) {
	seen := &observations{}
	config := configuration(t, seen)
	backend, app := factory(t, config)
	ctx := t.Context()
	asset, err := app.Local().Create(ctx, "assets", store.Values{"title": store.String("Asset"), "url": store.String("/asset")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	file, err := app.Upload(ctx, "files", ridu.UploadInput{Filename: "block.txt", Reader: strings.NewReader("An actual uploaded file")})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"asset", "target"} {
		_, err := app.Local().Create(ctx, "articles", store.Values{"title": store.String("Invalid reference"), "body": Document(Block("callout", "", store.Values{"title": store.String("Title"), name: store.String("missing")}))}, nil)
		issue(t, err, "body.root.children.0.fields."+name)
	}
	created, err := app.Local().Create(ctx, "articles", store.Values{"title": store.String("Localized"), "body": Document(
		Block("callout", "one", store.Values{"title": store.String("First"), "translation": store.String("Hello"), "target": store.String(asset.ID), "locked": store.String(asset.ID), "asset": store.String(file.ID), "lockedAsset": store.String(file.ID)}),
		Block("callout", "two", store.Values{"title": store.String("Second"), "translation": store.String("Goodbye"), "target": store.String(asset.ID)}),
	), "localizedBody": Document(Block("callout", "independent", store.Values{"title": store.String("English document")}))}, nil, ridu.LocaleOptions{Locale: "en"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = app.Local().Update(ctx, "articles", created.ID, store.Values{"body": Document(Block("callout", "two", store.Values{"translation": store.String("Au revoir")}), Block("callout", "one", store.Values{"translation": store.String("Bonjour")})), "localizedBody": Document(Block("cta", "independent", store.Values{"label": store.String("French document")}))}, nil, ridu.LocaleOptions{Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	draft := true
	all, err := app.Local().FindWithOptions(ctx, "articles", created.ID, ridu.FindOptions{Draft: &draft, AllLocales: true})
	if err != nil {
		t.Fatal(err)
	}
	translations, _ := payload(t, all.Values["body"], 1)["translation"].CopyObject()
	if stringValue(translations["en"]) != "Hello" || stringValue(translations["fr"]) != "Bonjour" {
		t.Fatal("positional localized merge corrupted translation")
	}
	whole, _ := all.Values["localizedBody"].CopyObject()
	if stringValue(payload(t, whole["en"], 0)["blockType"]) != "callout" || stringValue(payload(t, whole["fr"], 0)["blockType"]) != "cta" {
		t.Fatal("whole-field locales were correlated together")
	}
	for _, locale := range []schema.LocaleCode{"en", "fr"} {
		found, err := app.Local().FindWithOptions(ctx, "articles", created.ID, ridu.FindOptions{Draft: &draft, Locale: locale})
		if err != nil {
			t.Fatal(err)
		}
		if stringValue(payload(t, found.Values["body"], 0)["_key"]) != "two" {
			t.Fatal("locale read changed shared order")
		}
	}
	paths := []query.Population{}
	for _, name := range []string{"target", "asset"} {
		path, _ := query.ParsePath("body.blocks.block.callout." + name)
		paths = append(paths, query.Population{Path: path, Depth: 1})
	}
	populated, err := app.Local().FindWithOptions(ctx, "articles", created.ID, ridu.FindOptions{Draft: &draft, Populate: paths})
	if err != nil {
		t.Fatal(err)
	}
	for _, index := range []int{0, 1} {
		relation, ok := payload(t, populated.Values["body"], index)["target"].CopyDocument()
		if !ok || relation.ID != asset.ID {
			t.Fatal("repeated relationship population failed")
		}
	}
	upload, ok := payload(t, populated.Values["body"], 1)["asset"].CopyDocument()
	if !ok || upload.ID != file.ID {
		t.Fatal("upload population failed")
	}
	config.Collections = append([]ridu.Collection(nil), config.Collections...)
	for i, collection := range config.Collections {
		if collection.Slug == "assets" {
			var err error
			collection.Fields, err = collection.Fields.Edit(func(draft *field.ChildrenDraft) error {
				return draft.EditText("title", func(title field.TextField) field.TextField {
					return title.Access(field.Access{Read: func(operation.AccessContext) (bool, error) { return false, nil }})
				})
			})
			if err != nil {
				t.Fatal(err)
			}
			config.Collections[i] = collection
		}
	}
	restricted, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	redacted, err := restricted.Local().FindWithOptions(ctx, "articles", created.ID, ridu.FindOptions{Draft: &draft, Populate: paths})
	if err != nil {
		t.Fatal(err)
	}
	for _, index := range []int{0, 1} {
		target, ok := payload(t, redacted.Values["body"], index)["target"].CopyDocument()
		if !ok || target.ID != asset.ID {
			t.Fatal("redaction lost populated identity")
		}
		if _, exists := target.Values["title"]; exists {
			t.Fatal("populated target field access was bypassed")
		}
	}
	// Output documents are not input references. A mistaken populated write is
	// rejected; ordinary scoped edits retain canonical IDs in aggregate storage.
	_, err = app.Local().Update(ctx, "articles", created.ID, store.Values{"body": populated.Values["body"]}, nil)
	issue(t, err, "body.root.children.0.fields.target")
	_, err = app.Local().Update(ctx, "articles", created.ID, store.Values{"body": Document(Block("callout", "two", store.Values{"title": store.String("Edited title")}), Block("callout", "one", store.Values{}))}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var collection schema.Collection
	for _, item := range app.Manifest().Snapshot().Collections {
		if item.Slug == "articles" {
			collection = item
		}
	}
	transaction, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := transaction.Find(ctx, store.Request{Collection: collection, ID: created.ID, Locales: []schema.LocaleCode{"en", "fr"}})
	if rollbackErr := transaction.Rollback(ctx); rollbackErr != nil {
		t.Fatal(rollbackErr)
	}
	if err != nil {
		t.Fatal(err)
	}
	if value := payload(t, raw.Values["body"], 1)["target"]; value.Kind() != store.ValueString || stringValue(value) != asset.ID {
		t.Fatal("populated document persisted instead of ID")
	}
	if _, err = app.Local().Delete(ctx, "assets", asset.ID, nil); err == nil {
		t.Fatal("relationship restriction bypassed")
	}
	if _, err = app.Local().Delete(ctx, "files", file.ID, nil); err == nil {
		t.Fatal("upload restriction bypassed")
	}
	_, err = app.Local().Update(ctx, "articles", created.ID, store.Values{"body": Document(Block("callout", "two", store.Values{}), Block("callout", "one", store.Values{"locked": store.Null(), "lockedAsset": store.Null()}))}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = app.Local().Delete(ctx, "assets", asset.ID, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = app.Local().Delete(ctx, "files", file.ID, nil); err != nil {
		t.Fatal(err)
	}
	after, err := app.Local().FindWithOptions(ctx, "articles", created.ID, ridu.FindOptions{Draft: &draft})
	if err != nil {
		t.Fatal(err)
	}
	if payload(t, after.Values["body"], 0)["target"].Kind() != store.ValueNull || payload(t, after.Values["body"], 1)["target"].Kind() != store.ValueNull || payload(t, after.Values["body"], 1)["asset"].Kind() != store.ValueNull {
		t.Fatal("reference index did not nullify every occurrence")
	}
	all, err = app.Local().FindWithOptions(ctx, "articles", created.ID, ridu.FindOptions{Draft: &draft, AllLocales: true})
	if err != nil {
		t.Fatal(err)
	}
	translations, _ = payload(t, all.Values["body"], 1)["translation"].CopyObject()
	if stringValue(translations["fr"]) != "Bonjour" {
		t.Fatal("nullification destroyed another locale")
	}
	copied, err := app.Local().CopyLocale(ctx, "articles", created.ID, "en", "fr", all.Revision, &store.Document{ID: "editor"})
	if err != nil {
		t.Fatalf("locale copy: %#v", err)
	}
	if stringValue(payload(t, copied.Values["localizedBody"], 0)["_key"]) != "independent" {
		t.Fatal("whole-document locale copy changed identity")
	}
	if stringValue(payload(t, copied.Values["body"], 1)["translation"]) != "Hello" || stringValue(payload(t, copied.Values["body"], 0)["_key"]) != "two" {
		t.Fatal("locale copy lost source translation or shared order")
	}
}

func retiredSchema(t *testing.T, factory Factory) {
	config := configuration(t, &observations{})
	backend, app := factory(t, config)
	ctx := t.Context()
	created, err := app.Local().Create(ctx, "articles", store.Values{"title": store.String("Historical"), "body": Document(Block("cta", "historical", store.Values{"label": store.String("never disclose")}))}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i, collection := range config.Collections {
		if collection.Slug != "articles" {
			continue
		}
		collection.Fields = append(field.Fields(nil), collection.Fields...)
		body, err := field.AsPlugin(collection.Fields[1])
		if err != nil {
			t.Fatal(err)
		}
		trees := field.Snapshot(body).EmbeddedTrees()
		variants := trees[0].Cases[0].Types
		for j, variant := range variants {
			if variant.Slug == "cta" {
				variants[j] = field.Block{Slug: "renamed-cta", Labels: field.BlockLabels{Singular: "CTA"}, Fields: field.Fields{
					field.Text("label"),
					field.Relationship("destination", "pages"),
				}}
			}
		}
		trees[0].Cases[0].Types = variants
		collection.Fields[1] = body.EmbeddedTrees(trees...)
		config.Collections[i] = collection
	}
	current, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	draft := true
	_, readErr := current.Local().FindWithOptions(ctx, "articles", created.ID, ridu.FindOptions{Draft: &draft})
	_, writeErr := current.Local().Update(ctx, "articles", created.ID, store.Values{"title": store.String("Sibling update")}, nil)
	_, replaceErr := current.Local().Update(ctx, "articles", created.ID, store.Values{"body": Document()}, nil)
	for _, err := range []error{readErr, writeErr, replaceErr} {
		code(t, err, "block_recovery_required")
		noPrivateContent(t, err)
	}
	// Reinstalling the declared schema is enough to recover every stored byte.
	recovered, err := app.Local().FindWithOptions(ctx, "articles", created.ID, ridu.FindOptions{Draft: &draft})
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(created.Values)
	b, _ := json.Marshal(recovered.Values)
	if string(a) != string(b) || recovered.Revision != created.Revision {
		t.Fatal("failed recovery operation changed historical data")
	}
}
