package core_test

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/store"
	outline "github.com/riducms/ridu/tests/contracts/embedded_plugin"
)

var ordinaryValueActions = []string{"ScalarUpdate", "NestedTitleUpdate", "LocalizedEdit", "InsertReorder", "PopulatedRead"}

// BenchmarkOrdinaryValueScaling measures real Local API operations on an already
// stored document. There are no field callbacks. Nested edits submit a complete
// identity list with sparse row payloads, as required by list replacement semantics.
// PopulatedRead resolves one middle-row reference, keeping fanout constant as the
// document grows. InsertReorder starts with size-1 rows, moves the last row first
// and inserts one new row, finishing at size rows. This keeps the 5,000-row
// workload inside the existing embedded validation budget on both revisions.
// Every iteration creates a fresh app/store/document outside the timer, so all
// measured operations start from the same state rather than becoming no-op edits.
func BenchmarkOrdinaryValueScaling(b *testing.B) {
	for _, shape := range []string{"native", "embedded"} {
		b.Run(shape, func(b *testing.B) {
			for _, action := range ordinaryValueActions {
				b.Run(action, func(b *testing.B) {
					for _, size := range []int{10, 100, 1000, 5000} {
						b.Run(fmt.Sprint(size), func(b *testing.B) {
							b.ReportAllocs()
							b.StopTimer()
							for range b.N {
								fixture := newOrdinaryValueFixture(b, b.Context(), shape, action, size)
								b.StartTimer()
								result, err := fixture.run(b.Context())
								b.StopTimer()
								if err != nil {
									b.Fatal(err)
								}
								fixture.checkResult(b, result)
							}
						})
					}
				})
			}
		})
	}
}

type ordinaryValueFixture struct {
	app        *ridu.App
	shape      string
	action     string
	size       int
	initial    store.Document
	input      store.Values
	patch      store.Values
	targetID   string
	population query.Path
}

func newOrdinaryValueFixture(tb testing.TB, ctx context.Context, shape, action string, size int) ordinaryValueFixture {
	tb.Helper()
	config := embeddedConfig()
	if shape == "native" {
		config.Plugins = nil
		config.Collections[1].Fields = field.Fields{
			field.Group("body", field.Fields{field.Blocks("outline", field.Block{Slug: "card", Fields: field.Fields{field.Group("content", embeddedCard().Fields)}})}),
			field.JSON("ordinary"),
		}
	}
	config.Collections[1].Fields = append(config.Collections[1].Fields, field.Text("headline").Required())
	app, err := ridu.New(config, teststore.New())
	if err != nil {
		tb.Fatal(err)
	}
	target, err := app.Local().Create(ctx, "targets", store.Values{"name": store.String("A target")}, nil)
	if err != nil {
		tb.Fatal(err)
	}
	fixture := ordinaryValueFixture{app: app, shape: shape, action: action, size: size, targetID: target.ID}
	initialSize := size
	if action == "InsertReorder" {
		initialSize--
	}
	nodes := make([]store.Value, initialSize)
	for i := range nodes {
		payload := store.Values{"title": store.String(fmt.Sprintf("node-%d", i)), "translation": store.String(fmt.Sprintf("English %d", i))}
		if i == size/2 {
			payload["target"] = store.String(target.ID)
		}
		nodes[i] = fixture.row(fmt.Sprintf("row-%d", i), payload)
	}
	fixture.input = store.Values{"headline": store.String("Original"), "body": fixture.body(nodes)}
	fixture.initial, err = app.Local().Create(ctx, "pages", fixture.input, nil, ridu.LocaleOptions{Locale: "en"})
	if err != nil {
		tb.Fatal(err)
	}

	if action == "ScalarUpdate" {
		fixture.patch = store.Values{"headline": store.String("Edited")}
	} else if action != "PopulatedRead" {
		for i := range nodes {
			payload := store.Values{}
			if i == size/2 && action == "NestedTitleUpdate" {
				payload["title"] = store.String("Edited title")
			}
			if i == size/2 && action == "LocalizedEdit" {
				payload["translation"] = store.String("Bonjour")
			}
			nodes[i] = fixture.row(fmt.Sprintf("row-%d", i), payload)
		}
		if action == "InsertReorder" {
			reordered := make([]store.Value, 0, size)
			reordered = append(reordered, nodes[initialSize-1], fixture.row("inserted", store.Values{"title": store.String("Inserted title"), "translation": store.String("Inserted English")}))
			nodes = append(reordered, nodes[:initialSize-1]...)
		}
		fixture.patch = store.Values{"body": fixture.body(nodes)}
	}
	segments := []string{"body", "widgets", "widget", "card", "target"}
	if shape == "native" {
		segments = []string{"body", "outline", "card", "content", "target"}
	}
	fixture.population, err = query.NewPath(segments...)
	if err != nil {
		tb.Fatal(err)
	}
	return fixture
}

func (fixture ordinaryValueFixture) row(key string, payload store.Values) store.Value {
	if fixture.shape == "embedded" {
		return outline.Widget("card", key, payload)
	}
	return store.Object(store.Values{"_key": store.String(key), "blockType": store.String("card"), "content": store.Object(payload)})
}

func (fixture ordinaryValueFixture) body(nodes []store.Value) store.Value {
	if fixture.shape == "embedded" {
		return outline.Value(nodes...)
	}
	return store.Object(store.Values{"outline": store.List(nodes...)})
}

func (fixture ordinaryValueFixture) run(ctx context.Context) (store.Document, error) {
	if fixture.action == "PopulatedRead" {
		return fixture.app.Local().FindWithOptions(ctx, "pages", fixture.initial.ID, ridu.FindOptions{Locale: "en", Populate: []query.Population{{Path: fixture.population}}})
	}
	options := ridu.LocaleOptions{Locale: "en"}
	if fixture.action == "LocalizedEdit" {
		options.Locale = "fr"
	}
	return fixture.app.Local().Update(ctx, "pages", fixture.initial.ID, fixture.patch, nil, options)
}

