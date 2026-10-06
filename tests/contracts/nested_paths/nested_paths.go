// Package nestedpaths verifies that filters through more than one repeated
// container (arrays and blocks) mean the same thing on every store adapter,
// and that sorting through one is rejected before any adapter runs.
package nestedpaths

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"

	"github.com/riducms/ridu"
	localstorage "github.com/riducms/ridu/adapters/storage/local"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/operation"
	fieldoperation "github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/plugins/richtext"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// Factory opens a migrated application on the store under test.
type Factory func(*testing.T, ridu.Config) (store.Store, *ridu.App)

const (
	noteSlug  = "nested-note"
	quoteSlug = "nested-quote"
	cardSlug  = "nested-card"
	panelSlug = "nested-panel"
)

// Paths through two and three repeated levels. The panel block reuses the
// card's "children" name and the quote block reuses the note's "text", so a
// store that ignores a block discriminator at either level matches decoys.
const (
	noteText    = "layout." + cardSlug + ".children." + noteSlug + ".text"
	noteRank    = "layout." + cardSlug + ".children." + noteSlug + ".rank"
	noteCaption = "layout." + cardSlug + ".children." + noteSlug + ".caption"
	noteAuthor  = "layout." + cardSlug + ".children." + noteSlug + ".author"
	noteImage   = "layout." + cardSlug + ".children." + noteSlug + ".image"
	noteTags    = "layout." + cardSlug + ".children." + noteSlug + ".tags"
	noteSizes   = "layout." + cardSlug + ".children." + noteSlug + ".sizes"
	noteFlags   = "layout." + cardSlug + ".children." + noteSlug + ".flags"
	notePinned  = "layout." + cardSlug + ".children." + noteSlug + ".pinned"
	noteLabel   = "layout." + cardSlug + ".children." + noteSlug + ".meta.label"
	noteSecret  = "layout." + cardSlug + ".children." + noteSlug + ".secret"
	noteData    = "layout." + cardSlug + ".children." + noteSlug + ".data"
	noteFans    = "layout." + cardSlug + ".children." + noteSlug + ".fans"
	cardNotes   = "layout." + cardSlug + ".children"
	panelText   = "layout." + panelSlug + ".children." + noteSlug + ".text"
	linkAuthor  = "layout." + cardSlug + ".links.targets.author"
	linkTags    = "layout." + cardSlug + ".links.targets.tags"
	cellValue   = "section.rows.cells.value"
	cellSize    = "section.rows.cells.size"
	cellSizes   = "section.rows.cells.sizes"
	cellTags    = "section.rows.cells.tags"
	itemName    = "translated.items.name"
	entryWord   = "outer.local.entries.word"
	deepText    = "deep.middle.leaves." + noteSlug + ".text"
)

