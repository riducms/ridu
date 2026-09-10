package core_test

import (
	"context"
	"encoding/json"
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/store"
)

func TestBlockIdentityMatchesDirectAndRecursivePopulatedReads(t *testing.T) {
	ctx := context.Background()
	backend := teststore.New()
	app, err := ridu.New(ridu.Config{Name: "Populated blocks", Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}}}, Collections: []ridu.Collection{
		{Slug: "pages", Fields: field.Fields{field.Blocks("layout", field.Block{Slug: "hero", Fields: field.Fields{field.Text("heading"), field.Blocks("children", field.Block{Slug: "text", Fields: field.Fields{field.Text("body")}})}}).Localized(), field.Group("section", field.Fields{field.Blocks("cards", field.Block{Slug: "card", Fields: field.Fields{field.Text("title")}})}).Localized(), field.Relationship("related", "pages"), field.Join("incoming", "pages", "related")}},
		{Slug: "links", Fields: field.Fields{field.Relationship("page", "pages").Localized()}},
	}}, backend)
	if err != nil {
		t.Fatal(err)
	}
	rows := store.List(store.Object(store.Values{"blockType": store.String("hero"), "heading": store.String("Created through API"), "children": store.List(store.Object(store.Values{"blockType": store.String("text"), "body": store.String("Child")}))}))
	section := store.Object(store.Values{"cards": store.List(store.Object(store.Values{"blockType": store.String("card"), "title": store.String("Card")}))})
	second, err := app.Local().Create(ctx, "pages", store.Values{"section": section, "layout": rows}, nil)
	if err != nil {
		t.Fatal(err)
	}
	first, err := app.Local().Create(ctx, "pages", store.Values{"section": section, "layout": rows, "related": store.String(second.ID)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = app.Local().Update(ctx, "pages", first.ID, store.Values{"layout": rows}, nil, ridu.LocaleOptions{Locale: "fr"}); err != nil {
		t.Fatal(err)
	}
	link, err := app.Local().Create(ctx, "links", store.Values{"page": store.String(first.ID)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	path, _ := query.NewPath("page")
	for _, options := range []ridu.FindOptions{{Locale: "en"}, {Locale: "fr"}, {AllLocales: true}} {
		t.Run(string(options.Locale)+map[bool]string{true: "all"}[options.AllLocales], func(t *testing.T) {
			direct, err := app.Local().FindWithOptions(ctx, "pages", first.ID, options)
			if err != nil {
				t.Fatal(err)
			}
			nestedDirect, err := app.Local().FindWithOptions(ctx, "pages", second.ID, options)
			if err != nil {
				t.Fatal(err)
			}
			joinedRows, _ := nestedDirect.Values["incoming"].CopyList()
			if len(joinedRows) != 1 {
				t.Fatal("inverse join target missing")
			}
			joined, ok := joinedRows[0].CopyDocument()
			if !ok || !equalBlockIdentityJSON(t, direct.Values["layout"], joined.Values["layout"]) || !equalBlockIdentityJSON(t, direct.Values["section"], joined.Values["section"]) {
				t.Fatal("inverse join identities differ from direct read")
			}
			options.Populate = []query.Population{{Path: path, Depth: 2}}
			populated, err := app.Local().FindWithOptions(ctx, "links", link.ID, options)
			if err != nil {
				t.Fatal(err)
			}
			pageValue := populated.Values["page"]
			if options.AllLocales {
				locales, _ := pageValue.CopyObject()
				pageValue = locales["en"]
			}
			target, ok := pageValue.CopyDocument()
			if !ok {
				t.Fatal("page not populated")
			}
			nested, ok := target.Values["related"].CopyDocument()
			if !ok {
				t.Fatal("recursive target not populated")
			}
			for _, pair := range [][2]store.Document{{direct, target}, {nestedDirect, nested}} {
				if !equalBlockIdentityJSON(t, pair[0].Values["layout"], pair[1].Values["layout"]) {
					t.Fatalf("direct/populated layout differ: %#v / %#v", pair[0].Values["layout"], pair[1].Values["layout"])
				}
				if !equalBlockIdentityJSON(t, pair[0].Values["section"], pair[1].Values["section"]) {
					t.Fatal("localized group direct/populated identities differ")
				}
				sectionValue := pair[1].Values["section"]
				if options.AllLocales {
					locales, _ := sectionValue.CopyObject()
					sectionValue = locales["en"]
				}
				sectionObject, _ := sectionValue.CopyObject()
				if blockKey(blockRows(sectionObject["cards"])[0]) == "" {
					t.Fatal("localized group identity missing")
				}
				layout := pair[1].Values["layout"]
				if options.AllLocales {
					locales, _ := layout.CopyObject()
					layout = locales["en"]
				}
				row := blockRows(layout)[0]
				if blockKey(row) == "" || blockKey(blockRows(row["children"])[0]) == "" {
					t.Fatal("populated identities missing")
				}
			}
		})
	}
}

func equalBlockIdentityJSON(t *testing.T, left, right store.Value) bool {
	t.Helper()
	l, err := json.Marshal(left)
	if err != nil {
		t.Fatal(err)
	}
	r, err := json.Marshal(right)
	if err != nil {
		t.Fatal(err)
	}
	return string(l) == string(r)
}
