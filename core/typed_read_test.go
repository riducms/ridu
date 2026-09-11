package core_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/query"
)

func TestTypedReadsOwnTheirLocaleShape(t *testing.T) {
	app, err := core.New(core.Config{Name: "Typed locales", Localization: core.LocalizationConfig{DefaultLocale: "en", Locales: []core.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}}}, Collections: []core.Collection{{Slug: "pages", Fields: field.Fields{field.Text("title").Required().Localized()}}}, Globals: []core.Global{{Slug: "site", Fields: field.Fields{field.Text("title").Localized()}}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	type input struct {
		Title string `json:"title"`
	}
	type document struct {
		ID    string  `json:"id"`
		Title *string `json:"title"`
	}
	type allDocument struct {
		Title map[string]string `json:"title"`
	}
	pages := core.NewTypedCollection[document, input, input]("pages").With(app.Local())
	created, err := pages.Create(t.Context(), input{Title: "Hello"}, core.TypedMutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pages.Update(t.Context(), created.ID, input{Title: "Bonjour"}, core.TypedMutationOptions{Locale: "fr"}); err != nil {
		t.Fatal(err)
	}
	read, err := core.NewTypedCollection[document, input, input]("pages").With(app.Local()).Find(t.Context(), created.ID, core.TypedReadOptions{Locale: "fr"})
	if err != nil || read.Title == nil || *read.Title != "Bonjour" {
		t.Fatalf("single locale: %#v %v", read, err)
	}
	all, err := core.NewTypedAllLocalesCollection[allDocument]("pages").With(app.Local()).Find(t.Context(), created.ID, core.TypedReadOptions{})
	if err != nil || all.Title["en"] != "Hello" || all.Title["fr"] != "Bonjour" {
		t.Fatalf("all locales: %#v %v", all, err)
	}
	listed, err := core.NewTypedCollection[document, input, input]("pages").With(app.Local()).List(t.Context(), core.TypedListOptions{Locale: "fr", Limit: 1})
	if err != nil || listed.Total != 1 || listed.Page != 1 || listed.Limit != 1 || len(listed.Documents) != 1 || listed.Documents[0].Title == nil || *listed.Documents[0].Title != "Bonjour" {
		t.Fatalf("single-locale list: %#v %v", listed, err)
	}
	allListed, err := core.NewTypedAllLocalesCollection[allDocument]("pages").With(app.Local()).List(t.Context(), core.TypedListOptions{})
	if err != nil || len(allListed.Documents) != 1 || allListed.Documents[0].Title["en"] != "Hello" || allListed.Documents[0].Title["fr"] != "Bonjour" {
		t.Fatalf("all-locales list: %#v %v", allListed, err)
	}
	projectedList, err := core.NewTypedCollection[document, input, input]("pages").With(app.Local()).List(t.Context(), core.TypedListOptions{Select: []query.Path{}})
	if err != nil || len(projectedList.Documents) != 1 || projectedList.Documents[0].Title != nil || projectedList.Documents[0].ID != created.ID {
		t.Fatalf("projected list: %#v %v", projectedList, err)
	}
	// An incompatible consumer must fail without exposing a partially decoded page.
	bad, err := core.NewTypedCollection[allDocument, input, input]("pages").With(app.Local()).List(t.Context(), core.TypedListOptions{})
	var typeError *json.UnmarshalTypeError
	if !errors.As(err, &typeError) || !strings.Contains(err.Error(), "pages list document[0]") || bad.Documents != nil || bad.Total != 0 {
		t.Fatalf("decode failure: %#v %v", bad, err)
	}
	projected, err := core.NewTypedCollection[document, input, input]("pages").With(app.Local()).Find(t.Context(), created.ID, core.TypedReadOptions{Select: []query.Path{}})
	if err != nil || projected.Title != nil {
		t.Fatalf("projection: %#v %v", projected, err)
	}
	site := core.NewTypedGlobal[document, input]("site").With(app.Local())
	if _, err := site.Update(t.Context(), input{Title: "Hello"}, core.TypedMutationOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := site.Update(t.Context(), input{Title: "Bonjour"}, core.TypedMutationOptions{Locale: "fr"}); err != nil {
		t.Fatal(err)
	}
	global, err := core.NewTypedAllLocalesGlobal[allDocument]("site").With(app.Local()).Find(t.Context(), core.TypedReadOptions{})
	if err != nil || global.Title["en"] != "Hello" || global.Title["fr"] != "Bonjour" {
		t.Fatalf("global all locales: %#v %v", global, err)
	}
}

func TestTypedReadListPreservesEngineFiltering(t *testing.T) {
	titlePath, err := query.ParsePath("title")
	if err != nil {
		t.Fatal(err)
	}
	visible, err := query.Compare(titlePath, query.OperatorNotEqual, query.String("hidden"))
	if err != nil {
		t.Fatal(err)
	}
	denied := false
	app, err := core.New(core.Config{Name: "Typed list access", Collections: []core.Collection{{Slug: "pages", Fields: field.Fields{field.Text("title").Required(), field.Text("secret").Access(field.Access{Read: func(operation.Context) (bool, error) {
		return false, nil
	}})}, Access: core.CollectionAccess{Read: func(core.AccessContext) (core.AccessDecision, error) {
		if denied {
			return core.Deny(), nil
		}
		return core.Where(visible), nil
	}}}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	type input struct {
		Title  string `json:"title"`
		Secret string `json:"secret"`
	}
	type document struct {
		Title  *string `json:"title"`
		Secret *string `json:"secret"`
	}
	writes := core.NewTypedCollection[document, input, input]("pages").With(app.Local())
	for _, title := range []string{"b", "hidden", "a"} {
		if _, err := writes.Create(t.Context(), input{Title: title, Secret: "private"}, core.TypedMutationOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	reads := core.NewTypedCollection[document, input, input]("pages").With(app.Local())
	result, err := reads.List(t.Context(), core.TypedListOptions{Page: 2, Limit: 1, Sort: []query.Sort{{Path: titlePath, Direction: query.Ascending}}})
	if err != nil || result.Total != 2 || result.Page != 2 || len(result.Documents) != 1 || result.Documents[0].Title == nil || *result.Documents[0].Title != "b" || result.Documents[0].Secret != nil {
		t.Fatalf("filtered page: %#v %v", result, err)
	}
	hidden, err := query.Compare(titlePath, query.OperatorEqual, query.String("hidden"))
	if err != nil {
		t.Fatal(err)
	}
	empty, err := reads.List(t.Context(), core.TypedListOptions{Where: hidden})
	if err != nil || empty.Total != 0 || empty.Documents == nil || len(empty.Documents) != 0 {
		t.Fatalf("empty access intersection: %#v %v", empty, err)
	}
	failed, err := core.NewTypedCollection[selectiveReadDocument, input, input]("pages").With(app.Local()).List(t.Context(), core.TypedListOptions{Sort: []query.Sort{{Path: titlePath, Direction: query.Ascending}}})
	if !errors.Is(err, errTypedListDocument) || !strings.Contains(err.Error(), "document[1]") || failed.Documents != nil || failed.Total != 0 {
		t.Fatalf("partial decode escaped: %#v %v", failed, err)
	}
	denied = true
	rejected, err := reads.List(t.Context(), core.TypedListOptions{})
	if err == nil || rejected.Documents != nil || rejected.Total != 0 {
		t.Fatalf("denied list: %#v %v", rejected, err)
	}
}

var errTypedListDocument = errors.New("consumer decode failure")

type selectiveReadDocument struct {
	Title string `json:"title"`
}

func (document *selectiveReadDocument) UnmarshalJSON(data []byte) error {
	type payload selectiveReadDocument
	var value payload
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	if value.Title == "b" {
		return errTypedListDocument
	}
	*document = selectiveReadDocument(value)
	return nil
}