// Run exercises the shared nested-path truth table on one store.
func Run(t *testing.T, factory Factory) {
	t.Helper()
	storage, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, app := factory(t, configuration(storage))
	fixture := seed(t, app)
	handler := app.Handler(ridu.HandlerOptions{})

	t.Run("filters", func(t *testing.T) {
		for _, test := range fixture.cases() {
			t.Run(test.name, func(t *testing.T) {
				page, err := app.Local().List(t.Context(), "nested-pages", ridu.ListOptions{Where: test.where, Locale: test.locale, Limit: 100})
				if err != nil {
					t.Fatalf("list: %s", describe(err))
				}
				got := fixture.titles(page.Documents)
				if strings.Join(got, ",") != strings.Join(test.want, ",") || page.Total == nil || *page.Total != len(test.want) {
					t.Fatalf("list = %v (total %v), want %v", got, page.Total, test.want)
				}
			})
		}
	})

	t.Run("rest-where-list-and-count", func(t *testing.T) {
		for _, test := range []struct {
			where, locale string
			want          int
		}{
			{fmt.Sprintf(`{%q:{"equals":"match"}}`, noteText), "", 1},
			{fmt.Sprintf(`{%q:{"in":["match","else"]}}`, noteText), "", 2},
			{fmt.Sprintf(`{%q:{"exists":false}}`, noteText), "", 3},
			{fmt.Sprintf(`{%q:{"in":["green"]}}`, noteTags), "", 1},
			{fmt.Sprintf(`{%q:{"equals":"world"}}`, noteCaption), "fr", 1},
			{fmt.Sprintf(`{"not":{%q:{"equals":"c2"}}}`, cellValue), "", 4},
			{fmt.Sprintf(`{%q:{"equals":"deepest"}}`, deepText), "", 1},
		} {
			query := "?where=" + url.QueryEscape(test.where)
			if test.locale != "" {
				query += "&locale=" + test.locale
			}
			for _, route := range []string{"", "/count"} {
				response := request(handler, http.MethodGet, "/api/collections/nested-pages"+route+query)
				if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), fmt.Sprintf(`"totalDocs":%d`, test.want)) {
					t.Errorf("GET %s%s = %d %s, want totalDocs %d", route, query, response.Code, response.Body.String(), test.want)
				}
			}
		}
	})

	t.Run("field-read-rules-still-deny", func(t *testing.T) {
		_, err := app.Local().List(t.Context(), "nested-pages", ridu.ListOptions{Where: query.Equal(path(noteSecret), "hidden")})
		var failure *operation.Error
		if !errors.As(err, &failure) || failure.Status != http.StatusForbidden || failure.Code != "field_access_denied" || len(failure.Issues) != 1 || failure.Issues[0].Path != noteSecret {
			t.Fatalf("restricted nested path = %#v, want field_access_denied", err)
		}
	})

	t.Run("sorting-through-repeated-containers-is-rejected", func(t *testing.T) {
		for _, name := range []string{"layout." + cardSlug + ".heading", noteRank, cellSize, "section.rows.label", itemName, "section.rows", "outer"} {
			for _, direction := range []query.Direction{query.Ascending, query.Descending} {
				_, err := app.Local().List(t.Context(), "nested-pages", ridu.ListOptions{Sort: []query.Sort{{Path: path(name), Direction: direction}}})
				unsupported(t, err, name)
			}
			response := request(handler, http.MethodGet, "/api/collections/nested-pages?sort="+url.QueryEscape(name))
			if response.Code != http.StatusBadRequest {
				t.Errorf("REST sort %s = %d %s, want 400", name, response.Code, response.Body.String())
			}
		}
		page, err := app.Local().List(t.Context(), "nested-pages", ridu.ListOptions{Sort: []query.Sort{query.Asc(path("title"))}, Where: query.Equal(path(noteText), "match")})
		if err != nil || len(page.Documents) != 1 {
			t.Fatalf("a direct sort with a nested filter: %#v, %v", page, err)
		}
	})

	t.Run("embedded-plugin-tree-paths-are-not-filters", func(t *testing.T) {
		name := "body.blocks.block." + quoteSlug + ".text"
		_, err := app.Local().List(t.Context(), "nested-pages", ridu.ListOptions{Where: query.Equal(path(name), "match")})
		unsupported(t, err, name)
		_, err = app.Local().List(t.Context(), "nested-pages", ridu.ListOptions{Where: query.Exists(path("body"), true)})
		if err != nil {
			t.Fatalf("the plugin field itself remains filterable: %s", describe(err))
		}
	})

	t.Run("distinct", func(t *testing.T) {
		_, err := app.Local().Distinct(t.Context(), "nested-pages", ridu.DistinctOptions{Field: path(noteText)})
		unsupported(t, err, noteText)
		values, err := app.Local().Distinct(t.Context(), "nested-pages", ridu.DistinctOptions{Field: path("title"), Where: query.In(path(noteText), "match", "else")})
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, value := range values.Values {
			text, _ := value.StringValue()
			got = append(got, text)
		}
		if strings.Join(got, ",") != "alpha,beta" || values.Total != 2 {
			t.Fatalf("distinct titles under a nested filter = %v (%d)", got, values.Total)
		}
	})

	t.Run("access-predicates", func(t *testing.T) {
		gatedAccess(t, factory, storage)
	})
}

