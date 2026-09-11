package core_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	localstorage "github.com/riducms/ridu/adapters/storage/local"
	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/store"
	outline "github.com/riducms/ridu/tests/contracts/embedded_plugin"
)

func embeddedCard(overrides ...field.Node) field.Block {
	fields := field.Fields{field.Text("title").Required(), field.Text("caption").Default("A caption"), field.Text("translation").Localized(), field.Text("secret"), field.Group("style", field.Fields{field.Text("tone").Default("neutral")}), field.Array("links", field.Fields{field.Text("label").Required()}), field.Relationship("target", "targets"), field.JSON("ordinary")}
	for _, replacement := range overrides {
		for i, original := range fields {
			if original.Name() == replacement.Name() {
				fields[i] = replacement
			}
		}
	}
	return field.Block{Slug: "card", Fields: fields}
}
func embeddedConfig(overrides ...field.Node) ridu.Config {
	return ridu.Config{Name: "Embedded lifecycle", Plugins: []ridu.Plugin{outline.Plugin{}}, Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}}}, Collections: []ridu.Collection{
		{Slug: "targets", Fields: field.Fields{field.Text("name").Required()}},
		{Slug: "pages", Fields: field.Fields{outline.Field("body", embeddedCard(overrides...)), field.JSON("ordinary")}},
	}}
}
func embeddedPayload(t *testing.T, value store.Value, index int) store.Values {
	t.Helper()
	object, ok := value.CopyObject()
	if !ok {
		t.Fatalf("bad envelope: %#v", value)
	}
	nodes, _ := object["outline"].CopyList()
	if index >= len(nodes) {
		t.Fatal("missing node")
	}
	node, _ := nodes[index].CopyObject()
	payload, _ := node["content"].CopyObject()
	return payload
}
func embeddedString(values store.Values, key string) string {
	value, _ := values[key].StringValue()
	return value
}

