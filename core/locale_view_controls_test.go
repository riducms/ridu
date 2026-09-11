package core_test

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

var localeViewControlModes = []string{"FallbackENFR", "FallbackFREN", "ExactFrench", "AllLocales", "HookChanges", "RetainHookChanges"}

// BenchmarkLocaleViewControls complements the unchanged create/ordinary matrices.
// Every iteration prepares a fresh persisted EN/FR fixture outside the timer.
// Reads vary projection selection; writes check current earlier-hook changes with
// three indexed reads per callback, never a scan of every title per callback.
func BenchmarkLocaleViewControls(b *testing.B) {
	for _, shape := range []string{"native", "embedded"} {
		for _, mode := range localeViewControlModes {
			for _, size := range []int{10, 100, 1000, 5000} {
				b.Run(fmt.Sprintf("%s/%s/%d", shape, mode, size), func(b *testing.B) {
					b.ReportAllocs()
					b.StopTimer()
					for range b.N {
						fixture := newLocaleViewFixture(b, b.Context(), shape, mode, size)
						b.StartTimer()
						result, err := fixture.run(b.Context())
						b.StopTimer()
						if err != nil {
							b.Fatal(err)
						}
						fixture.check(b, result)
						runtime.KeepAlive(fixture.hooks.retained)
					}
				})
			}
		}
	}
}

// BenchmarkOrdinaryLocaleSerialization adds final result encoding to the exact
// ScalarUpdate workload in BenchmarkOrdinaryValueScaling. Setup remains outside
// the timer, making its extra bytes/work visible after projection reuse.
func BenchmarkOrdinaryLocaleSerialization(b *testing.B) {
	for _, shape := range []string{"native", "embedded"} {
		for _, size := range []int{10, 100, 1000, 5000} {
			b.Run(fmt.Sprintf("%s/%d", shape, size), func(b *testing.B) {
				b.ReportAllocs()
				b.StopTimer()
				for range b.N {
					fixture := newOrdinaryValueFixture(b, b.Context(), shape, "ScalarUpdate", size)
					b.StartTimer()
					result, err := fixture.run(b.Context())
					var encoded []byte
					if err == nil {
						encoded, err = json.Marshal(result.Values)
					}
					b.StopTimer()
					if err != nil {
						b.Fatal(err)
					}
					if len(encoded) == 0 {
						b.Fatal("empty serialized response")
					}
					fixture.checkResult(b, result)
					runtime.KeepAlive(encoded)
				}
			})
		}
	}
}

type localeViewFixture struct {
	ordinaryValueFixture
	mode  string
	hooks *localeViewHooks
}

type localeViewHooks struct {
	active   bool
	retain   bool
	calls    int
	size     int
	retained []operation.View
}

func (hooks *localeViewHooks) change(ctx operation.Context, input operation.Value[string]) (operation.Change[string], error) {
	if !hooks.active {
		return operation.Keep[string](), nil
	}
	index, step := hooks.calls/2, hooks.calls%2
	hooks.calls++
	if index >= hooks.size {
		return operation.Keep[string](), fmt.Errorf("too many title callbacks: %d", hooks.calls)
	}
	want := fmt.Sprintf("node-%d", index)
	if step == 1 {
		want = fmt.Sprintf("first-%d", index)
	}
	value, present := input.Get()
	if sibling, _ := ctx.Siblings.String("title"); !present || value != want || sibling != want {
		return operation.Keep[string](), fmt.Errorf("callback %d input/sibling did not include prior change", hooks.calls)
	}
	rows := ctx.Root.Get("body").Get("outline")
	current, _ := rows.ListItem(index)
	if title, _ := current.Get("content").Get("title").StringValue(); title != want {
		return operation.Keep[string](), fmt.Errorf("callback %d root title = %q, want %q", hooks.calls, title, want)
	}
	if index > 0 {
		previous, _ := rows.ListItem(index - 1)
		if title, _ := previous.Get("content").Get("title").StringValue(); title != fmt.Sprintf("second-%d", index-1) {
			return operation.Keep[string](), fmt.Errorf("callback %d root lost previous row edit", hooks.calls)
		}
	}
	if index+1 < hooks.size {
		next, _ := rows.ListItem(index + 1)
		if title, _ := next.Get("content").Get("title").StringValue(); title != fmt.Sprintf("node-%d", index+1) {
			return operation.Keep[string](), fmt.Errorf("callback %d unexpectedly changed a later row", hooks.calls)
		}
	}
	if hooks.retain {
		hooks.retained = append(hooks.retained, ctx.Root)
	}
	changed := fmt.Sprintf("first-%d", index)
	if step == 1 {
		changed = fmt.Sprintf("second-%d", index)
	}
	return operation.Replace(operation.Present(changed)), nil
}