// gatedAccess applies nested-path access predicates to reads, every-locale
// reads and version history, where adapters compile them against snapshots.
func gatedAccess(t *testing.T, factory Factory, storage *localstorage.Backend) {
	gateNote := field.Block{Slug: "gate-note", Fields: field.Fields{field.Text("text")}}
	gateCard := field.Block{Slug: "gate-card", Fields: field.Fields{field.Blocks("children", gateNote)}}
	visible := query.Or(
		query.Equal(path("blocks.gate-card.children.gate-note.text"), "public"),
		query.Equal(path("rows.items.label"), "shown"),
	)
	allow := func(ridu.AccessContext) (ridu.AccessDecision, error) { return ridu.Allow(), nil }
	gated := func(ridu.AccessContext) (ridu.AccessDecision, error) { return ridu.Where(visible), nil }
	_, app := factory(t, ridu.Config{
		Name: "Nested access predicates", Storage: storage, StorageNamespace: "nested-access",
		Localization: localization(),
		Collections: []ridu.Collection{{
			Slug: "nested-gated", Versions: true,
			Access: ridu.CollectionAccess{Create: allow, Update: allow, Read: gated, ReadVersions: gated},
			Fields: field.Fields{
				field.Text("title"),
				field.Blocks("blocks", gateCard),
				field.Array("rows", field.Fields{field.Array("items", field.Fields{field.Text("label").Localized()})}),
			},
		}},
	})
	create := func(title, text, label string) string {
		document, err := app.Local().Create(t.Context(), "nested-gated", store.Values{
			"title":  store.String(title),
			"blocks": store.List(block("gate-card", "c-"+title, store.Values{"children": store.List(block("gate-note", "n-"+title, store.Values{"text": store.String(text)}))})),
			"rows":   store.List(row("r-"+title, store.Values{"items": store.List(row("i-"+title, store.Values{"label": store.String(label)}))})),
		}, ridu.MutationOptions{System: true})
		if err != nil {
			t.Fatal(err)
		}
		return document.ID
	}
	public := create("public", "public", "hidden")
	shown := create("shown", "private", "shown")
	create("private", "private", "hidden")
	// Every-locale reads apply the rule to each locale exactly, without
	// fallback, so the label stored only in English does not admit "shown".
	for _, test := range []struct {
		options ridu.ListOptions
		want    string
	}{
		{ridu.ListOptions{}, "public,shown"},
		{ridu.ListOptions{Locale: "fr"}, "public,shown"},
		{ridu.ListOptions{Locale: "fr", DisableFallback: true}, "public"},
		{ridu.ListOptions{AllLocales: true}, "public"},
	} {
		page, err := app.Local().List(t.Context(), "nested-gated", test.options)
		if err != nil {
			t.Fatalf("gated list %+v: %s", test.options, describe(err))
		}
		titles := make([]string, 0, len(page.Documents))
		for _, document := range page.Documents {
			title, _ := document.Values["title"].StringValue()
			titles = append(titles, title)
		}
		sort.Strings(titles)
		if strings.Join(titles, ",") != test.want || *page.Total != len(titles) {
			t.Fatalf("gated list %+v = %v, want %s", test.options, titles, test.want)
		}
	}
	for id, want := range map[string]int{public: 1, shown: 1} {
		versions, err := app.Local().Versions(t.Context(), "nested-gated", id, ridu.FindOptions{})
		if err != nil || len(versions) != want {
			t.Fatalf("gated versions of %s = %d, %v", id, len(versions), err)
		}
	}
	_, err := app.Local().PublishChanges(t.Context(), "nested-gated", public, store.Values{
		"blocks": store.List(block("gate-card", "c-public", store.Values{"children": store.List(block("gate-note", "n-public", store.Values{"text": store.String("withdrawn")}))})),
	}, ridu.MutationOptions{System: true})
	if err != nil {
		t.Fatal(err)
	}
	versions, err := app.Local().Versions(t.Context(), "nested-gated", public, ridu.FindOptions{})
	if err != nil || len(versions) != 1 {
		t.Fatalf("version access must still match the earlier public snapshot only: %d, %v", len(versions), err)
	}
	if _, err := app.Local().Find(t.Context(), "nested-gated", public, ridu.FindOptions{}); !errors.Is(err, store.ErrNotFound) {
		var failure *operation.Error
		if !errors.As(err, &failure) || failure.Status != http.StatusNotFound {
			t.Fatalf("withdrawn document remained readable: %v", err)
		}
	}
}

