package unifiedfields_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/tests/contracts/unifiedfields"
	g "github.com/riducms/ridu/tests/contracts/unifiedfields/generated"
)

func TestGeneratedUnifiedContractsThroughTypedLocalAPI(t *testing.T) {
	app, err := core.New(unifiedfields.Config(), teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	users := g.UsersCollection.With(app.Local())
	user, err := users.Create(t.Context(), g.UserCreate{Name: "Ada"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	articles := g.UnifiedArticlesCollection.With(app.Local())
	var input g.UnifiedArticleCreate
	if err := json.Unmarshal([]byte(`{"title":"Typed graph","sku":" sku-root ","sections":[{"_key":"A","products":[{"_key":"B","sku":"sku-b"}]}],"content":[{"blockType":"card","_key":"C","sku":"sku-c"}],"localizedTitle":"English","privateNote":"redacted"}`), &input); err != nil {
		t.Fatal(err)
	}
	input.Author = core.Set(user.ID)
	created, err := articles.Create(t.Context(), input, nil)
	if err != nil {
		t.Fatal(err)
	}
	if created.Sku == nil || *created.Sku != "SKU-ROOT" || created.Defaulted == nil || *created.Defaulted != "Ready" || created.Summary == nil || *created.Summary != "Article: Typed graph" || created.PrivateNote != nil {
		t.Fatalf("typed callback/default/redaction result: %#v", created)
	}
	if len(created.Sections) != 1 || created.Sections[0].Key != "A" || created.Sections[0].Products[0].Key != "B" || *created.Sections[0].Products[0].Sku != "SKU-B" {
		t.Fatal("typed nested identity/value")
	}
	if created.Content == nil || len(*created.Content) != 1 || (*created.Content)[0].BlockType() != "card" || (*created.Content)[0].BlockKey() != "C" {
		t.Fatal("typed Block discriminated identity")
	}
	var patch g.UnifiedArticleUpdate
	if err := json.Unmarshal([]byte(`{"sections":[{"_key":"A","products":[{"_key":"B","sku":"sku-new"}]}],"localizedTitle":"Français"}`), &patch); err != nil {
		t.Fatal(err)
	}
	updated, err := articles.Update(t.Context(), created.ID, patch, nil, core.TypedLocaleOptions{Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	if *updated.Sections[0].Products[0].Sku != "SKU-NEW" {
		t.Fatal("typed keyed patch lost hook")
	}
	all, err := g.UnifiedArticlesCollectionAllLocales.With(app.Local()).Find(t.Context(), created.ID, core.TypedReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if all.LocalizedTitle == nil || *(*all.LocalizedTitle)["en"] != "English" || *(*all.LocalizedTitle)["fr"] != "Français" {
		t.Fatal("typed locale output")
	}
	authorPath, _ := query.ParsePath("author")
	found, err := articles.Find(t.Context(), created.ID, core.TypedReadOptions{Populate: []query.Population{{Path: authorPath, Depth: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	if found.Author == nil || found.Author.Document == nil || *found.Author.Document.Name != "Ada" {
		t.Fatal("typed populated reference")
	}
	titlePath, _ := query.ParsePath("title")
	selected, err := articles.Find(t.Context(), created.ID, core.TypedReadOptions{Select: []query.Path{titlePath}})
	if err != nil {
		t.Fatal(err)
	}
	if selected.Title == nil || selected.Sku != nil || selected.Sections != nil {
		t.Fatal("typed selected output preserves omission")
	}
	_, err = articles.Update(t.Context(), created.ID, g.UnifiedArticleUpdate{Sku: core.Set("invalid")}, nil)
	var operationError *core.OperationError
	if !errors.As(err, &operationError) || len(operationError.Issues) != 1 || operationError.Issues[0].Code != "sku" || operationError.Issues[0].Path != "sku" || operationError.Issues[0].Target == "" {
		t.Fatalf("typed structured issue: %v", err)
	}
}
