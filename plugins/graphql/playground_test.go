package graphql_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	graphqlplugin "github.com/riducms/ridu/plugins/graphql"
	riduschema "github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"golang.org/x/crypto/bcrypt"
)

func playgroundConfig(options graphqlplugin.Options) ridu.Config {
	return ridu.Config{
		Name: "GraphQL playground", Plugins: []ridu.Plugin{graphqlplugin.New(options)},
		Admin: ridu.AdminConfig{User: "users"},
		Collections: []ridu.Collection{
			{Slug: "users", Auth: true, AuthConfig: ridu.AuthConfig{Password: ridu.PasswordPolicy{BcryptCost: bcrypt.MinCost}}, Fields: field.Fields{
				field.Email("email").Required().Unique(),
			}},
			{Slug: "posts", Fields: field.Fields{field.Text("title")}},
		},
	}
}

func TestPlaygroundSchemaIsAvailableOnlyToAdminSessions(t *testing.T) {
	application, err := ridu.New(playgroundConfig(graphqlplugin.Options{Path: "/api/v2/graphql"}), teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	manifest := application.Manifest().Snapshot()
	index := slices.IndexFunc(manifest.Plugins, func(plugin riduschema.Plugin) bool { return plugin.Key == graphqlplugin.Key })
	if index < 0 || manifest.Plugins[index].Admin == nil || manifest.Plugins[index].Admin.Package != "@riducms/plugin-graphql" ||
		!slices.Equal(manifest.Plugins[index].Admin.Routes, []string{"graphql"}) {
		t.Fatalf("manifest plugin = %#v", manifest.Plugins)
	}
	loaders := manifest.Application.AdminLoaders
	if len(loaders) != 1 || loaders[0].Key != "graphql-playground" {
		t.Fatalf("admin loaders = %#v", loaders)
	}
	// The admin package declares this contract by hand; its build check compares it exactly.
	contract, err := json.Marshal(loaders[0])
	if err != nil {
		t.Fatal(err)
	}
	const wantContract = `{"key":"graphql-playground","input":{"kind":"object"},"output":{"kind":"object","fields":{"endpoint":{"kind":"string"},"schema":{"kind":"string"}}}}`
	if string(contract) != wantContract {
		t.Fatalf("loader contract changed; update packages/plugin-graphql:\n%s", contract)
	}

	handler := application.Handler(ridu.HandlerOptions{})
	load := func(token string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodGet, "/api/admin/loaders/graphql-playground?route=%2Fgraphql", nil)
		if token != "" {
			request.Header.Set("Authorization", "Session "+token)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	if response := load(""); response.Code != http.StatusForbidden {
		t.Fatalf("anonymous loader = %d %s", response.Code, response.Body.String())
	}

	data := authenticatedPlaygroundData(t, application, "admin@example.test")
	if data.Endpoint != "/api/v2/graphql" || !strings.Contains(data.Schema, "type Query") || !strings.Contains(data.Schema, "Post") {
		t.Fatalf("playground data = %#v", data)
	}
}

func TestDisabledPlaygroundLeavesNoAdminSurface(t *testing.T) {
	manifest, err := ridu.Resolve(playgroundConfig(graphqlplugin.Options{DisablePlayground: true}))
	if err != nil {
		t.Fatal(err)
	}
	snapshot := manifest.Snapshot()
	for _, plugin := range snapshot.Plugins {
		if plugin.Key == graphqlplugin.Key && plugin.Admin != nil {
			t.Fatalf("disabled playground still pairs an admin package: %#v", plugin.Admin)
		}
	}
	if len(snapshot.Application.AdminLoaders) != 0 {
		t.Fatalf("disabled playground registered loaders: %#v", snapshot.Application.AdminLoaders)
	}
}

func TestPlaygroundSchemaIsOwnedByEachApplication(t *testing.T) {
	shared := graphqlplugin.New()
	config := func(name string) ridu.Config {
		return ridu.Config{
			Name: name, Plugins: []ridu.Plugin{shared}, Admin: ridu.AdminConfig{User: "users"},
			Collections: []ridu.Collection{
				{Slug: "users", Auth: true, AuthConfig: ridu.AuthConfig{Password: ridu.PasswordPolicy{BcryptCost: bcrypt.MinCost}}, Fields: field.Fields{field.Email("email").Required().Unique()}},
				{Slug: riduschema.CollectionSlug(strings.ToLower(name)), Labels: ridu.CollectionLabels{Singular: name, Plural: name + "s"}, Fields: field.Fields{field.Text("title")}},
			},
		}
	}
	first, err := ridu.New(config("Alpha"), teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	second, err := ridu.New(config("Beta"), teststore.New())
	if err != nil {
		t.Fatal(err)
	}

	firstData := authenticatedPlaygroundData(t, first, "first@example.test")
	secondData := authenticatedPlaygroundData(t, second, "second@example.test")
	if !strings.Contains(firstData.Schema, "type Alpha ") || strings.Contains(firstData.Schema, "type Beta ") {
		t.Fatalf("first application playground schema was replaced:\n%s", firstData.Schema)
	}
	if !strings.Contains(secondData.Schema, "type Beta ") || strings.Contains(secondData.Schema, "type Alpha ") {
		t.Fatalf("second application playground schema =\n%s", secondData.Schema)
	}
}

type playgroundResponse struct {
	Endpoint string `json:"endpoint"`
	Schema   string `json:"schema"`
}

func authenticatedPlaygroundData(t *testing.T, application *ridu.App, email string) playgroundResponse {
	t.Helper()
	admin, err := application.Local().Create(t.Context(), "users", store.Values{"email": store.String(email)}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	const password = "correct horse battery staple"
	if err := application.SetPassword(t.Context(), "users", admin.ID, password); err != nil {
		t.Fatal(err)
	}
	session, err := application.Login(t.Context(), "users", email, password)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/admin/loaders/graphql-playground?route=%2Fgraphql", nil)
	request.Header.Set("Authorization", "Session "+session.Token)
	response := httptest.NewRecorder()
	application.Handler(ridu.HandlerOptions{}).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("admin loader = %d %s", response.Code, response.Body.String())
	}
	var data playgroundResponse
	if err := json.Unmarshal(response.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	return data
}