func newLocaleViewFixture(tb testing.TB, ctx context.Context, shape, mode string, size int) localeViewFixture {
	tb.Helper()
	hooks := &localeViewHooks{size: size, retain: mode == "RetainHookChanges"}
	title := field.Text("title").Required()
	if mode == "HookChanges" || mode == "RetainHookChanges" {
		title = title.Hooks(field.Hooks[string]{BeforeChange: []field.Transform[string]{hooks.change, hooks.change}})
	}
	config := embeddedConfig(title)
	config.Localization.Locales = []ridu.Locale{
		{Code: "en", Label: "English"},
		{Code: "fr", Label: "French", FallbackLocales: []schema.LocaleCode{"en"}},
		{Code: "de", Label: "German", FallbackLocales: []schema.LocaleCode{"en", "fr"}},
	}
	if shape == "native" {
		config.Plugins = nil
		config.Collections[1].Fields = field.Fields{
			field.Group("body", field.Fields{field.Blocks("outline", field.Block{Slug: "card", Fields: field.Fields{field.Group("content", embeddedCard(title).Fields)}})}),
			field.JSON("ordinary"),
		}
	}
	config.Collections[1].Fields = append(config.Collections[1].Fields, field.Text("headline").Required())
	app, err := ridu.New(config, teststore.New())
	if err != nil {
		tb.Fatal(err)
	}
	fixture := localeViewFixture{ordinaryValueFixture: ordinaryValueFixture{app: app, shape: shape, size: size}, mode: mode, hooks: hooks}
	target, err := app.Local().Create(ctx, "targets", store.Values{"name": store.String("A target")}, ridu.MutationOptions{})
	if err != nil {
		tb.Fatal(err)
	}
	fixture.targetID = target.ID
	nodes := make([]store.Value, size)
	for index := range nodes {
		payload := store.Values{"title": store.String(fmt.Sprintf("node-%d", index)), "translation": store.String(fmt.Sprintf("English %d", index))}
		if index == size/2 {
			payload["target"] = store.String(target.ID)
		}
		nodes[index] = fixture.row(fmt.Sprintf("row-%d", index), payload)
	}
	fixture.input = store.Values{"headline": store.String("Original"), "body": fixture.body(nodes)}
	fixture.initial, err = app.Local().Create(ctx, "pages", fixture.input, ridu.MutationOptions{Locale: "en"})
	if err != nil {
		tb.Fatal(err)
	}
	for index := range nodes {
		payload := store.Values{}
		switch index % 4 {
		case 0:
			payload["translation"] = store.String(fmt.Sprintf("French %d", index))
		case 1:
			payload["translation"] = store.String("")
		case 2:
			payload["translation"] = store.Null()
		}
		nodes[index] = fixture.row(fmt.Sprintf("row-%d", index), payload)
	}
	_, err = app.Local().Update(ctx, "pages", fixture.initial.ID, store.Values{"body": fixture.body(nodes)}, ridu.MutationOptions{Locale: "fr"})
	if err != nil {
		tb.Fatal(err)
	}
	segments := []string{"body", "widgets", "widget", "card", "target"}
	if shape == "native" {
		segments = []string{"body", "outline", "card", "content", "target"}
	}
	fixture.population, err = query.NewPath(segments...)
	if err != nil {
		tb.Fatal(err)
	}
	fixture.patch = store.Values{"headline": store.String("Edited")}
	hooks.active = true
	return fixture
}

func (fixture localeViewFixture) run(ctx context.Context) (store.Document, error) {
	if fixture.mode == "HookChanges" || fixture.mode == "RetainHookChanges" {
		return fixture.app.Local().Update(ctx, "pages", fixture.initial.ID, fixture.patch, ridu.MutationOptions{Locale: "en"})
	}
	options := ridu.FindOptions{Locale: "de", Populate: []query.Population{{Path: fixture.population}}}
	switch fixture.mode {
	case "FallbackENFR":
	case "FallbackFREN":
		options.FallbackLocales = []schema.LocaleCode{"fr", "en"}
	case "ExactFrench":
		options.Locale, options.DisableFallback = "fr", true
	case "AllLocales":
		options.Locale, options.AllLocales = "", true
	default:
		return store.Document{}, fmt.Errorf("unknown locale control %q", fixture.mode)
	}
	return fixture.app.Local().Find(ctx, "pages", fixture.initial.ID, options)
}

