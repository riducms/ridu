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
		if err != nil || page.Total != 1 || len(page.Documents) != 1 {
			t.Fatalf("public configured join = %#v, %v", page, err)
		}
		if _, exists := page.Documents[0].Values["category"]; exists {
			t.Fatal("private backing relation returned")
		}
		path, _ := query.ParsePath("category")
		_, err = app.Local().ListJoin(t.Context(), "categories", category.ID, "posts", ridu.ListOptions{Where: query.Equal(path, query.String(category.ID))})
		var denied *ridu.OperationError
		if !errors.As(err, &denied) || denied.Code != "field_access_denied" {
			t.Fatalf("caller cannot reuse private membership as filter: %v", err)
		}
	}
}
