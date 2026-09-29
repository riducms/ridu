package core_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/store"
)

type benchLink struct {
	Key   string `json:"_key,omitempty"`
	Label string `json:"label"`
	URL   string `json:"url"`
}

type benchPage struct {
	ID      string  `json:"id"`
	Title   *string `json:"title"`
	Summary *string `json:"summary"`
	SEO     *struct {
		Title *string `json:"title"`
	} `json:"seo"`
	Links []benchLink     `json:"links"`
	Body  json.RawMessage `json:"body"`
}

type benchPageInput struct {
	Title   string          `json:"title"`
	Summary string          `json:"summary"`
	SEO     map[string]any  `json:"seo"`
	Links   []benchLink     `json:"links"`
	Body    json.RawMessage `json:"body"`
}

func benchPageApp(b *testing.B) (*core.App, benchPageInput) {
	app, err := core.New(core.Config{Name: "Bench", Collections: []core.Collection{{Slug: "pages", Fields: field.Fields{
		field.Text("title"), field.Textarea("summary"),
		field.Group("seo", field.Fields{field.Text("title")}),
		field.Array("links", field.Fields{field.Text("label"), field.Text("url")}),
		field.JSON("body"),
	}}}}, teststore.New())
	if err != nil {
		b.Fatal(err)
	}
	var body strings.Builder
	body.WriteString(`{"root":{"children":[`)
	for index := range 30 {
		if index > 0 {
			body.WriteByte(',')
		}
		fmt.Fprintf(&body, `{"type":"paragraph","children":[{"type":"text","text":"Paragraph %d","format":0},{"type":"link","children":[{"type":"text","text":"link","format":1}]}]}`, index)
	}
	body.WriteString(`]}}`)
	links := make([]benchLink, 20)
	for index := range links {
		links[index] = benchLink{Label: fmt.Sprintf("Link %d", index), URL: fmt.Sprintf("/l/%d", index)}
	}
	return app, benchPageInput{Title: "Home", Summary: "Start here", SEO: map[string]any{"title": "Welcome"}, Links: links, Body: json.RawMessage(body.String())}
}

func BenchmarkTypedHandleOverhead(b *testing.B) {
	app, input := benchPageApp(b)
	typed := core.NewTypedCollection[benchPage, benchPageInput, benchPageInput]("pages").With(app.Local())
	var created benchPage
	for range 20 {
		page, err := typed.Create(b.Context(), input, core.TypedMutationOptions{})
		if err != nil {
			b.Fatal(err)
		}
		created = page
	}
	b.Run("untyped find", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := app.Local().Find(b.Context(), "pages", created.ID, core.FindOptions{}); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("typed find", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := typed.Find(b.Context(), created.ID, core.TypedReadOptions{}); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("untyped list 20", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := app.Local().List(b.Context(), "pages", core.ListOptions{Limit: 20}); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("typed list 20", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := typed.List(b.Context(), core.TypedListOptions{Limit: 20}); err != nil {
				b.Fatal(err)
			}
		}
	})
	encoded, _ := json.Marshal(input)
	b.Run("untyped create", func(b *testing.B) {
		app, _ := benchPageApp(b)
		var values store.Values
		_ = values.UnmarshalJSON(encoded)
		b.ReportAllocs()
		for b.Loop() {
			if _, err := app.Local().Create(b.Context(), "pages", values, core.MutationOptions{}); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("typed create", func(b *testing.B) {
		app, _ := benchPageApp(b)
		typed := core.NewTypedCollection[benchPage, benchPageInput, benchPageInput]("pages").With(app.Local())
		b.ReportAllocs()
		for b.Loop() {
			if _, err := typed.Create(b.Context(), input, core.TypedMutationOptions{}); err != nil {
				b.Fatal(err)
			}
		}
	})
}
