package core_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	operationengine "github.com/riducms/ridu/internal/operation"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/store"
)

func TestPreferencesArePersistentAndScopedToAdminUsers(t *testing.T) {
	application, err := ridu.New(ridu.Config{
		Name:  "Preferences",
		Admin: ridu.AdminConfig{User: "users"},
		Collections: []ridu.Collection{{
			Slug: "users", Auth: true,
			Fields: []field.Definition{field.Text("email", field.Required(), field.Unique())},
		}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	firstDocument, err := application.CreateAuthUser(ctx, "users", store.Values{"email": store.String("first@example.test")}, "first-password-value", nil)
	if err != nil {
		t.Fatal(err)
	}
	secondDocument, err := application.CreateAuthUser(ctx, "users", store.Values{"email": store.String("second@example.test")}, "second-password-value", nil)
	if err != nil {
		t.Fatal(err)
	}
	first := &ridu.AuthIdentity{Collection: "users", Actor: firstDocument}
	second := &ridu.AuthIdentity{Collection: "users", Actor: secondDocument}
	want := json.RawMessage(`[{"name":"Editorial","view":"hierarchy"}]`)

	got, err := application.SetPreference(ctx, first, "collection:posts:presets", want)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("SetPreference() = %s, want %s", got, want)
	}
	got, err = application.Preference(ctx, first, "collection:posts:presets")
	if err != nil || string(got) != string(want) {
		t.Fatalf("Preference() = %s, %v", got, err)
	}
	if got, err := application.Preference(ctx, second, "collection:posts:presets"); err != nil || string(got) != "null" {
		t.Fatalf("second user's Preference() = %s, %v", got, err)
	}
	if _, err := application.SetPreference(ctx, first, "theme", json.RawMessage(`"dark"`)); err != nil {
		t.Fatal(err)
	}
	if err := application.ResetPreferences(ctx, first); err != nil {
		t.Fatal(err)
	}
	if got, err := application.Preference(ctx, first, "theme"); err != nil || string(got) != "null" {
		t.Fatalf("reset Preference() = %s, %v", got, err)
	}
	if _, err := application.SetPreference(ctx, first, "collection:posts:presets", want); err != nil {
		t.Fatal(err)
	}
	if err := application.DeletePreference(ctx, first, "collection:posts:presets"); err != nil {
		t.Fatal(err)
	}
	if got, err := application.Preference(ctx, first, "collection:posts:presets"); err != nil || string(got) != "null" {
		t.Fatalf("deleted Preference() = %s, %v", got, err)
	}
}

func TestPreferencesRejectAnonymousInvalidKeysAndInvalidJSON(t *testing.T) {
	application, err := ridu.New(ridu.Config{
		Name:        "Preferences",
		Admin:       ridu.AdminConfig{User: "users"},
		Collections: []ridu.Collection{{Slug: "users", Auth: true, Fields: []field.Definition{field.Text("email", field.Required(), field.Unique())}}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	actor := &ridu.AuthIdentity{Collection: "users", Actor: store.Document{ID: "user_1"}}
	if _, err := application.Preference(context.Background(), nil, "theme"); errorCode(err) != "access_denied" {
		t.Fatalf("anonymous Preference() error = %v", err)
	}
	if _, err := application.SetPreference(context.Background(), actor, "Bad key", json.RawMessage(`true`)); errorCode(err) != "bad_request" {
		t.Fatalf("invalid key SetPreference() error = %v", err)
	}
	if _, err := application.SetPreference(context.Background(), actor, "theme", json.RawMessage(`{`)); errorCode(err) != "bad_request" {
		t.Fatalf("invalid JSON SetPreference() error = %v", err)
	}
}

func TestPreferencesUseExactAuthCollectionForSameIDActors(t *testing.T) {
	application, err := ridu.New(ridu.Config{
		Name: "Exact preference identity", Admin: ridu.AdminConfig{User: "users"},
		Collections: []ridu.Collection{
			{Slug: "users", Auth: true, Fields: []field.Definition{field.Text("email", field.Required(), field.Unique())}},
			{Slug: "staff", Auth: true, Fields: []field.Definition{field.Text("email", field.Required(), field.Unique())}},
		},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	user, err := application.Local().Import(ctx, "users", store.Values{"email": store.String("user@example.test")}, ridu.ImportOptions{ID: "shared-actor", Status: store.StatusPublished}, nil)
	if err != nil {
		t.Fatal(err)
	}
	staff, err := application.Local().Import(ctx, "staff", store.Values{"email": store.String("staff@example.test")}, ridu.ImportOptions{ID: user.ID, Status: store.StatusPublished}, nil)
	if err != nil {
		t.Fatal(err)
	}
	userIdentity := &ridu.AuthIdentity{Collection: "users", Actor: user}
	staffIdentity := &ridu.AuthIdentity{Collection: "staff", Actor: staff}

	if _, err := application.SetPreference(ctx, userIdentity, "theme", json.RawMessage(`"user-theme"`)); err != nil {
		t.Fatal(err)
	}
	if got, err := application.Preference(ctx, staffIdentity, "theme"); err != nil || string(got) != "null" {
		t.Fatalf("staff read of users preference = %s, %v", got, err)
	}
	if _, err := application.SetPreference(ctx, staffIdentity, "theme", json.RawMessage(`"staff-theme"`)); err != nil {
		t.Fatal(err)
	}
	if got, err := application.Preference(ctx, userIdentity, "theme"); err != nil || string(got) != `"user-theme"` {
		t.Fatalf("users preference after staff write = %s, %v", got, err)
	}
	if err := application.DeletePreference(ctx, staffIdentity, "theme"); err != nil {
		t.Fatal(err)
	}
	if got, err := application.Preference(ctx, userIdentity, "theme"); err != nil || string(got) != `"user-theme"` {
		t.Fatalf("users preference after staff delete = %s, %v", got, err)
	}
	if _, err := application.SetPreference(ctx, staffIdentity, "layout", json.RawMessage(`"compact"`)); err != nil {
		t.Fatal(err)
	}
	if err := application.ResetPreferences(ctx, staffIdentity); err != nil {
		t.Fatal(err)
	}
	if got, err := application.Preference(ctx, userIdentity, "theme"); err != nil || string(got) != `"user-theme"` {
		t.Fatalf("users preference after staff reset = %s, %v", got, err)
	}

}

func errorCode(err error) string {
	var operationError *operationengine.Error
	if errors.As(err, &operationError) {
		return operationError.Code
	}
	return ""
}
