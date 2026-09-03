package core_test

import (
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/store"
)

func TestLocalDistinctComposesReadAccessAndProtectsFieldValues(t *testing.T) {
	title, _ := query.NewPath("title")
	audience, _ := query.NewPath("audience")
	secret, _ := query.NewPath("secret")
	application, err := ridu.New(ridu.Config{Name: "Distinct", Collections: []ridu.Collection{{
		Slug: "posts",
		Fields: []field.Definition{
			field.Text("title"), field.Text("audience"), field.Text("secret"),
		},
		Access: ridu.CollectionAccess{Read: func(ridu.AccessContext) (ridu.AccessDecision, error) {
			return ridu.Where(query.Equal(audience, query.String("public"))), nil
		}},
		FieldAccess: map[string]ridu.FieldAccess{"secret": {Read: func(ctx ridu.FieldAccessContext) (bool, error) {
			return ctx.Actor != nil && ctx.ActorCollection == "staff", nil
		}}},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	for _, values := range []store.Values{
		{"title": store.String("alpha"), "audience": store.String("public"), "secret": store.String("one")},
		{"title": store.String("alpha"), "audience": store.String("public"), "secret": store.String("two")},
		{"title": store.String("beta"), "audience": store.String("public"), "secret": store.String("one")},
		{"title": store.String("excluded"), "audience": store.String("public"), "secret": store.String("three")},
		{"title": store.String("private"), "audience": store.String("private"), "secret": store.String("hidden")},
	} {
		if _, err := application.Local().Create(t.Context(), "posts", values, nil); err != nil {
			t.Fatal(err)
		}
	}

	page, err := application.Local().Distinct(t.Context(), "posts", ridu.DistinctOptions{
		Field: title, Where: query.NotEqual(title, query.String("excluded")), Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || len(page.Values) != 2 {
		t.Fatalf("access-filtered distinct page = %#v, want alpha and beta", page)
	}
	first, firstOK := page.Values[0].StringValue()
	second, secondOK := page.Values[1].StringValue()
	if !firstOK || !secondOK || first != "alpha" || second != "beta" {
		t.Fatalf("access-filtered distinct values = %#v, want alpha and beta", page.Values)
	}

	if _, err := application.Local().Distinct(t.Context(), "posts", ridu.DistinctOptions{Field: secret, Limit: 10}); !operationCode(err, "field_access_denied") {
		t.Fatalf("anonymous secret distinct error = %v, want field_access_denied", err)
	}
	if _, err := application.Local().Distinct(t.Context(), "posts", ridu.DistinctOptions{
		Field: secret, Limit: 10, Actor: &store.Document{ID: "editor"}, ActorCollection: "staff",
	}); !operationCode(err, "field_access_denied") {
		t.Fatalf("staff secret distinct error = %v, want field_access_denied for document-aware field", err)
	}
}

func TestLocalDistinctBoundsRecursiveAccess(t *testing.T) {
	title, _ := query.NewPath("title")
	accessCalls := 0
	application, err := ridu.New(ridu.Config{Name: "Recursive distinct", Collections: []ridu.Collection{{
		Slug:   "posts",
		Fields: []field.Definition{field.Text("title")},
		Access: ridu.CollectionAccess{Read: func(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
			accessCalls++
			_, err := ctx.Local.Distinct(ctx.Context, "posts", ridu.DistinctOptions{Field: title})
			return ridu.Deny(), err
		}},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}

	if _, err := application.Local().Distinct(t.Context(), "posts", ridu.DistinctOptions{Field: title}); !operationCode(err, "operation_recursion") {
		t.Fatalf("recursive distinct error = %v, want operation_recursion", err)
	}
	if accessCalls != 4 {
		t.Fatalf("recursive distinct evaluated access %d times, want 4 bounded frames", accessCalls)
	}
}
