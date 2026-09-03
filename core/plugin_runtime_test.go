package core_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/store"
)

type runtimePlugin struct{}

func (runtimePlugin) Key() string { return "runtime" }

func (runtimePlugin) Descriptor() ridu.PluginDescriptor {
	return ridu.PluginDescriptor{Version: "1.0.0", GoPackage: "example.com/plugins/runtime", APIVersion: ridu.PluginAPIVersion, Ridu: ridu.RiduCompatibility{Minimum: ridu.FrameworkVersion, MaximumExclusive: "0.2.0"}}
}

func (runtimePlugin) Hooks() []ridu.PluginHookContribution {
	return []ridu.PluginHookContribution{{Collection: "posts", Hooks: ridu.CollectionHooks{BeforeValidate: []ridu.Hook{func(ctx ridu.HookContext) error {
		ctx.Data["fromPlugin"] = store.String("yes")
		return nil
	}}}}}
}

func (runtimePlugin) Endpoints() []ridu.PluginEndpoint {
	return []ridu.PluginEndpoint{{Method: http.MethodGet, Path: "status", Summary: "Plugin status", Handler: func(ctx ridu.PluginEndpointContext) {
		ctx.Writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(ctx.Writer).Encode(map[string]bool{"ok": ctx.Local != nil})
	}}}
}

func TestPluginHooksAndNamespacedEndpointsUsePublicRuntimeContracts(t *testing.T) {
	application, err := ridu.New(ridu.Config{
		Name: "Plugin runtime", Plugins: []ridu.Plugin{runtimePlugin{}},
		Collections: []ridu.Collection{{Slug: "posts", Fields: []field.Definition{field.Text("fromPlugin", field.Required())}}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	created, err := application.Local().Create(t.Context(), "posts", store.Values{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if value, _ := created.Values["fromPlugin"].StringValue(); value != "yes" {
		t.Fatalf("plugin hook value = %q", value)
	}
	client := handlerClient(application.Handler(ridu.HandlerOptions{}))
	response := requestJSON(t, client, http.MethodGet, "http://ridu.test/api/plugins/runtime/status", nil, "")
	if response.StatusCode != http.StatusOK || !responseBodyOK(t, response) {
		t.Fatalf("plugin endpoint status = %d", response.StatusCode)
	}
	methodResponse := requestJSON(t, client, http.MethodPost, "http://ridu.test/api/plugins/runtime/status", nil, "")
	if methodResponse.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("plugin endpoint method status = %d", methodResponse.StatusCode)
	}
	plugin := application.Manifest().Snapshot().Plugins[0]
	if len(plugin.Endpoints) != 1 || plugin.Endpoints[0].Path != "status" {
		t.Fatalf("manifest plugin endpoints = %#v", plugin.Endpoints)
	}
}

func responseBodyOK(t *testing.T, response *http.Response) bool {
	t.Helper()
	var body map[string]bool
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return body["ok"]
}

var _ ridu.HookProvider = runtimePlugin{}
var _ ridu.EndpointProvider = runtimePlugin{}

type transportPlugin struct {
	boundManifestName string
}

func (plugin *transportPlugin) Key() string { return "transport" }

func (plugin *transportPlugin) Descriptor() ridu.PluginDescriptor {
	return ridu.PluginDescriptor{Version: "1.0.0", GoPackage: "example.com/plugins/transport", APIVersion: ridu.PluginAPIVersion, Ridu: ridu.RiduCompatibility{Minimum: ridu.FrameworkVersion, MaximumExclusive: "0.2.0"}}
}

func (plugin *transportPlugin) BindTransports(ctx ridu.PluginTransportContext) ([]ridu.PluginTransport, error) {
	plugin.boundManifestName = ctx.Manifest.Snapshot().Application.Name
	return []ridu.PluginTransport{{Method: http.MethodPost, Path: "/api/example-protocol", Summary: "Example protocol", Handler: func(endpoint ridu.PluginEndpointContext) {
		endpoint.Writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(endpoint.Writer).Encode(map[string]bool{"ok": endpoint.Local == ctx.Local})
	}}}, nil
}

func TestPluginTransportBindsAtStartupAndUsesExactPublicPath(t *testing.T) {
	plugin := &transportPlugin{}
	application, err := ridu.New(ridu.Config{
		Name: "Transport runtime", Plugins: []ridu.Plugin{plugin},
		Collections: []ridu.Collection{{Slug: "posts", Fields: []field.Definition{field.Text("title")}}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	if plugin.boundManifestName != "Transport runtime" {
		t.Fatalf("bound manifest name = %q", plugin.boundManifestName)
	}
	client := handlerClient(application.Handler(ridu.HandlerOptions{}))
	response := requestJSON(t, client, http.MethodPost, "http://ridu.test/api/example-protocol", nil, "")
	if response.StatusCode != http.StatusOK || !responseBodyOK(t, response) {
		t.Fatalf("transport status = %d", response.StatusCode)
	}
	methodResponse := requestJSON(t, client, http.MethodGet, "http://ridu.test/api/example-protocol", nil, "")
	if methodResponse.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("transport method status = %d", methodResponse.StatusCode)
	}
}

var _ ridu.TransportProvider = (*transportPlugin)(nil)

type invalidHookPlugin struct{ runtimePlugin }

func (invalidHookPlugin) Key() string { return "invalid-hook" }
func (invalidHookPlugin) Hooks() []ridu.PluginHookContribution {
	return []ridu.PluginHookContribution{{Collection: "posts", FieldPath: "missing", Hooks: ridu.CollectionHooks{BeforeValidate: []ridu.Hook{func(ridu.HookContext) error { return nil }}}}}
}

func TestPluginFieldHooksMustTargetAResolvedField(t *testing.T) {
	_, err := ridu.New(ridu.Config{Name: "Invalid hook", Plugins: []ridu.Plugin{invalidHookPlugin{}}, Collections: []ridu.Collection{{Slug: "posts", Fields: []field.Definition{field.Text("title")}}}}, teststore.New())
	if err == nil || !strings.Contains(err.Error(), "unknown_plugin_hook_field") {
		t.Fatalf("New error = %v", err)
	}
}
