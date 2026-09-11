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
		Slug: "posts", Fields: field.Fields{field.Text("title"), field.Text("private").Access(field.Access{Read: func(ctx operation.Context) (bool, error) {
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
	document, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("visible"), "private": store.String("staff only")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	accessCollections = nil
	hookCollections = nil
	sharedActor := &store.Document{ID: "same-id"}
	staff, err := application.Local().Find(context.Background(), "posts", document.ID, ridu.FindOptions{Actor: sharedActor, ActorCollection: "staff"})
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := staff.Values["private"]; !exists {
		t.Fatalf("staff private field was redacted: %#v", staff.Values)
	}
	customer, err := application.Local().Find(context.Background(), "posts", document.ID, ridu.FindOptions{Actor: sharedActor, ActorCollection: "customers"})
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

func TestMutationOptionsForwardIdentityRevisionAndBulkLocales(t *testing.T) {
	staffOnly := func(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
		if ctx.Actor != nil && ctx.ActorCollection == "staff" {
			return ridu.Allow(), nil
		}
		return ridu.Deny(), nil
	}
	app, err := ridu.New(ridu.Config{Name: "Mutation options", Localization: ridu.LocalizationConfig{
		DefaultLocale: "en", Locales: []ridu.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}},
	}, Collections: []ridu.Collection{{Slug: "posts", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true}, Trash: true,
		Fields: field.Fields{field.Text("title").Localized()},
		Access: ridu.CollectionAccess{Create: staffOnly, Read: staffOnly, Update: staffOnly, Delete: staffOnly},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	type input struct {
		Title string `json:"title"`
	}
	type document struct {
		ID       string `json:"id"`
		Title    string `json:"title"`
		Revision int    `json:"_revision"`
	}
	actor := &store.Document{ID: "shared-id"}
	posts := ridu.NewTypedCollection[document, input, input]("posts").With(app.Local())
	options := ridu.TypedMutationOptions{Actor: actor, ActorCollection: "staff", Locale: "en"}
	created, err := posts.Create(t.Context(), input{"Hello"}, options)
	if err != nil {
		t.Fatal(err)
	}
	options.ExpectedRevision = created.Revision
	options.Locale = "fr"
	updated, err := posts.Update(t.Context(), created.ID, input{"Bonjour"}, options)
	if err != nil || updated.Title != "Bonjour" {
		t.Fatalf("typed localized write: %#v %v", updated, err)
	}
	if _, err := posts.Update(t.Context(), created.ID, input{"stale"}, options); !operationCode(err, "conflict") {
		t.Fatalf("typed revision lost: %v", err)
	}
	bulk := ridu.BulkOptions{Actor: actor, ActorCollection: "staff", Locale: "fr"}
	changed, err := app.Local().BulkUpdate(t.Context(), "posts", []string{created.ID}, store.Values{"title": store.String("Salut")}, bulk)
	if err != nil || len(changed) != 1 {
		t.Fatalf("bulk identity: %#v %v", changed, err)
	}
	english, err := posts.Find(t.Context(), created.ID, ridu.TypedReadOptions{Actor: actor, ActorCollection: "staff", Locale: "en"})
	if err != nil || english.Title != "Hello" {
		t.Fatalf("bulk locale leaked: %#v %v", english, err)
	}
	bulk.ActorCollection = "customers"
	if _, err := app.Local().BulkDelete(t.Context(), "posts", []string{created.ID}, bulk); !operationCode(err, "access_denied") {
		t.Fatalf("wrong identity accepted: %v", err)
	}
	bulk.ActorCollection = "staff"
	if _, err := app.Local().BulkDelete(t.Context(), "posts", []string{created.ID}, bulk); err != nil {
		t.Fatal(err)
	}
	if deleted, err := app.Local().EmptyTrash(t.Context(), "posts", bulk); err != nil || len(deleted) != 1 {
		t.Fatalf("empty trash identity: %#v %v", deleted, err)
	}
	if _, err := posts.Import(t.Context(), input{"Imported"}, ridu.ImportOptions{ID: "imported", Status: store.StatusDraft, Actor: actor, ActorCollection: "staff"}); err != nil {
		t.Fatal(err)
	}
}