type testCase struct {
	name   string
	locale schema.LocaleCode
	where  query.Expression
	want   []string
}

type fixture struct {
	ids     map[string]string
	authors map[string]string
	media   string
}

func (f fixture) titles(documents []store.Document) []string {
	byID := make(map[string]string, len(f.ids))
	for title, id := range f.ids {
		byID[id] = title
	}
	result := make([]string, 0, len(documents))
	for _, document := range documents {
		result = append(result, byID[document.ID])
	}
	sort.Strings(result)
	return result
}

func (f fixture) cases() []testCase {
	all := []string{"alpha", "beta", "delta", "epsilon", "gamma"}
	except := func(excluded ...string) []string {
		var result []string
		for _, title := range all {
			found := false
			for _, candidate := range excluded {
				found = found || candidate == title
			}
			if !found {
				result = append(result, title)
			}
		}
		return result
	}
	c := func(name string, where query.Expression, want ...string) testCase {
		return testCase{name: name, where: where, want: want}
	}
	l := func(name string, locale schema.LocaleCode, where query.Expression, want ...string) testCase {
		return testCase{name: name, locale: locale, where: where, want: want}
	}
	return []testCase{
		// Two levels of blocks: card at level one, note at level two.
		c("equals", query.Equal(path(noteText), "match"), "alpha"),
		c("not-equals", query.NotEqual(path(noteText), "match"), except("alpha")...),
		c("not", query.Not(query.Equal(path(noteText), "match")), except("alpha")...),
		c("in", query.In(path(noteText), "match", "else"), "alpha", "beta"),
		c("in-with-null", query.In(path(noteText), query.String("match"), query.Null()), "alpha", "delta", "epsilon", "gamma"),
		c("exists", query.Exists(path(noteText), true), "alpha", "beta"),
		c("not-exists", query.Exists(path(noteText), false), "delta", "epsilon", "gamma"),
		c("equals-null", query.Equal(path(noteText), query.Null()), "delta", "epsilon", "gamma"),
		c("not-equals-null", query.NotEqual(path(noteText), query.Null()), "alpha", "beta"),
		c("contains", query.Contains(path(noteText), "ATC"), "alpha"),
		c("like", query.Like(path(noteText), "mat ch"), "alpha"),
		c("or", query.Or(query.Equal(path(noteText), "match"), query.Equal(path("title"), "delta")), "alpha", "delta"),
		c("and", query.And(query.Equal(path(noteText), "else"), query.Equal(path(noteRank), 2.0)), "beta"),
		c("greater-than", query.GreaterThan(path(noteRank), 3.0), "alpha"),
		c("less-than-equal", query.LessThanEqual(path(noteRank), 2.0), "beta"),
		c("checkbox", query.Equal(path(notePinned), true), "alpha"),
		c("group-inside-nested-rows", query.Equal(path(noteLabel), "inner"), "alpha"),
		c("level-one-discriminator", query.Equal(path(panelText), "match"), "gamma"),
		c("level-one-discriminator-other-value", query.Equal(path(panelText), "panel-only"), "beta"),
		// Localized leaf inside nested rows: fr falls back to en.
		l("localized-en", "en", query.Equal(path(noteCaption), "hello"), "alpha"),
		l("localized-fr", "fr", query.Equal(path(noteCaption), "bonjour"), "alpha"),
		l("localized-fr-shadowed", "fr", query.Equal(path(noteCaption), "hello")),
		l("localized-fr-fallback", "fr", query.Equal(path(noteCaption), "world"), "beta"),
		l("localized-fr-exists", "fr", query.Exists(path(noteCaption), true), "alpha", "beta", "epsilon"),
		l("localized-fr-empty-falls-back", "fr", query.Equal(path(noteCaption), "fallback"), "epsilon"),
		// Relationship and upload targets inside nested blocks filter by ID.
		c("relationship", query.Equal(path(noteAuthor), f.authors["ada"]), "alpha"),
		c("relationship-in", query.In(path(noteAuthor), f.authors["grace"]), "beta"),
		c("relationship-not-exists", query.Exists(path(noteAuthor), false), "delta", "epsilon", "gamma"),
		c("upload", query.Equal(path(noteImage), f.media), "alpha"),
		c("upload-exists", query.Exists(path(noteImage), true), "alpha"),
		// Primitive lists and has-many selects inside nested rows.
		c("text-list-in", query.In(path(noteTags), "green"), "alpha"),
		c("text-list-in-any", query.In(path(noteTags), "blue", "absent"), "beta"),
		c("text-list-not-in", query.Not(query.In(path(noteTags), "green")), except("alpha")...),
		c("number-list-in", query.In(path(noteSizes), 2.0), "alpha"),
		c("number-list-exists", query.Exists(path(noteSizes), true), "alpha", "beta"),
		c("number-list-equals-null", query.Equal(path(noteSizes), query.Null()), "delta", "epsilon", "gamma"),
		c("multi-select-in", query.In(path(noteFlags), "red"), "alpha"),
		c("multi-select-not-in", query.Not(query.In(path(noteFlags), "red")), except("alpha")...),
		c("multi-select-exists", query.Exists(path(noteFlags), true), "alpha"),
		c("has-many-relationship-in", query.In(path(noteFans), f.authors["grace"]), "alpha"),
		c("has-many-relationship-not-in", query.Not(query.In(path(noteFans), f.authors["ada"])), except("alpha")...),
		// Opaque JSON compares a stored scalar of the operand's type; other
		// values that hold no scalar compare by presence alone.
		c("json-equals", query.Equal(path(noteData), "Launch plan"), "alpha"),
		c("json-contains", query.Contains(path(noteData), "PLAN"), "alpha"),
		c("json-number", query.GreaterThan(path(noteData), 2.0), "beta"),
		c("json-exists", query.Exists(path(noteData), true), "alpha", "beta"),
		c("has-many-relationship-exists", query.Exists(path(noteFans), true), "alpha"),
		c("container-exists", query.Exists(path(cardNotes), true), "alpha", "beta", "epsilon"),
		c("container-equals-null", query.Equal(path(cardNotes), query.Null()), "delta", "gamma"),
		c("root-container-exists", query.Exists(path("section"), true), "alpha", "beta", "gamma"),
		// Arrays inside a block inside the layout.
		c("array-in-block-relationship", query.Equal(path(linkAuthor), f.authors["ada"]), "alpha"),
		c("array-in-block-text-list", query.In(path(linkTags), "x"), "alpha"),
		// Arrays inside an array inside a group.
		c("group-array-array", query.Equal(path(cellValue), "c2"), "beta"),
		c("group-array-array-not", query.Not(query.Equal(path(cellValue), "c2")), except("beta")...),
		c("group-array-array-number", query.GreaterThan(path(cellSize), 5.0), "alpha"),
		c("group-array-array-number-list", query.In(path(cellSizes), 3.0), "alpha"),
		c("group-array-array-text-list", query.In(path(cellTags), "t1"), "alpha"),
		c("group-array-array-not-exists", query.Exists(path(cellValue), false), "delta", "epsilon", "gamma"),
		// A localized array containing an array, and a localized group
		// between two arrays.
		l("localized-root-en", "en", query.Equal(path(itemName), "apple"), "alpha"),
		l("localized-root-fr", "fr", query.Equal(path(itemName), "pomme"), "alpha"),
		l("localized-root-fr-shadowed", "fr", query.Equal(path(itemName), "apple")),
		l("localized-root-fr-fallback", "fr", query.Equal(path(itemName), "banana"), "beta"),
		l("localized-group-en", "en", query.Equal(path(entryWord), "cat"), "alpha"),
		l("localized-group-fr", "fr", query.Equal(path(entryWord), "chat"), "alpha"),
		l("localized-group-fr-shadowed", "fr", query.Equal(path(entryWord), "cat")),
		// Three repeated levels.
		c("three-levels", query.Equal(path(deepText), "deepest"), "alpha"),
		c("three-levels-not-exists", query.Exists(path(deepText), false), except("alpha")...),
	}
}