func TestEmbeddedFieldsDefaultsHooksAccessAndIdentity(t *testing.T) {
	var identities []operation.OccurrenceID
	title := field.Text("title").Required().Hooks(field.Hooks[string]{BeforeChange: []field.Transform[string]{func(ctx operation.Context, input operation.Value[string]) (operation.Change[string], error) {
		identities = append(identities, ctx.OccurrenceID)
		value, _ := input.Get()
		return operation.Replace(operation.Present(strings.ToUpper(value))), nil
	}}})
	config := embeddedConfig(title, field.Text("secret").Access(field.Access{Read: func(operation.Context) (bool, error) { return false, nil }}))
	app, err := ridu.New(config, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	bait := outline.Value(outline.Widget("not-configured", "", store.Values{"arbitrary": store.String("untouched")}))
	created, err := app.Local().Create(t.Context(), "pages", store.Values{"body": outline.Value(outline.Widget("card", "", store.Values{"title": store.String("one"), "secret": store.String("private"), "ordinary": bait}), outline.Widget("card", "", store.Values{"title": store.String("two")})), "ordinary": bait}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(identities) != 2 || identities[0] == "" || identities[0] == identities[1] {
		t.Fatalf("hook occurrences: %#v", identities)
	}
	first, second := embeddedPayload(t, created.Values["body"], 0), embeddedPayload(t, created.Values["body"], 1)
	if embeddedString(first, "title") != "ONE" || embeddedString(first, "caption") != "A caption" {
		t.Fatalf("defaults or hook writeback: %#v", first)
	}
	if _, exists := first["secret"]; exists {
		t.Fatal("secret leaked")
	}
	style, _ := first["style"].CopyObject()
	if embeddedString(style, "tone") != "neutral" {
		t.Fatal("nested default missing")
	}
	key1, key2 := embeddedString(first, "uid"), embeddedString(second, "uid")
	if key1 == "" || key2 == "" || key1 == key2 {
		t.Fatal("identities missing or reused")
	}
	identities = nil
	updated, err := app.Local().Update(t.Context(), "pages", created.ID, store.Values{"body": outline.Value(outline.Widget("card", key2, store.Values{"title": store.String("edited")}), outline.Widget("card", key1, store.Values{"title": store.String("one")}))}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if embeddedString(embeddedPayload(t, updated.Values["body"], 0), "uid") != key2 {
		t.Fatal("reorder identity changed")
	}
	if len(identities) != 2 {
		t.Fatalf("reorder hook count %d", len(identities))
	}
	if _, exists := created.Values["ordinary"]; !exists {
		t.Fatal("ordinary JSON removed")
	}
}

func TestEmbeddedFieldsPreciseValidationAndRollback(t *testing.T) {
	config := embeddedConfig()
	fail := false
	config.Collections[1].Hooks = ridu.CollectionHooks{BeforeChange: []ridu.Hook{func(ctx ridu.HookContext) error {
		if fail {
			if _, err := ctx.Local.Create(ctx.Context, "targets", store.Values{"name": store.String("rolled back")}, ridu.MutationOptions{}); err != nil {
				return err
			}
			return errors.New("rollback resource transaction")
		}
		return nil
	}}}
	app, err := ridu.New(config, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name  string
		value store.Value
		path  string
	}{
		{"required", outline.Value(outline.Widget("card", "", store.Values{})), "body.outline.0.content.title"},
		{"unknown", outline.Value(outline.Widget("unknown", "", store.Values{})), "body.outline.0.content.schema"},
		{"duplicate", outline.Value(outline.Widget("card", "same", store.Values{"title": store.String("one")}), outline.Widget("card", "same", store.Values{"title": store.String("two")})), "body.outline.1.content.uid"},
		{"nested", outline.Value(outline.Widget("card", "", store.Values{"title": store.String("one"), "links": store.List(store.Object(store.Values{"_key": store.String("a")}))})), "body.outline.0.content.links.0.label"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := app.Local().Create(t.Context(), "pages", store.Values{"body": test.value}, ridu.MutationOptions{})
			var failure *ridu.OperationError
			if !errors.As(err, &failure) {
				t.Fatalf("expected validation: %v", err)
			}
			found := false
			for _, issue := range failure.Issues {
				found = found || issue.Path == test.path
			}
			if !found {
				t.Fatalf("wanted %s: %#v", test.path, failure.Issues)
			}
		})
	}
	fail = true
	_, err = app.Local().Create(t.Context(), "pages", store.Values{"body": outline.Value(outline.Widget("card", "", store.Values{"title": store.String("one")}))}, ridu.MutationOptions{})
	if err == nil {
		t.Fatal("hook should fail")
	}
	targets, err := app.Local().List(t.Context(), "targets", ridu.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(targets.Documents) != 0 {
		t.Fatal("nested write escaped rollback")
	}
}

func TestEmbeddedLocalizationReferencesPopulationAndDelete(t *testing.T) {
	config := embeddedConfig()
	config.Collections[1].Versions = true
	config.Collections[1].VersionConfig = ridu.VersionConfig{Drafts: true}
	app, err := ridu.New(config, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	target, err := app.Local().Create(t.Context(), "targets", store.Values{"name": store.String("A target")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	first := outline.Widget("card", "first", store.Values{"title": store.String("first"), "translation": store.String("Hello"), "target": store.String(target.ID)})
	second := outline.Widget("card", "second", store.Values{"title": store.String("second"), "translation": store.String("Goodbye")})
	created, err := app.Local().Create(t.Context(), "pages", store.Values{"body": outline.Value(first, second)}, ridu.MutationOptions{Locale: "en"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = app.Local().Update(t.Context(), "pages", created.ID, store.Values{"body": outline.Value(outline.Widget("card", "second", store.Values{"translation": store.String("Au revoir")}), outline.Widget("card", "first", store.Values{"translation": store.String("Bonjour")}))}, ridu.MutationOptions{Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	draft := true
	all, err := app.Local().Find(t.Context(), "pages", created.ID, ridu.FindOptions{AllLocales: true, Draft: &draft})
	if err != nil {
		t.Fatal(err)
	}
	payload := embeddedPayload(t, all.Values["body"], 1)
	translations, _ := payload["translation"].CopyObject()
	if embeddedString(translations, "en") != "Hello" || embeddedString(translations, "fr") != "Bonjour" {
		t.Fatalf("identity locale merge: %#v", translations)
	}
	path, _ := query.NewPath("body", "widgets", "widget", "card", "target")
	populated, err := app.Local().Find(t.Context(), "pages", created.ID, ridu.FindOptions{Populate: []query.Population{{Path: path}}, Draft: &draft})
	if err != nil {
		t.Fatal(err)
	}
	relation, ok := embeddedPayload(t, populated.Values["body"], 1)["target"].CopyDocument()
	if !ok || relation.ID != target.ID {
		t.Fatal("embedded relation not populated")
	}
	if _, err := app.Local().Delete(t.Context(), "targets", target.ID, ridu.MutationOptions{}); err != nil {
		t.Fatal(err)
	}
	after, err := app.Local().Find(t.Context(), "pages", created.ID, ridu.FindOptions{Draft: &draft})
	if err != nil {
		t.Fatal(err)
	}
	if value := embeddedPayload(t, after.Values["body"], 1)["target"]; value.Kind() != store.ValueNull {
		t.Fatalf("delete failed nullification: %v", value.Kind())
	}
	versions, err := app.Local().Versions(t.Context(), "pages", created.ID, ridu.FindOptions{})
	if err != nil || len(versions) == 0 {
		t.Fatalf("embedded versions: %v", err)
	}
}

func TestEmbeddedNestedPluginAndStructuralBudget(t *testing.T) {
	config := embeddedConfig()
	calls := 0
	title := field.Text("title").Required().Hooks(field.Hooks[string]{AfterChange: []field.Observer[string]{func(ctx operation.Context, _ operation.Value[string]) error {
		calls++
		if ctx.OccurrenceID == "" {
			t.Error("missing nested identity")
		}
		return nil
	}}})
	config.Collections[1].Fields = field.Fields{outline.Field("body", field.Block{Slug: "wrapper", Fields: field.Fields{outline.Field("nested", embeddedCard(title))}})}
	app, err := ridu.New(config, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	value := outline.Value(outline.Widget("wrapper", "", store.Values{"nested": outline.Value(outline.Widget("card", "", store.Values{"title": store.String("nested")}))}))
	if _, err = app.Local().Create(t.Context(), "pages", store.Values{"body": value}, ridu.MutationOptions{}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("nested hook calls %d", calls)
	}
	deep := store.Object(store.Values{"kind": store.String("section")})
	for range 70 {
		deep = store.Object(store.Values{"kind": store.String("section"), "items": store.List(deep)})
	}
	_, err = app.Local().Create(t.Context(), "pages", store.Values{"body": outline.Value(deep)}, ridu.MutationOptions{})
	var failure *ridu.OperationError
	if !errors.As(err, &failure) || len(failure.Issues) == 0 || failure.Issues[0].Code != "embedded_limit" {
		t.Fatalf("unbounded local API envelope: %v", err)
	}
}

func TestEmbeddedProtectedOccurrencesCannotBeDeletedOrReplaced(t *testing.T) {
	config := embeddedConfig()
	secret := field.Text("secret").Access(field.Access{Update: func(operation.Context) (bool, error) { return false, nil }})
	config.Collections[1].Fields = field.Fields{outline.Field("body", embeddedCard(secret), field.Block{Slug: "note", Fields: field.Fields{field.Text("text")}})}
	app, err := ridu.New(config, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	created, err := app.Local().Create(t.Context(), "pages", store.Values{"body": outline.Value(outline.Widget("card", "one", store.Values{"title": store.String("first"), "secret": store.String("locked")}), outline.Widget("card", "two", store.Values{"title": store.String("second")}))}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// A reorder alone is not a change to the protected occurrence.
	_, err = app.Local().Update(t.Context(), "pages", created.ID, store.Values{"body": outline.Value(outline.Widget("card", "two", store.Values{"title": store.String("second")}), outline.Widget("card", "one", store.Values{"title": store.String("first")}))}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []store.Value{
		outline.Value(),
		outline.Value(outline.Widget("note", "one", store.Values{"text": store.String("replacement")})),
		outline.Value(outline.Widget("note", "new", store.Values{"text": store.String("replacement")})),
	} {
		_, err = app.Local().Update(t.Context(), "pages", created.ID, store.Values{"body": value}, ridu.MutationOptions{})
		if err == nil {
			t.Fatal("protected occurrence deleted or replaced")
		}
	}
}

func TestEmbeddedRetiredSchemaNeverDisclosesOrDropsPayload(t *testing.T) {
	config := embeddedConfig()
	backend := teststore.New()
	original, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	created, err := original.Local().Create(t.Context(), "pages", store.Values{"body": outline.Value(outline.Widget("card", "one", store.Values{"title": store.String("retired private payload")}))}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	config.Collections[1].Fields = field.Fields{outline.Field("body", field.Block{Slug: "replacement", Fields: field.Fields{field.Text("text")}})}
	current, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	_, readErr := current.Local().Find(t.Context(), "pages", created.ID, ridu.FindOptions{})
	_, writeErr := current.Local().Update(t.Context(), "pages", created.ID, store.Values{"body": outline.Value()}, ridu.MutationOptions{})
	for _, err := range []error{readErr, writeErr} {
		var failure *ridu.OperationError
		if !errors.As(err, &failure) || failure.Code != "block_recovery_required" {
			t.Fatalf("recovery: %v", err)
		}
		if strings.Contains(err.Error(), "private payload") {
			t.Fatal("raw payload disclosed")
		}
	}
	restored, err := original.Local().Find(t.Context(), "pages", created.ID, ridu.FindOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if embeddedString(embeddedPayload(t, restored.Values["body"], 0), "title") != "retired private payload" || restored.Revision != created.Revision {
		t.Fatal("failed update changed stored payload")
	}
}

func TestEmbeddedAfterReadAndAfterCommitEachObserveOccurrences(t *testing.T) {
	reads, commits := 0, 0
	title := field.Text("title").Required().Hooks(field.Hooks[string]{AfterCommit: []field.Observer[string]{func(ctx operation.Context, _ operation.Value[string]) error {
		commits++
		if ctx.OccurrenceID == "" {
			t.Error("missing committed occurrence")
		}
		return nil
	}}}).ReplaceAfterRead(func(_ operation.Context, input operation.Value[string]) (operation.Change[string], error) {
		reads++
		value, _ := input.Get()
		return operation.Replace(operation.Present("read " + value)), nil
	})
	config := embeddedConfig(title)
	app, err := ridu.New(config, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	created, err := app.Local().Create(t.Context(), "pages", store.Values{"body": outline.Value(outline.Widget("card", "one", store.Values{"title": store.String("one")}), outline.Widget("card", "two", store.Values{"title": store.String("two")}))}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if reads != 2 || commits != 2 {
		t.Fatalf("read/commit counts %d/%d", reads, commits)
	}
	if embeddedString(embeddedPayload(t, created.Values["body"], 0), "title") != "read one" {
		t.Fatal("read hook writeback missing")
	}
}

func TestEmbeddedHookBatchTransformsEachOccurrenceOnce(t *testing.T) {
	calls := make(map[operation.OccurrenceID]int)
	title := field.Text("title").Required().Hooks(field.Hooks[string]{BeforeChange: []field.Transform[string]{func(ctx operation.Context, value operation.Value[string]) (operation.Change[string], error) {
		calls[ctx.OccurrenceID]++
		text, _ := value.Get()
		return operation.Replace(operation.Present("edited " + text)), nil
	}}})
	app, err := ridu.New(embeddedConfig(title), teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	nodes := make([]store.Value, 5)
	for i := range nodes {
		nodes[i] = outline.Widget("card", fmt.Sprint(i), store.Values{"title": store.String(fmt.Sprintf("node %d", i))})
	}
	created, err := app.Local().Create(t.Context(), "pages", store.Values{"body": outline.Value(nodes...)}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != len(nodes) {
		t.Fatalf("hook occurrence count = %d, want %d", len(calls), len(nodes))
	}
	for occurrence, count := range calls {
		if occurrence == "" || count != 1 {
			t.Fatalf("hook calls for %q = %d, want one call per identified occurrence", occurrence, count)
		}
	}
	stored, err := app.Local().Find(t.Context(), "pages", created.ID, ridu.FindOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, document := range []store.Document{created, stored} {
		for i := range nodes {
			if got, want := embeddedString(embeddedPayload(t, document.Values["body"], i), "title"), fmt.Sprintf("edited node %d", i); got != want {
				t.Fatalf("occurrence %d title = %q, want %q", i, got, want)
			}
		}
	}
}

func BenchmarkEmbeddedHookBatch(b *testing.B) {
	for _, size := range []int{100, 1000} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			calls := 0
			title := field.Text("title").Required().Hooks(field.Hooks[string]{BeforeChange: []field.Transform[string]{func(operation.Context, operation.Value[string]) (operation.Change[string], error) {
				calls++
				return operation.Replace(operation.Present("edited")), nil
			}}})
			config := embeddedConfig(title)
			nodes := make([]store.Value, size)
			for i := range nodes {
				nodes[i] = outline.Widget("card", fmt.Sprint(i), store.Values{"title": store.String("node")})
			}
			values := store.Values{"body": outline.Value(nodes...)}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				b.StopTimer()
				app, err := ridu.New(config, teststore.New())
				if err != nil {
					b.Fatal(err)
				}
				calls = 0
				b.StartTimer()
				_, err = app.Local().Create(b.Context(), "pages", values, ridu.MutationOptions{})
				if err != nil || calls != size {
					b.Fatalf("nodes=%d hooks=%d error=%v", size, calls, err)
				}
			}
		})
	}
}

func TestEmbeddedUploadAdmissionPopulationAndDeleteActions(t *testing.T) {
	storage, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	config := embeddedConfig()
	config.Storage = storage
	config.StorageNamespace = "embedded-uploads"
	config.Collections = append(config.Collections, ridu.Collection{Slug: "media", Upload: true, UploadConfig: ridu.UploadConfig{MaxFileSize: 1024, MimeTypes: []string{"text/plain"}}})
	config.Collections[1].Fields = field.Fields{outline.Field("body", field.Block{Slug: "card", Fields: field.Fields{field.Upload("asset", "media"), field.Upload("locked", "media").OnDelete(field.ReferenceDeleteRestrict), field.Relationship("target", "targets")}})}
	app, err := ridu.New(config, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	media, err := app.Upload(t.Context(), "media", ridu.UploadInput{Filename: "embedded.txt", Reader: strings.NewReader("actual upload")})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"asset", "target"} {
		_, err = app.Local().Create(t.Context(), "pages", store.Values{"body": outline.Value(outline.Widget("card", "one", store.Values{name: store.String("missing")}))}, ridu.MutationOptions{})
		var failure *ridu.OperationError
		if !errors.As(err, &failure) {
			t.Fatalf("missing %s admitted: %v", name, err)
		}
		found := false
		for _, issue := range failure.Issues {
			found = found || issue.Path == "body.outline.0.content."+name
		}
		if !found {
			t.Fatalf("%s reference issue path: %#v", name, failure.Issues)
		}
	}
	created, err := app.Local().Create(t.Context(), "pages", store.Values{"body": outline.Value(outline.Widget("card", "one", store.Values{"asset": store.String(media.ID), "locked": store.String(media.ID)}))}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	path, _ := query.NewPath("body", "widgets", "widget", "card", "asset")
	populated, err := app.Local().Find(t.Context(), "pages", created.ID, ridu.FindOptions{Populate: []query.Population{{Path: path}}})
	if err != nil {
		t.Fatal(err)
	}
	asset, ok := embeddedPayload(t, populated.Values["body"], 0)["asset"].CopyDocument()
	if !ok || asset.ID != media.ID {
		t.Fatal("upload was not populated")
	}
	if _, err = app.Local().Delete(t.Context(), "media", media.ID, ridu.MutationOptions{}); err == nil {
		t.Fatal("embedded upload restriction bypassed")
	}
	_, err = app.Local().Update(t.Context(), "pages", created.ID, store.Values{"body": outline.Value(outline.Widget("card", "one", store.Values{"locked": store.Null()}))}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = app.Local().Delete(t.Context(), "media", media.ID, ridu.MutationOptions{}); err != nil {
		t.Fatal(err)
	}
	after, err := app.Local().Find(t.Context(), "pages", created.ID, ridu.FindOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if embeddedPayload(t, after.Values["body"], 0)["asset"].Kind() != store.ValueNull {
		t.Fatal("embedded upload not nullified")
	}
}

func TestEmbeddedHooksCorrelateDetachedOriginalValuesAcrossReorderAndLocales(t *testing.T) {
	observed := map[string]string{}
	translations := map[string]string{}
	title := field.Text("title").Required().Hooks(field.Hooks[string]{BeforeChange: []field.Transform[string]{func(ctx operation.Context, _ operation.Value[string]) (operation.Change[string], error) {
		key, _ := ctx.Siblings.String("uid")
		if prior, ok := ctx.Prior.String("title"); ok {
			observed[key] = prior
			if oldKey, _ := ctx.Prior.String("uid"); oldKey != key {
				t.Error("prior identity mismatch")
			}
		} else {
			observed[key] = "<new>"
		}
		return operation.Keep[string](), nil
	}}})
	translation := field.Text("translation").Localized().Hooks(field.Hooks[string]{BeforeChange: []field.Transform[string]{func(ctx operation.Context, _ operation.Value[string]) (operation.Change[string], error) {
		key, _ := ctx.Siblings.String("uid")
		if _, exists := ctx.Prior.Lookup("uid"); !exists {
			translations[key] = "<new>"
		} else if value, ok := ctx.Prior.String("translation"); ok {
			translations[key] = value
		} else {
			translations[key] = "<absent>"
		}
		return operation.Keep[string](), nil
	}}})
	config := embeddedConfig(title, translation)
	app, err := ridu.New(config, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	created, err := app.Local().Create(t.Context(), "pages", store.Values{"body": outline.Value(outline.Widget("card", "alpha", store.Values{"title": store.String("Alpha"), "translation": store.String("Hello")}), outline.Widget("card", "beta", store.Values{"title": store.String("Beta"), "translation": store.String("Bye")}))}, ridu.MutationOptions{Locale: "en"})
	if err != nil {
		t.Fatal(err)
	}
	if observed["alpha"] != "<new>" || observed["beta"] != "<new>" {
		t.Fatal("create acquired old occurrences")
	}
	observed = map[string]string{}
	translations = map[string]string{}
	_, err = app.Local().Update(t.Context(), "pages", created.ID, store.Values{"body": outline.Value(outline.Widget("card", "beta", store.Values{"title": store.String("Beta edited")}), outline.Widget("card", "alpha", store.Values{"title": store.String("Alpha edited")}), outline.Widget("card", "fresh", store.Values{"title": store.String("Fresh")}))}, ridu.MutationOptions{Locale: "en"})
	if err != nil {
		t.Fatal(err)
	}
	if observed["alpha"] != "Alpha" || observed["beta"] != "Beta" || observed["fresh"] != "<new>" {
		t.Fatalf("reordered previous values: %#v", observed)
	}
	if translations["alpha"] != "Hello" || translations["beta"] != "Bye" {
		t.Fatalf("English previous values: %#v", translations)
	}
	translations = map[string]string{}
	_, err = app.Local().Update(t.Context(), "pages", created.ID, store.Values{"body": outline.Value(outline.Widget("card", "alpha", store.Values{"translation": store.String("Bonjour")}), outline.Widget("card", "beta", store.Values{"translation": store.String("Au revoir")}))}, ridu.MutationOptions{Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	if translations["alpha"] != "<absent>" || translations["beta"] != "<absent>" {
		t.Fatalf("fallback became prior French value: %#v", translations)
	}
	translations = map[string]string{}
	_, err = app.Local().Update(t.Context(), "pages", created.ID, store.Values{"body": outline.Value(outline.Widget("card", "beta", store.Values{"translation": store.String("Salut")}), outline.Widget("card", "alpha", store.Values{"translation": store.String("Bonsoir")}))}, ridu.MutationOptions{Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	if translations["alpha"] != "Bonjour" || translations["beta"] != "Au revoir" {
		t.Fatalf("French previous identities: %#v", translations)
	}
}

func TestEmbeddedParentTransformPreservesSurvivingChildScopes(t *testing.T) {
	reorder := false
	var writes, after []string
	prior := map[string]string{}
	identities := map[string]operation.OccurrenceID{}
	title := field.Text("title").Required().Hooks(field.Hooks[string]{BeforeChange: []field.Transform[string]{func(ctx operation.Context, input operation.Value[string]) (operation.Change[string], error) {
		key, _ := ctx.Siblings.String("uid")
		writes = append(writes, key)
		if old, ok := ctx.Prior.String("title"); ok {
			prior[key] = old
		} else {
			prior[key] = "<new>"
		}
		if old, ok := identities[key]; ok && old != ctx.OccurrenceID {
			t.Errorf("retained %s changed identity", key)
		}
		identities[key] = ctx.OccurrenceID
		value, _ := input.Get()
		return operation.Replace(operation.Present(strings.ToUpper(value))), nil
	}}, AfterChange: []field.Observer[string]{func(ctx operation.Context, _ operation.Value[string]) error {
		key, _ := ctx.Siblings.String("uid")
		after = append(after, key)
		return nil
	}}})
	body := outline.Field("body", embeddedCard(title)).Hooks(field.Hooks[store.Value]{BeforeChange: []field.Transform[store.Value]{func(_ operation.Context, input operation.Value[store.Value]) (operation.Change[store.Value], error) {
		if !reorder {
			return operation.Keep[store.Value](), nil
		}
		value, _ := input.Get()
		envelope, _ := value.CopyObject()
		nodes, _ := envelope["outline"].CopyList()
		return operation.Replace(operation.Present(outline.Value(nodes[2], nodes[0], outline.Widget("card", "added", store.Values{"title": store.String("new")})))), nil
	}}})
	config := embeddedConfig()
	config.Collections[1].Fields = field.Fields{body}
	app, err := ridu.New(config, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	created, err := app.Local().Create(t.Context(), "pages", store.Values{"body": outline.Value(outline.Widget("card", "first", store.Values{"title": store.String("one")}), outline.Widget("card", "removed", store.Values{"title": store.String("two")}), outline.Widget("card", "third", store.Values{"title": store.String("three")}))}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	reorder, writes, after, prior = true, nil, nil, map[string]string{}
	updated, err := app.Local().Update(t.Context(), "pages", created.ID, store.Values{}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(writes, ",") != "third,first,added" || strings.Join(after, ",") != "third,first,added" {
		t.Fatalf("child dispatch: writes=%v after=%v", writes, after)
	}
	if prior["third"] != "THREE" || prior["first"] != "ONE" || prior["added"] != "<new>" {
		t.Fatalf("child prior scopes: %#v", prior)
	}
	if embeddedString(embeddedPayload(t, updated.Values["body"], 2), "title") != "NEW" {
		t.Fatal("new child transform was not applied")
	}
}
