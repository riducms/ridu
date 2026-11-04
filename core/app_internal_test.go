package core

import (
	"testing"

	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
)

func TestNewRegistersScheduledPublicationingOnlyForVersionedCollections(t *testing.T) {
	backend := teststore.New()
	withoutVersions, err := New(Config{
		Name: "Plain",
		Collections: []Collection{{
			Slug:   "posts",
			Fields: field.Fields{field.Text("title")},
		}},
	}, backend)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := withoutVersions.taskRegistry[builtinPublishTask]; exists {
		t.Fatal("plain application registered the scheduled-publish task")
	}

	withVersions, err := New(Config{
		Name: "Versioned",
		Collections: []Collection{{
			Slug:     "posts",
			Versions: true,
			Fields:   field.Fields{field.Text("title")},
		}},
	}, backend)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := withVersions.taskRegistry[builtinPublishTask]; !exists {
		t.Fatal("versioned application did not register the scheduled-publish task")
	}
}

func TestPluginTransportsAcceptAPIAndWellKnownPaths(t *testing.T) {
	for _, transport := range []struct {
		method, path string
		valid        bool
	}{
		{"POST", "/api/mcp", true},
		{"GET", "/api/graphql", true},
		{"GET", "/.well-known/oauth-authorization-server", true},
		{"GET", "/.well-known/oauth-protected-resource/api/mcp", true},
		{"POST", "/.well-known/oauth-authorization-server", false},
		{"GET", "/.well-known/", false},
		{"GET", "/.well-known//api", false},
		{"GET", "/.well-known/a%2Fb", false},
		{"GET", "/healthz", false},
		{"GET", "/admin/oauth", false},
		{"POST", "/api/auth/login", false},
		{"POST", "/api/plugins/oauth/token", false},
		{"GET", "/api/mcp/", false},
		{"GET", "/api/../admin", false},
		{"DELETE", "/api/mcp", false},
	} {
		if got := validPluginTransport(transport.method, transport.path); got != transport.valid {
			t.Errorf("validPluginTransport(%s %s) = %v, want %v", transport.method, transport.path, got, transport.valid)
		}
	}
}