func configuration(storage *localstorage.Backend) ridu.Config {
	allow := func(ridu.AccessContext) (ridu.AccessDecision, error) { return ridu.Allow(), nil }
	open := ridu.CollectionAccess{Create: allow, Read: allow, Update: allow}
	return ridu.Config{
		Name: "Nested query paths", Storage: storage, StorageNamespace: "nested-paths",
		Localization: localization(), Plugins: []ridu.Plugin{richtext.New()},
		Collections: []ridu.Collection{
			{Slug: "nested-authors", Access: open, Fields: field.Fields{field.Text("name")}},
			{Slug: "nested-media", Access: open, Upload: true, UploadConfig: ridu.UploadConfig{MimeTypes: []string{"image/png"}}, Fields: field.Fields{field.Text("alt")}},
			{Slug: "nested-pages", Access: open, Fields: field.Fields{
				field.Text("title"),
				field.Blocks("layout", cardBlock(), field.Block{Slug: panelSlug, Fields: field.Fields{field.Blocks("children", noteBlock)}}),
				field.Group("section", field.Fields{field.Array("rows", field.Fields{
					field.Text("label"),
					field.Array("cells", field.Fields{field.Text("value"), field.Number("size"), field.NumberList("sizes"), field.TextList("tags")}),
				})}),
				field.Array("translated", field.Fields{field.Text("label"), field.Array("items", field.Fields{field.Text("name")})}).Localized(),
				field.Array("outer", field.Fields{field.Group("local", field.Fields{field.Array("entries", field.Fields{field.Text("word")})}).Localized()}),
				field.Array("deep", field.Fields{field.Array("middle", field.Fields{field.Blocks("leaves", noteBlock)})}),
				richtext.Field("body", richtext.Config{Blocks: []field.Block{quoteBlock}}),
			}},
		},
	}
}

