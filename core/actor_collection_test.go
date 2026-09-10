package core_test

import (
	"context"
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/store"
)

func TestLocalCRUDPreservesExactActorCollectionInAccessFieldsAndHooks(t *testing.T) {
	var accessCollections, hookCollections []string
	application, err := ridu.New(ridu.Config{Name: "Exact actor collection", Collections: []ridu.Collection{{
		Slug: "posts", Fields: field.Fields{field.Text("title"), field.Text("private").Access(field.Access{Read: func(ctx operation.AccessContext,

		) (bool, error) {
			return ctx.Actor.ID != "" && ctx.Actor.Collection ==
				"staff", nil
		}})},
		Access: ridu.CollectionAccess{Read: func(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
			accessCollections = append(accessCollections, string(ctx.ActorCollection))
			if ctx.Actor == nil || ctx.ActorCollection == "" {
				return ridu.Deny(), nil
			}
			return ridu.Allow(), nil
		}},

		Hooks: ridu.CollectionHooks{AfterRead: []ridu.Hook{func(ctx ridu.HookContext) error {
			hookCollections = append(hookCollections, string(ctx.ActorCollection))
			return nil
		}}},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	document, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("visible"), "private": store.String("staff only")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	accessCollections = nil
	hookCollections = nil
	sharedActor := &store.Document{ID: "same-id"}
	staff, err := application.Local().FindWithOptions(context.Background(), "posts", document.ID, ridu.FindOptions{Actor: sharedActor, ActorCollection: "staff"})
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := staff.Values["private"]; !exists {
		t.Fatalf("staff private field was redacted: %#v", staff.Values)
	}
	customer, err := application.Local().FindWithOptions(context.Background(), "posts", document.ID, ridu.FindOptions{Actor: sharedActor, ActorCollection: "customers"})
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := customer.Values["private"]; exists {
		t.Fatalf("customer with same actor ID read staff field: %#v", customer.Values)
	}
	if len(accessCollections) != 2 || accessCollections[0] != "staff" || accessCollections[1] != "customers" {
		t.Fatalf("access actor collections = %#v", accessCollections)
	}
	if len(hookCollections) != 2 || hookCollections[0] != "staff" || hookCollections[1] != "customers" {
		t.Fatalf("hook actor collections = %#v", hookCollections)
	}
}
