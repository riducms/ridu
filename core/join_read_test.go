package core_test

import (
	"errors"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/store"
)

func TestListJoinPreservesSourceReadRulesAndPrivateMembership(t *testing.T) {
	app, err := ridu.New(ridu.Config{Name: "Authorized join reads", Collections: []ridu.Collection{
		{Slug: "categories", Fields: field.Fields{field.Text("name"), field.Checkbox("confidential"), field.Join("posts", "posts", "category").Access(field.Access{Read: func(ctx operation.Context) (bool, error) {
			confidential, _ := ctx.Siblings.Get("confidential").
				BooleanValue()
			return !confidential, nil
		}})}},
		{Slug: "posts", Fields: field.Fields{field.Text("title"), field.Relationship("category", "categories").Access(field.Access{Read: func(operation.Context) (bool, error) {
			return false, nil
		}})}},
	}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	for _, confidential := range []bool{false, true} {
		category, err := app.Local().Create(t.Context(), "categories", store.Values{"name": store.String("Category"), "confidential": store.Boolean(confidential)}, ridu.MutationOptions{})
		if err != nil {
			t.Fatal(err)
		}
		_, err = app.Local().Create(t.Context(), "posts", store.Values{"title": store.String("Story"), "category": store.String(category.ID)}, ridu.MutationOptions{})
		if err != nil {
			t.Fatal(err)
		}
		page, err := app.Local().ListJoin(t.Context(), "categories", category.ID, "posts", ridu.ListOptions{})
		if confidential {
			var denied *ridu.OperationError
			if !errors.As(err, &denied) || denied.Code != "field_access_denied" {
				t.Fatalf("private source join = %#v, %v", page, err)
			}
			continue
		}
		if err != nil || *page.Total != 1 || len(page.Documents) != 1 {
			t.Fatalf("public configured join = %#v, %v", page, err)
		}
		if _, exists := page.Documents[0].Values["category"]; exists {
			t.Fatal("private backing relation returned")
		}
		path, _ := query.ParsePath("category")
		_, err = app.Local().ListJoin(t.Context(), "categories", category.ID, "posts", ridu.ListOptions{Where: query.Equal(path, category.ID)})
		var denied *ridu.OperationError
		if !errors.As(err, &denied) || denied.Code != "field_access_denied" {
			t.Fatalf("caller cannot reuse private membership as filter: %v", err)
		}
	}
}

func TestListJoinUsesTheJoinsDefaultSortUnlessTheCallerSorts(t *testing.T) {
	app, err := ridu.New(ridu.Config{Name: "Join default sort", Collections: []ridu.Collection{
		{Slug: "categories", Fields: field.Fields{field.Text("name"), field.Join("posts", "posts", "category").DefaultSort("-title")}},
		{Slug: "posts", Fields: field.Fields{field.Text("title"), field.Relationship("category", "categories")}},
	}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	category, err := app.Local().Create(t.Context(), "categories", store.Values{"name": store.String("News")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, title := range []string{"Beta", "Alpha", "Gamma"} {
		if _, err := app.Local().Create(t.Context(), "posts", store.Values{"title": store.String(title), "category": store.String(category.ID)}, ridu.MutationOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	titles := func(page store.Page) []string {
		result := make([]string, len(page.Documents))
		for index, document := range page.Documents {
			result[index], _ = document.Values["title"].StringValue()
		}
		return result
	}
	page, err := app.Local().ListJoin(t.Context(), "categories", category.ID, "posts", ridu.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := titles(page); len(got) != 3 || got[0] != "Gamma" || got[1] != "Beta" || got[2] != "Alpha" {
		t.Fatalf("default join order = %v, want Gamma, Beta, Alpha", got)
	}
	titlePath, _ := query.ParsePath("title")
	ascending, _ := query.NewSort(titlePath, query.Ascending)
	page, err = app.Local().ListJoin(t.Context(), "categories", category.ID, "posts", ridu.ListOptions{Sort: []query.Sort{ascending}})
	if err != nil {
		t.Fatal(err)
	}
	if got := titles(page); len(got) != 3 || got[0] != "Alpha" || got[2] != "Gamma" {
		t.Fatalf("caller join order = %v, want Alpha first", got)
	}
}