func localization() ridu.LocalizationConfig {
	return ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{
		{Code: "en", Label: "English"},
		{Code: "fr", Label: "French", FallbackLocales: []schema.LocaleCode{"en"}},
	}}
}

var noteBlock = field.Block{Slug: noteSlug, Fields: field.Fields{
	field.Text("text"),
	field.Text("caption").Localized(),
	field.Number("rank"),
	field.Checkbox("pinned"),
	field.Relationship("author", "nested-authors"),
	field.Upload("image", "nested-media"),
	field.TextList("tags"),
	field.NumberList("sizes"),
	field.MultiSelect("flags", "red", "blue"),
	field.Group("meta", field.Fields{field.Text("label")}),
	field.JSON("data"),
	field.Relationships("fans", "nested-authors"),
	field.Text("secret").Access(field.Access{Read: func(fieldoperation.Context) (bool, error) { return false, nil }}),
}}

var quoteBlock = field.Block{Slug: quoteSlug, Fields: field.Fields{field.Text("text"), field.Number("rank")}}

func cardBlock() field.Block {
	return field.Block{Slug: cardSlug, Fields: field.Fields{
		field.Text("heading"),
		field.Blocks("children", noteBlock, quoteBlock),
		field.Array("links", field.Fields{
			field.Text("label"),
			field.Array("targets", field.Fields{field.Relationship("author", "nested-authors"), field.TextList("tags")}),
		}),
	}}
}

