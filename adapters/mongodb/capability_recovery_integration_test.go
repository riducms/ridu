package mongodb

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/plugins/richtext"
	"github.com/riducms/ridu/store"
	outline "github.com/riducms/ridu/tests/contracts/embedded_plugin"
	richtextblocks "github.com/riducms/ridu/tests/contracts/richtext_blocks"
)

// Admin document loading requests both a document and its capabilities. A
// decoder recovery diagnostic must survive either request, including when the
// only unsupported payload is in the versions consulted by capabilities.
func TestMongoCapabilityReadsPreserveSchemaRecovery(t *testing.T) {
	for _, kind := range []string{"blocks", "outline", "richtext"} {
		for _, historical := range []bool{false, true} {
			name := kind + "/current"
			if historical {
				name = kind + "/historical"
			}
			t.Run(name, func(t *testing.T) {
				known := field.Block{Slug: "known", Fields: field.Fields{field.Text("title")}}
				retired := field.Block{Slug: "retired", Fields: field.Fields{field.Text("private")}}
				definition := func(types ...field.Block) field.Node {
					return field.Blocks("body", types...)
				}
				value := store.List(store.Object(store.Values{"blockType": store.String("retired"), "private": store.String("must-stay-private")}))
				empty := store.List()
				code, path := "unknown_block_schema", "body.0.blockType"
				config := ridu.Config{Name: "Capability recovery", Plugins: []ridu.Plugin{outline.Plugin{}, richtext.New()}}
				switch kind {
				case "outline":
					definition = func(types ...field.Block) field.Node { return outline.Field("body", types...) }
					value = outline.Value(outline.Widget("retired", "one", store.Values{"private": store.String("must-stay-private")}))
					empty = outline.Value()
					code, path = "unknown_embedded_schema", "body.outline.0.content.schema"
				case "richtext":
					definition = func(types ...field.Block) field.Node {
						return richtext.Field("body", richtext.Config{Blocks: types})
					}
					value = richtextblocks.Document(richtextblocks.Block("retired", "one", store.Values{"private": store.String("must-stay-private")}))
					empty = richtextblocks.Document()
					code, path = "unknown_embedded_schema", "body.root.children.0.fields.blockType"
				}
				config.Collections = []ridu.Collection{{Slug: "pages", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true}, Fields: field.Fields{definition(known, retired)}}}
				backend := mongoIntegrationStore(t)
				app, err := ridu.New(config, backend)
				if err != nil {
					t.Fatal(err)
				}
				if err := backend.SyncIndexes(t.Context(), app.Manifest()); err != nil {
					t.Fatal(err)
				}
				created, err := app.Local().Create(t.Context(), "pages", store.Values{"body": value}, nil)
				if err != nil {
					t.Fatal(err)
				}
				if historical {
					if _, err := app.Local().Update(t.Context(), "pages", created.ID, store.Values{"body": empty}, nil); err != nil {
						t.Fatal(err)
					}
				}
				config.Collections[0].Fields = field.Fields{definition(known)}
				current, err := ridu.New(config, backend)
				if err != nil {
					t.Fatal(err)
				}
				caps, err := current.Local().Capabilities(t.Context(), "pages", created.ID, ridu.CapabilityOptions{})
				var failure *ridu.OperationError
				if !errors.As(err, &failure) || failure.Code != "block_recovery_required" || failure.Status != 409 || len(failure.Issues) != 1 || failure.Issues[0].Code != code || failure.Issues[0].Path != path {
					t.Fatalf("capability recovery: %#v, %v", failure, err)
				}
				if len(caps.Fields) != 0 || caps.Operations.Update {
					t.Fatal("failed capability returned usable access data")
				}
				request := httptest.NewRequest(http.MethodPost, "/api/access/collections/pages", strings.NewReader(`{"id":"`+created.ID+`"}`))
				request.Header.Set("Content-Type", "application/json")
				response := httptest.NewRecorder()
				current.Handler(ridu.HandlerOptions{}).ServeHTTP(response, request)
				body := response.Body.String()
				if response.Code != 409 || !strings.Contains(body, code) || !strings.Contains(body, path) || strings.Contains(body, "must-stay-private") {
					t.Fatalf("HTTP recovery: %d %s", response.Code, body)
				}
			})
		}
	}
}
