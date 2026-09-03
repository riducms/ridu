package schema_test

import (
	"testing"

	"github.com/riducms/ridu/schema"
)

func TestEndpointPathContract(t *testing.T) {
	for _, valid := range []string{"/", "/status", "/:id/tracking", "/orders/:order_id/events/:event2"} {
		if !schema.IsValidEndpointPath(valid) {
			t.Errorf("IsValidEndpointPath(%q) = false", valid)
		}
	}
	for _, invalid := range []string{"", "status", "/status/", "//status", "/./status", "/../status", "/post-:id", "/:id/:id", "/:1id", "/{id}", "/encoded%20path", "/status?draft=true", "/status#fragment", "/status\\child", "/has space"} {
		if schema.IsValidEndpointPath(invalid) {
			t.Errorf("IsValidEndpointPath(%q) = true", invalid)
		}
	}
	for _, method := range []string{"connect", "DELETE", " get ", "HEAD", "options", "PATCH", "POST", "put"} {
		if !schema.IsSupportedEndpointMethod(method) {
			t.Errorf("IsSupportedEndpointMethod(%q) = false", method)
		}
	}
	for _, method := range []string{"", "TRACE", "COPY"} {
		if schema.IsSupportedEndpointMethod(method) {
			t.Errorf("IsSupportedEndpointMethod(%q) = true", method)
		}
	}
}

func TestManifestParseRejectsInvalidEndpointMetadata(t *testing.T) {
	for _, endpoints := range []string{
		`[{"method":"TRACE","path":"/status"}]`,
		`[{"method":"GET","path":"/{id}"}]`,
		`[{"method":"GET","path":"/:id"},{"method":"GET","path":"/:slug"}]`,
		`[{"method":"GET","path":"/:id"},{"method":"POST","path":"/:slug"}]`,
		`[{"method":"GET","path":"/collections/posts/count"}]`,
		`[{"method":"GET","path":"/:namespace/:slug/:action"}]`,
	} {
		encoded := []byte(`{"version":1,"application":{"name":"Example","endpoints":` + endpoints + `},"collections":[],"plugins":[]}`)
		if _, err := schema.Parse(encoded); err == nil {
			t.Fatalf("Parse invalid endpoint metadata succeeded: %s", encoded)
		}
	}

	for name, snapshot := range map[string]schema.Snapshot{
		"collection": {
			Version: schema.CurrentVersion, Application: schema.Application{Name: "Example"}, Plugins: []schema.Plugin{},
			Collections: []schema.Collection{{
				ID: "posts", Slug: "posts", Labels: schema.CollectionLabels{Singular: "Post", Plural: "Posts"},
				Fields: []schema.Field{}, Endpoints: []schema.Endpoint{{Method: "GET", Path: "/:id"}, {Method: "GET", Path: "/:slug"}},
			}},
		},
		"global": {
			Version: schema.CurrentVersion, Application: schema.Application{Name: "Example"}, Collections: []schema.Collection{}, Plugins: []schema.Plugin{},
			Globals: []schema.Global{{
				ID: "global-settings", Slug: "settings", Labels: schema.CollectionLabels{Singular: "Settings", Plural: "Settings"}, Capabilities: schema.Capabilities{Global: true},
				Fields: []schema.Field{}, Endpoints: []schema.Endpoint{{Method: "GET", Path: "/:id"}, {Method: "POST", Path: "/:slug"}},
			}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			encoded, err := schema.NewManifest(snapshot).Bytes()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := schema.Parse(encoded); err == nil {
				t.Fatalf("Parse invalid %s endpoint metadata succeeded: %s", name, encoded)
			}
		})
	}
}