func seed(t *testing.T, app *ridu.App) fixture {
	t.Helper()
	result := fixture{ids: map[string]string{}, authors: map[string]string{}}
	for _, name := range []string{"ada", "grace"} {
		document, err := app.Local().Create(t.Context(), "nested-authors", store.Values{"name": store.String(name)}, ridu.MutationOptions{})
		if err != nil {
			t.Fatal(err)
		}
		result.authors[name] = document.ID
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewNRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	media, err := app.Upload(t.Context(), "nested-media", ridu.UploadInput{Filename: "pixel.png", Reader: bytes.NewReader(encoded.Bytes())})
	if err != nil {
		t.Fatal(err)
	}
	result.media = media.ID
	ada, grace := store.String(result.authors["ada"]), store.String(result.authors["grace"])
	texts := func(values ...string) store.Value {
		items := make([]store.Value, len(values))
		for index, value := range values {
			items[index] = store.String(value)
		}
		return store.List(items...)
	}
	numbers := func(values ...float64) store.Value {
		items := make([]store.Value, len(values))
		for index, value := range values {
			items[index] = store.Number(value)
		}
		return store.List(items...)
	}
	documents := map[string]store.Values{
		"alpha": {
			"layout": store.List(card("a1", store.Values{
				"heading": store.String("Intro"),
				"links":   store.List(row("a1-l", store.Values{"label": store.String("l1"), "targets": store.List(row("a1-t", store.Values{"author": ada, "tags": texts("x")}))})),
			},
				note("a1-n", store.Values{
					"text": store.String("match"), "caption": store.String("hello"),
					"rank": store.Number(5), "pinned": store.Boolean(true), "author": ada, "image": store.String(result.media),
					"tags": texts("red", "green"), "sizes": numbers(1, 2), "flags": texts("red"),
					"meta": store.Object(store.Values{"label": store.String("inner")}), "secret": store.String("hidden"),
					"data": store.String("Launch plan"), "fans": store.List(ada, grace),
				}),
				quote("a1-q", "other", 1),
			)),
			"section": store.Object(store.Values{"rows": store.List(row("a-r", store.Values{"label": store.String("r1"), "cells": store.List(row("a-c", store.Values{
				"value": store.String("c1"), "size": store.Number(10), "sizes": numbers(3), "tags": texts("t1"),
			}))}))}),
			"translated": store.List(row("a-te", store.Values{"label": store.String("en-row"), "items": store.List(row("a-tei", store.Values{"name": store.String("apple")}))})),
			"outer":      store.List(row("a-o", store.Values{"local": store.Object(store.Values{"entries": store.List(row("a-oe", store.Values{"word": store.String("cat")}))})})),
			"deep":       store.List(row("a-d", store.Values{"middle": store.List(row("a-dm", store.Values{"leaves": store.List(note("a-dl", store.Values{"text": store.String("deepest")}))}))})),
		},
		"beta": {
			"layout": store.List(
				card("b1", store.Values{"heading": store.String("Other")},
					quote("b1-q", "match", 9),
					note("b1-n", store.Values{
						"text": store.String("else"), "caption": store.String("world"),
						"rank": store.Number(2), "author": grace, "tags": texts("blue"), "sizes": numbers(),
						"data": store.Number(3),
					}),
				),
				panel("b2", note("b2-n", store.Values{"text": store.String("panel-only"), "rank": store.Number(7)})),
			),
			"section": store.Object(store.Values{"rows": store.List(row("b-r", store.Values{"label": store.String("r2"), "cells": store.List(
				row("b-c1", store.Values{"value": store.String("c2"), "size": store.Number(3)}),
				row("b-c2", store.Values{"value": store.String("c3")}),
			)}))}),
			"translated": store.List(row("b-te", store.Values{"label": store.String("en-only"), "items": store.List(row("b-tei", store.Values{"name": store.String("banana")}))})),
			"deep":       store.List(row("b-d", store.Values{"middle": store.List(row("b-dm", store.Values{"leaves": store.List()}))})),
		},
		"gamma": {
			"layout":  store.List(panel("g1", note("g1-n", store.Values{"text": store.String("match"), "rank": store.Number(5), "author": ada}))),
			"section": store.Object(store.Values{"rows": store.List(row("g-r", store.Values{"label": store.String("r3"), "cells": store.List()}))}),
		},
		"delta": {},
		"epsilon": {
			"layout": store.List(card("e1", store.Values{"heading": store.String("Empty")},
				note("e1-n", store.Values{"text": store.Null(), "caption": store.String("fallback")}),
			)),
		},
	}
	// French values are written by a French update of the same keyed rows;
	// omitted children of retained rows keep their stored values.
	french := map[string]store.Values{
		"alpha": {
			"layout":     store.List(card("a1", nil, note("a1-n", store.Values{"caption": store.String("bonjour")}), block(quoteSlug, "a1-q", store.Values{}))),
			"translated": store.List(row("a-tf", store.Values{"label": store.String("fr-row"), "items": store.List(row("a-tfi", store.Values{"name": store.String("pomme")}))})),
			"outer":      store.List(row("a-o", store.Values{"local": store.Object(store.Values{"entries": store.List(row("a-of", store.Values{"word": store.String("chat")}))})})),
		},
		"epsilon": {
			"layout": store.List(card("e1", nil, note("e1-n", store.Values{"caption": store.String("")}))),
		},
	}
	for _, title := range []string{"alpha", "beta", "gamma", "delta", "epsilon"} {
		values := documents[title]
		values["title"] = store.String(title)
		document, err := app.Local().Create(t.Context(), "nested-pages", values, ridu.MutationOptions{Locale: "en"})
		if err != nil {
			t.Fatalf("create %s: %s", title, describe(err))
		}
		result.ids[title] = document.ID
		if update, found := french[title]; found {
			if _, err := app.Local().Update(t.Context(), "nested-pages", document.ID, update, ridu.MutationOptions{Locale: "fr"}); err != nil {
				t.Fatalf("translate %s: %s", title, describe(err))
			}
		}
	}
	return result
}

func row(key string, values store.Values) store.Value {
	values["_key"] = store.String(key)
	return store.Object(values)
}

func block(slug, key string, values store.Values) store.Value {
	values["blockType"] = store.String(slug)
	return row(key, values)
}

func note(key string, values store.Values) store.Value { return block(noteSlug, key, values) }

func quote(key, text string, rank float64) store.Value {
	return block(quoteSlug, key, store.Values{"text": store.String(text), "rank": store.Number(rank)})
}

func card(key string, values store.Values, children ...store.Value) store.Value {
	if values == nil {
		values = store.Values{}
	}
	values["children"] = store.List(children...)
	return block(cardSlug, key, values)
}

func panel(key string, children ...store.Value) store.Value {
	return block(panelSlug, key, store.Values{"children": store.List(children...)})
}

func path(value string) query.Path {
	parsed, err := query.ParsePath(value)
	if err != nil {
		panic(err)
	}
	return parsed
}

func unsupported(t *testing.T, err error, path string) {
	t.Helper()
	var failure *operation.Error
	if !errors.As(err, &failure) || failure.Status != http.StatusBadRequest || failure.Code != "bad_query" || len(failure.Issues) != 1 || failure.Issues[0].Code != "unsupported_path" || failure.Issues[0].Path != path {
		t.Errorf("query %s error = %v, want bad_query (400) with an unsupported_path issue", path, describe(err))
	}
}

func describe(err error) string {
	var failure *operation.Error
	if errors.As(err, &failure) {
		encoded, _ := json.Marshal(failure.Issues)
		return fmt.Sprintf("%d %s %s %s (cause: %v)", failure.Status, failure.Code, failure.Message, encoded, failure.Cause)
	}
	return fmt.Sprint(err)
}

func request(handler http.Handler, method, target string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(method, "http://ridu.test"+target, nil))
	return response
}
