package core_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/store"
)

func TestVirtualFieldsAndInverseJoinsResolveOnRead(t *testing.T) {
	visiblePath, err := query.NewPath("visible")
	if err != nil {
		t.Fatal(err)
	}
	joinedReadHooks := 0
	application, err := ridu.New(ridu.Config{Name: "Computed output", Collections: []ridu.Collection{
		{
			Slug: "categories",
			Fields: field.Fields{field.Text("name").Required(), field.Virtual("displayName", field.ValueString, func(ctx operation.Context) (operation.Value[store.
				Value],

				error) {
				name, _ := ctx.Root.Get("name").
					StringValue()
				return operation.Present(store.String("Category: " + name)), nil
			}),

				field.Join("posts", "posts", "category").Limit(2)},
		},
		{
			Slug: "posts",
			Fields: field.Fields{field.Text("title").Required(), field.Relationship("category", "categories").Required(), field.Checkbox("visible").Required(), field.Text("secret").Access(field.Access{Read: func(operation.Context) (bool, error) {
				return false, nil
			}})},
			Access: ridu.CollectionAccess{Read: func(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
				if ctx.Actor == nil {
					return ridu.Deny(), nil
				}
				return ridu.Where(query.Equal(visiblePath, query.Boolean(true))), nil
			}},

			Hooks: ridu.CollectionHooks{AfterRead: []ridu.Hook{func(ctx ridu.HookContext) error {
				if ctx.Operation == operation.Read {
					joinedReadHooks++
					title, _ := ctx.Document.Values["title"].StringValue()
					ctx.Document.Values["title"] = store.String("read: " + title)
				}
				return nil
			}}},
		},
	}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	category, err := application.Local().Create(context.Background(), "categories", store.Values{"name": store.String("News")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for index, title := range []string{"First", "Second", "Third"} {
		if _, err := application.Local().Create(context.Background(), "posts", store.Values{
			"title": store.String(title), "category": store.String(category.ID),
			"visible": store.Boolean(index < 2), "secret": store.String("private:" + title),
		}, ridu.MutationOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	actor := &store.Document{ID: "editor"}
	found, err := application.Local().Find(context.Background(), "categories", category.ID, ridu.FindOptions{Actor: actor})
	if err != nil {
		t.Fatal(err)
	}
	displayName, _ := found.Values["displayName"].StringValue()
	if displayName != "Category: News" {
		t.Fatalf("displayName = %q", displayName)
	}
	joined, valid := found.Values["posts"].CopyList()
	if !valid || len(joined) != 2 {
		t.Fatalf("joined posts = %#v", joined)
	}
	if document, populated := joined[0].CopyDocument(); !populated || document.ID == "" {
		t.Fatalf("joined post = %#v", joined[0])
	} else {
		if _, leaked := document.Values["secret"]; leaked {
			t.Fatalf("joined target leaked field-read denied value: %#v", document.Values)
		}
		title, _ := document.Values["title"].StringValue()
		if !strings.HasPrefix(title, "read: ") {
			t.Fatalf("joined target skipped after-read hook: %#v", document.Values)
		}
	}
	if joinedReadHooks != 2 {
		t.Fatalf("joined target after-read hooks = %d, want 2", joinedReadHooks)
	}
	anonymous, err := application.Local().Find(context.Background(), "categories", category.ID, ridu.FindOptions{})
	if err != nil {
		t.Fatal(err)
	}
	anonymousJoined, _ := anonymous.Values["posts"].CopyList()
	if len(anonymousJoined) != 0 {
		t.Fatalf("inverse join bypassed target read access: %#v", anonymousJoined)
	}
}

func TestComputedRuntimeRequiresResolversAndExactValues(t *testing.T) {
	_, err := ridu.New(ridu.Config{Name: "Missing resolver", Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{field.Virtual("label", field.ValueString, nil)}}}}, teststore.New())
	if err == nil {
		t.Fatal("missing computed resolver succeeded")
	}

	application, err := ridu.New(ridu.Config{Name: "Bad resolver", Collections: []ridu.Collection{{
		Slug: "posts", Fields: field.Fields{field.Text("title"), field.Virtual("label", field.ValueString, func(operation.Context) (operation.Value[store.
			Value],

			error) {
			return operation.Present(store.Number(42)), nil
		})},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	_, err = application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("Hello")}, ridu.MutationOptions{})
	var operationError *ridu.OperationError
	if !errors.As(err, &operationError) || operationError.Code != "invalid_computed_value" || !strings.Contains(operationError.Message, `"label"`) {
		t.Fatalf("computed error = %#v", err)
	}
}

func TestGlobalsRejectInverseJoins(t *testing.T) {
	_, err := ridu.Resolve(ridu.Config{
		Name:        "Invalid global join",
		Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{field.Relationship("category", "posts")}}},
		Globals:     []ridu.Global{{Slug: "settings", Fields: field.Fields{field.Join("posts", "posts", "category")}}},
	})
	if err == nil {
		t.Fatal("global inverse join succeeded")
	}
}
