package core_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/protocol"
	"github.com/riducms/ridu/store"
)

func TestRESTPreferencesAndLocksUseExactSameIDAuthCollection(t *testing.T) {
	application, err := ridu.New(ridu.Config{
		Name: "Exact HTTP identity", Admin: ridu.AdminConfig{User: "users"},
		Collections: []ridu.Collection{
			{Slug: "users", Auth: true, Fields: []field.Definition{field.Text("email", field.Required(), field.Unique())}},
			{Slug: "staff", Auth: true, Fields: []field.Definition{field.Text("email", field.Required(), field.Unique())}},
			{Slug: "posts", LockDocuments: true, Fields: []field.Definition{field.Text("title")}},
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
	if err := application.SetPassword(ctx, "users", user.ID, "users-password-value"); err != nil {
		t.Fatal(err)
	}
	if err := application.SetPassword(ctx, "staff", staff.ID, "staff-password-value"); err != nil {
		t.Fatal(err)
	}
	userSession, err := application.Login(ctx, "users", "user@example.test", "users-password-value")
	if err != nil {
		t.Fatal(err)
	}
	staffSession, err := application.Login(ctx, "staff", "staff@example.test", "staff-password-value")
	if err != nil {
		t.Fatal(err)
	}
	post, err := application.Local().Create(ctx, "posts", store.Values{"title": store.String("Locked")}, &user)
	if err != nil {
		t.Fatal(err)
	}
	client := handlerClient(application.Handler(ridu.HandlerOptions{}))
	userCookie := "ridu_session=" + userSession.Token
	staffCookie := "ridu_session=" + staffSession.Token

	response := requestJSON(t, client, http.MethodPut, "http://ridu.test/api/preferences/theme", strings.NewReader(`{"value":"user-theme"}`), userCookie)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("users preference write = %d: %s", response.StatusCode, readBody(t, response))
	}
	response = requestJSON(t, client, http.MethodGet, "http://ridu.test/api/preferences/theme", nil, staffCookie)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("staff preference read = %d: %s", response.StatusCode, readBody(t, response))
	}
	var preference protocol.PreferenceEnvelope[json.RawMessage]
	decodeResponse(t, response, &preference)
	if string(preference.Value) != "null" {
		t.Fatalf("staff read of users preference = %s", preference.Value)
	}
	response = requestJSON(t, client, http.MethodPut, "http://ridu.test/api/preferences/theme", strings.NewReader(`{"value":"staff-theme"}`), staffCookie)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("staff preference write = %d: %s", response.StatusCode, readBody(t, response))
	}
	response = requestJSON(t, client, http.MethodGet, "http://ridu.test/api/preferences/theme", nil, userCookie)
	decodeResponse(t, response, &preference)
	if string(preference.Value) != `"user-theme"` {
		t.Fatalf("users preference after staff write = %s", preference.Value)
	}

	lockURL := "http://ridu.test/api/collections/posts/" + post.ID + "/lock"
	response = requestJSON(t, client, http.MethodPost, lockURL, strings.NewReader(`{}`), userCookie)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("users lock acquire = %d: %s", response.StatusCode, readBody(t, response))
	}
	var lock protocol.DocumentLockEnvelope
	decodeResponse(t, response, &lock)
	if !lock.Acquired || !lock.Owned || lock.Lock == nil || lock.Lock.OwnerLabel != "user@example.test" {
		t.Fatalf("users lock = %#v", lock)
	}
	response = requestJSON(t, client, http.MethodDelete, lockURL, nil, staffCookie)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("staff release of users lock = %d: %s", response.StatusCode, readBody(t, response))
	}
	response = requestJSON(t, client, http.MethodGet, lockURL, nil, userCookie)
	decodeResponse(t, response, &lock)
	if !lock.Owned || lock.Lock == nil || lock.Lock.OwnerLabel != "user@example.test" {
		t.Fatalf("users lock after staff release = %#v", lock)
	}
	response = requestJSON(t, client, http.MethodPost, lockURL, strings.NewReader(`{"takeover":true}`), staffCookie)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("staff lock takeover = %d: %s", response.StatusCode, readBody(t, response))
	}
	decodeResponse(t, response, &lock)
	if !lock.Acquired || !lock.Owned || lock.Lock == nil || lock.Lock.OwnerLabel != "staff@example.test" {
		t.Fatalf("staff lock = %#v", lock)
	}
	response = requestJSON(t, client, http.MethodDelete, lockURL, nil, userCookie)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("users release of staff lock = %d: %s", response.StatusCode, readBody(t, response))
	}
	response = requestJSON(t, client, http.MethodGet, lockURL, nil, staffCookie)
	decodeResponse(t, response, &lock)
	if !lock.Owned || lock.Lock == nil || lock.Lock.OwnerLabel != "staff@example.test" {
		t.Fatalf("staff lock after users release = %#v", lock)
	}
}