func (fixture ordinaryValueFixture) checkResult(tb testing.TB, result store.Document) {
	tb.Helper()
	rows := result.Values["body"].Get("outline")
	count := fixture.size
	if result.ID != fixture.initial.ID || rows.Kind() != store.ValueList || rows.Len() != count {
		tb.Fatalf("%s/%s returned the wrong document or row count", fixture.shape, fixture.action)
	}
	middle, _ := rows.ListItem(fixture.size / 2)
	switch fixture.action {
	case "ScalarUpdate":
		if text, _ := result.Values["headline"].StringValue(); text != "Edited" {
			tb.Fatal("scalar edit did not run")
		}
	case "NestedTitleUpdate":
		if text, _ := middle.Get("content").Get("title").StringValue(); text != "Edited title" {
			tb.Fatal("nested edit did not run")
		}
	case "LocalizedEdit":
		if text, _ := middle.Get("content").Get("translation").StringValue(); text != "Bonjour" {
			tb.Fatal("locale edit did not run")
		}
	case "InsertReorder":
		first, _ := rows.ListItem(0)
		inserted, _ := rows.ListItem(1)
		if fixture.key(first) != fmt.Sprintf("row-%d", fixture.size-2) || fixture.key(inserted) != "inserted" {
			tb.Fatal("row insertion or reorder did not run")
		}
	case "PopulatedRead":
		target, populated := middle.Get("content").Get("target").CopyDocument()
		if !populated || target.ID != fixture.targetID {
			tb.Fatal("relationship was not populated")
		}
	default:
		tb.Fatalf("unknown ordinary workload %q", fixture.action)
	}
}

func (fixture ordinaryValueFixture) key(row store.Value) string {
	value := row.Get("_key")
	if fixture.shape == "embedded" {
		value = row.Get("content").Get("uid")
	}
	key, _ := value.StringValue()
	return key
}

// The fixture check exercises persisted data, stable identities, defaults and
// exact-locale preservation independently of the timed workload.
func TestOrdinaryValueScalingFixtures(t *testing.T) {
	for _, shape := range []string{"native", "embedded"} {
		for _, action := range ordinaryValueActions {
			t.Run(shape+"/"+action, func(t *testing.T) {
				fixture := newOrdinaryValueFixture(t, t.Context(), shape, action, 3)
				beforeInput, err := json.Marshal(fixture.input)
				if err != nil {
					t.Fatal(err)
				}
				beforeSnapshot, err := json.Marshal(fixture.initial)
				if err != nil {
					t.Fatal(err)
				}
				result, err := fixture.run(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				fixture.checkResult(t, result)
				stored, err := fixture.app.Local().FindWithOptions(t.Context(), "pages", fixture.initial.ID, ridu.FindOptions{AllLocales: true})
				if err != nil {
					t.Fatal(err)
				}
				rows := stored.Values["body"].Get("outline")
				wantKeys := []string{"row-0", "row-1", "row-2"}
				if action == "InsertReorder" {
					wantKeys = []string{"row-1", "inserted", "row-0"}
				}
				var keys []string
				for row := range rows.Elements() {
					key := fixture.key(row)
					keys = append(keys, key)
					content := row.Get("content")
					index := 0
					if key != "inserted" {
						if _, err := fmt.Sscanf(key, "row-%d", &index); err != nil {
							t.Fatal(err)
						}
					}
					wantTitle, wantEnglish := fmt.Sprintf("node-%d", index), fmt.Sprintf("English %d", index)
					if key == "inserted" {
						wantTitle, wantEnglish = "Inserted title", "Inserted English"
					} else if action == "NestedTitleUpdate" && key == "row-1" {
						wantTitle = "Edited title"
					}
					if title, _ := content.Get("title").StringValue(); title != wantTitle {
						t.Fatalf("row %s title = %q, want %q", key, title, wantTitle)
					}
					if english, _ := content.Get("translation").Get("en").StringValue(); english != wantEnglish {
						t.Fatalf("row %s English = %q, want %q", key, english, wantEnglish)
					}
					if action == "LocalizedEdit" && key == "row-1" {
						if french, _ := content.Get("translation").Get("fr").StringValue(); french != "Bonjour" {
							t.Fatal("exact French edit was not persisted")
						}
					}
					if caption, _ := content.Get("caption").StringValue(); caption != "A caption" {
						t.Fatal("caption default was lost")
					}
					if tone, _ := content.Get("style").Get("tone").StringValue(); tone != "neutral" {
						t.Fatal("nested default was lost")
					}
					if key == "row-1" {
						if id, _ := content.Get("target").StringValue(); id != fixture.targetID {
							t.Fatal("stored relationship ID was changed or populated")
						}
					}
				}
				if !reflect.DeepEqual(keys, wantKeys) {
					t.Fatalf("stored row order = %v, want %v", keys, wantKeys)
				}
				wantHeadline := "Original"
				if action == "ScalarUpdate" {
					wantHeadline = "Edited"
				}
				if headline, _ := stored.Values["headline"].StringValue(); headline != wantHeadline {
					t.Fatal("unrelated headline changed")
				}
				afterInput, _ := json.Marshal(fixture.input)
				afterSnapshot, _ := json.Marshal(fixture.initial)
				if string(beforeInput) != string(afterInput) || string(beforeSnapshot) != string(afterSnapshot) {
					t.Fatal("operation changed a retained input or snapshot")
				}
			})
		}
	}
}