func (fixture localeViewFixture) check(tb testing.TB, result store.Document) {
	tb.Helper()
	rows := result.Values["body"].Get("outline")
	if result.ID != fixture.initial.ID || rows.Len() != fixture.size {
		tb.Fatal("locale control returned wrong document or size")
	}
	hookMode := fixture.mode == "HookChanges" || fixture.mode == "RetainHookChanges"
	if hookMode && fixture.hooks.calls != fixture.size*2 {
		tb.Fatalf("hook calls = %d, want %d", fixture.hooks.calls, fixture.size*2)
	}
	index := -1
	for row := range rows.Elements() {
		index++
		if key := fixture.key(row); key != fmt.Sprintf("row-%d", index) {
			tb.Fatalf("row %d identity changed: %s", index, key)
		}
		content := row.Get("content")
		wantTitle := fmt.Sprintf("node-%d", index)
		if hookMode {
			wantTitle = fmt.Sprintf("second-%d", index)
		}
		if title, _ := content.Get("title").StringValue(); title != wantTitle {
			tb.Fatalf("row %d title = %q, want %q", index, title, wantTitle)
		}
		translation, present := content.Lookup("translation")
		switch fixture.mode {
		case "AllLocales":
			if text, _ := translation.Get("en").StringValue(); text != fmt.Sprintf("English %d", index) {
				tb.Fatal("all-locales English changed")
			}
			translation, present = translation.Lookup("fr")
			fallthrough
		case "ExactFrench":
			switch index % 4 {
			case 0:
				if text, _ := translation.StringValue(); !present || text != fmt.Sprintf("French %d", index) {
					tb.Fatal("exact French changed")
				}
			case 1:
				if text, ok := translation.StringValue(); !present || !ok || text != "" {
					tb.Fatal("exact empty French changed")
				}
			case 2:
				if fixture.mode == "AllLocales" {
					if !present || translation.Kind() != store.ValueNull {
						tb.Fatal("stored null French changed")
					}
				} else if present {
					tb.Fatal("single-locale projection exposed a null translation")
				}
			case 3:
				if present {
					tb.Fatal("absent French was materialized")
				}
			}
		default:
			want := fmt.Sprintf("English %d", index)
			if fixture.mode == "FallbackFREN" && index%4 == 0 {
				want = fmt.Sprintf("French %d", index)
			}
			if text, _ := translation.StringValue(); !present || text != want {
				tb.Fatalf("row %d translation = %q, want %q", index, text, want)
			}
		}
	}
	if !hookMode {
		middle, _ := rows.ListItem(fixture.size / 2)
		target, populated := middle.Get("content").Get("target").CopyDocument()
		if !populated || target.ID != fixture.targetID {
			tb.Fatal("locale control did not populate its target")
		}
	}
	if fixture.hooks.retain {
		if len(fixture.hooks.retained) != fixture.size*2 {
			tb.Fatal("hook snapshots were not retained")
		}
		for call, view := range fixture.hooks.retained {
			row, _ := view.Get("body").Get("outline").ListItem(call / 2)
			want := fmt.Sprintf("node-%d", call/2)
			if call%2 == 1 {
				want = fmt.Sprintf("first-%d", call/2)
			}
			if title, _ := row.Get("content").Get("title").StringValue(); title != want {
				tb.Fatalf("retained callback %d snapshot changed", call)
			}
		}
	}
}

func TestLocaleViewControlFixtures(t *testing.T) {
	for _, shape := range []string{"native", "embedded"} {
		for _, mode := range localeViewControlModes {
			t.Run(shape+"/"+mode, func(t *testing.T) {
				fixture := newLocaleViewFixture(t, t.Context(), shape, mode, 4)
				before, _ := json.Marshal(fixture.initial)
				result, err := fixture.run(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				fixture.check(t, result)
				after, _ := json.Marshal(fixture.initial)
				if string(before) != string(after) {
					t.Fatal("operation mutated retained initial snapshot")
				}
			})
		}
	}
}
