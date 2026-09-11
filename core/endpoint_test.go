package core_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"golang.org/x/crypto/bcrypt"
)

func TestCustomEndpointConfigResolvesEveryScopeAndRejectsUnsafeDefinitions(t *testing.T) {
	handler := func(ridu.EndpointContext) {}
	config := ridu.Config{
		Name:      "Custom endpoints",
		Endpoints: []ridu.Endpoint{{Method: " get ", Path: "/status/:name", Summary: " Status ", Handler: handler}},
		Collections: []ridu.Collection{{
			Slug: "posts", Fields: field.Fields{field.Text("title")},
			Endpoints: []ridu.Endpoint{{Method: "POST", Path: "/:id/tracking", Handler: handler}},
		}},
		Globals: []ridu.Global{{
			Slug: "site-settings", Fields: field.Fields{field.Text("title")},
			Endpoints: []ridu.Endpoint{{Method: "PUT", Path: "/refresh", Handler: handler}},
		}},
	}
	manifest, err := ridu.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := manifest.Snapshot()
	if got := snapshot.Application.Endpoints; len(got) != 1 || got[0].Method != http.MethodGet || got[0].Path != "/status/:name" || got[0].Summary != "Status" {
		t.Fatalf("root endpoints = %#v", got)
	}
	if got := snapshot.Collections[0].Endpoints; len(got) != 1 || got[0].Path != "/:id/tracking" {
		t.Fatalf("collection endpoints = %#v", got)
	}
	if got := snapshot.Globals[0].Endpoints; len(got) != 1 || got[0].Method != http.MethodPut {
		t.Fatalf("global endpoints = %#v", got)
	}
	config.Endpoints[0].Path = "/mutated"
	if manifest.Snapshot().Application.Endpoints[0].Path != "/status/:name" {
		t.Fatal("manifest retained caller-owned endpoint metadata")
	}

	tests := []struct {
		name      string
		endpoints []ridu.Endpoint
		code      string
		path      string
	}{
		{"missing handler", []ridu.Endpoint{{Method: "GET", Path: "/status"}}, "missing_endpoint_handler", "endpoints[0].handler"},
		{"invalid method", []ridu.Endpoint{{Method: "TRACE", Path: "/status", Handler: handler}}, "invalid_endpoint_method", "endpoints[0].method"},
		{"traversal", []ridu.Endpoint{{Method: "GET", Path: "/../secret", Handler: handler}}, "invalid_endpoint_path", "endpoints[0].path"},
		{"partial param", []ridu.Endpoint{{Method: "GET", Path: "/post-:id", Handler: handler}}, "invalid_endpoint_path", "endpoints[0].path"},
		{"openapi braces", []ridu.Endpoint{{Method: "GET", Path: "/{id}", Handler: handler}}, "invalid_endpoint_path", "endpoints[0].path"},
		{"duplicate", []ridu.Endpoint{{Method: "get", Path: "/status", Handler: handler}, {Method: "GET", Path: "/status", Handler: handler}}, "duplicate_endpoint", "endpoints[1]"},
		{"parameter alias duplicate", []ridu.Endpoint{{Method: "GET", Path: "/:id", Handler: handler}, {Method: "GET", Path: "/:slug", Handler: handler}}, "duplicate_endpoint", "endpoints[1]"},
		{"parameter names conflict across methods", []ridu.Endpoint{{Method: "GET", Path: "/:id", Handler: handler}, {Method: "POST", Path: "/:slug", Handler: handler}}, "conflicting_endpoint_parameters", "endpoints[1].path"},
		{"reserved resource namespace", []ridu.Endpoint{{Method: "GET", Path: "/collections/posts/count", Handler: handler}}, "reserved_endpoint_namespace", "endpoints[0].path"},
		{"parameterized root namespace", []ridu.Endpoint{{Method: "GET", Path: "/:namespace/:slug/:action", Handler: handler}}, "reserved_endpoint_namespace", "endpoints[0].path"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ridu.Resolve(ridu.Config{Name: "Invalid endpoint", Endpoints: test.endpoints, Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{field.Text("title")}}}})
			var validation *schema.ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("Resolve error = %T %v", err, err)
			}
			for _, issue := range validation.Issues {
				if issue.Code == test.code && issue.Path == test.path {
					return
				}
			}
			t.Fatalf("issues = %#v, want %s at %s", validation.Issues, test.code, test.path)
		})
	}
}

func TestCustomEndpointsRouteByScopeWithParamsActorAndBuiltInPrecedence(t *testing.T) {
	var collectionContext ridu.EndpointContext
	application, err := ridu.New(ridu.Config{
		Name: "Endpoint runtime", Admin: ridu.AdminConfig{User: "users"},
		Endpoints: []ridu.Endpoint{
			{Method: http.MethodGet, Path: "/schema", Handler: jsonEndpoint(map[string]string{"source": "root"})},
		},
		Collections: []ridu.Collection{
			{Slug: "users", Auth: true, AuthConfig: ridu.AuthConfig{Password: ridu.PasswordPolicy{BcryptCost: bcrypt.MinCost}}, Fields: field.Fields{field.Text("email").Required().Unique()}},
			{
				Slug: "posts", Fields: field.Fields{field.Text("title").Required()},
				Endpoints: []ridu.Endpoint{
					{Method: http.MethodGet, Path: "/count", Handler: jsonEndpoint(map[string]string{"source": "collection-override"})},
					{Method: http.MethodGet, Path: "/:id/tracking", Handler: func(ctx ridu.EndpointContext) {
						collectionContext = ctx
						jsonResponse(ctx.Writer, map[string]string{"id": ctx.RouteParams["id"], "source": "collection"})
					}},
				},
			},
		},
		Globals: []ridu.Global{{
			Slug: "site-settings", Fields: field.Fields{field.Text("title")},
			Endpoints: []ridu.Endpoint{{Method: http.MethodGet, Path: "/:locale/preview", Handler: func(ctx ridu.EndpointContext) {
				jsonResponse(ctx.Writer, map[string]string{"global": string(ctx.Global), "locale": ctx.RouteParams["locale"]})
			}}},
		}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	user, err := application.Local().Create(context.Background(), "users", store.Values{"email": store.String("endpoint@example.test")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := application.SetPassword(context.Background(), "users", user.ID, "correct-horse"); err != nil {
		t.Fatal(err)
	}
	client := handlerClient(application.Handler(ridu.HandlerOptions{}))
	login := requestJSON(t, client, http.MethodPost, "http://ridu.test/api/auth/users/login", strings.NewReader(`{"email":"endpoint@example.test","password":"correct-horse"}`), "")
	if login.StatusCode != http.StatusOK {
		t.Fatalf("login = %d: %s", login.StatusCode, readBody(t, login))
	}
	cookie := login.Cookies()[0].String()
	login.Body.Close()

	root := requestJSON(t, client, http.MethodGet, "http://ridu.test/api/schema", nil, "")
	assertJSONField(t, root, "source", "root")
	override := requestJSON(t, client, http.MethodGet, "http://ridu.test/api/collections/posts/count", nil, "")
	assertJSONField(t, override, "source", "collection-override")
	overrideWithSlash := requestJSON(t, client, http.MethodGet, "http://ridu.test/api/collections/posts/count/", nil, "")
	assertJSONField(t, overrideWithSlash, "source", "collection-override")
	tracking := requestJSON(t, client, http.MethodGet, "http://ridu.test/api/collections/posts/order%20one/tracking", nil, cookie)
	assertJSONField(t, tracking, "id", "order one")
	if collectionContext.Request.PathValue("id") != "order one" || collectionContext.Collection != "posts" || collectionContext.Global != "" || collectionContext.Actor == nil || collectionContext.Actor.ID != user.ID || collectionContext.ActorCollection != "users" || collectionContext.Local == nil || collectionContext.RequestID == "" {
		t.Fatalf("collection endpoint context = %#v", collectionContext)
	}
	global := requestJSON(t, client, http.MethodGet, "http://ridu.test/api/globals/site-settings/fr/preview", nil, "")
	assertJSONField(t, global, "global", "site-settings")

	encodedSlash := requestJSON(t, client, http.MethodGet, "http://ridu.test/api/collections/posts/order%2Fone/tracking", nil, cookie)
	if encodedSlash.StatusCode == http.StatusOK {
		t.Fatalf("encoded slash matched a named segment: %s", readBody(t, encodedSlash))
	}
	emptyParam := requestJSON(t, client, http.MethodGet, "http://ridu.test/api/collections/posts//tracking", nil, cookie)
	if emptyParam.StatusCode == http.StatusOK {
		t.Fatalf("empty segment matched a named parameter: %s", readBody(t, emptyParam))
	}
}

func TestCustomEndpointBodiesAreBoundedAndPanicsAreRedacted(t *testing.T) {
	var diagnostic ridu.RequestErrorEvent
	var streamObservation ridu.RequestObservation
	application, err := ridu.New(ridu.Config{
		Name:        "Endpoint safety",
		Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{field.Text("title")}}},
		Endpoints: []ridu.Endpoint{
			{Method: http.MethodPost, Path: "/read", MaxBodyBytes: 4, Handler: func(ctx ridu.EndpointContext) {
				_, readError := io.ReadAll(ctx.Request.Body)
				var maximum *http.MaxBytesError
				if errors.As(readError, &maximum) {
					ctx.Writer.WriteHeader(http.StatusRequestEntityTooLarge)
				}
			}},
			{Method: http.MethodGet, Path: "/panic", Handler: func(ridu.EndpointContext) { panic("private endpoint secret") }},
			{Method: http.MethodGet, Path: "/stream-panic", Handler: func(ctx ridu.EndpointContext) {
				if err := http.NewResponseController(ctx.Writer).Flush(); err != nil {
					panic(err)
				}
				panic("private stream secret")
			}},
		},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	client := handlerClient(application.Handler(ridu.HandlerOptions{
		RequestError: func(event ridu.RequestErrorEvent) { diagnostic = event },
		Observe: func(observation ridu.RequestObservation) {
			if observation.Path == "/api/stream-panic" {
				streamObservation = observation
			}
		},
	}))
	large := requestJSON(t, client, http.MethodPost, "http://ridu.test/api/read", strings.NewReader("12345"), "")
	if large.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("large body status = %d", large.StatusCode)
	}
	large.Body.Close()
	panicked := requestJSON(t, client, http.MethodGet, "http://ridu.test/api/panic", nil, "")
	body := readBody(t, panicked)
	if panicked.StatusCode != http.StatusInternalServerError || strings.Contains(body, "private endpoint secret") {
		t.Fatalf("panic response = %d: %s", panicked.StatusCode, body)
	}
	if !diagnostic.Panic || diagnostic.Stack == "" || diagnostic.Error == nil || strings.Contains(diagnostic.Error.Error(), "private endpoint secret") {
		t.Fatalf("panic diagnostic = %#v", diagnostic)
	}
	streamed := requestJSON(t, client, http.MethodGet, "http://ridu.test/api/stream-panic", nil, "")
	streamBody := readBody(t, streamed)
	if streamed.StatusCode != http.StatusOK || streamBody != "" || streamObservation.Status != http.StatusOK {
		t.Fatalf("stream panic response = %d %q, observation = %#v", streamed.StatusCode, streamBody, streamObservation)
	}
}

func TestCustomEndpointRawHTTPBoundary(t *testing.T) {
	application, err := ridu.New(ridu.Config{
		Name: "Endpoint HTTP boundary",
		Endpoints: []ridu.Endpoint{
			{Method: http.MethodGet, Path: "/", Handler: func(ctx ridu.EndpointContext) {
				_, _ = io.WriteString(ctx.Writer, "root endpoint")
			}},
			{Method: http.MethodOptions, Path: "/probe", Handler: func(ctx ridu.EndpointContext) {
				ctx.Writer.Header().Set("X-Custom-Options", "handled")
				ctx.Writer.WriteHeader(http.StatusNoContent)
			}},
		},
		Collections: []ridu.Collection{{
			Slug: "posts", Fields: field.Fields{field.Text("title")},
			Endpoints: []ridu.Endpoint{{Method: http.MethodGet, Path: "/:collection", Handler: func(ctx ridu.EndpointContext) {
				_, _ = io.WriteString(ctx.Writer, ctx.RouteParams["collection"])
			}}},
		}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	client := handlerClient(application.Handler(ridu.HandlerOptions{}))

	for _, path := range []string{"http://ridu.test/api", "http://ridu.test/api/"} {
		response := requestJSON(t, client, http.MethodGet, path, nil, "")
		if body := readBody(t, response); body != "root endpoint" {
			t.Fatalf("%s body = %q", path, body)
		}
		if got := response.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/plain") {
			t.Fatalf("%s content type = %q", path, got)
		}
		if got := response.Header.Get("Cache-Control"); got != "private, no-store" {
			t.Fatalf("%s cache control = %q", path, got)
		}
	}

	optionsRequest, err := http.NewRequest(http.MethodOptions, "http://ridu.test/api/probe", nil)
	if err != nil {
		t.Fatal(err)
	}
	optionsRequest.Header.Set("Origin", "http://ridu.test")
	optionsRequest.Header.Set("Access-Control-Request-Method", http.MethodPost)
	optionsResponse, err := client.Do(optionsRequest)
	if err != nil {
		t.Fatal(err)
	}
	optionsResponse.Body.Close()
	if optionsResponse.StatusCode != http.StatusNoContent || optionsResponse.Header.Get("X-Custom-Options") != "handled" {
		t.Fatalf("OPTIONS response = %d %#v", optionsResponse.StatusCode, optionsResponse.Header)
	}

	reservedParam := requestJSON(t, client, http.MethodGet, "http://ridu.test/api/collections/posts/private", nil, "")
	if body := readBody(t, reservedParam); body != "private" {
		t.Fatalf("reserved-name route param = %q", body)
	}
}

func TestCustomEndpointSupportsEveryDeclaredMethod(t *testing.T) {
	methods := []string{http.MethodConnect, http.MethodDelete, http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodPatch, http.MethodPost, http.MethodPut}
	endpoints := make([]ridu.Endpoint, 0, len(methods))
	for _, method := range methods {
		current := method
		endpoints = append(endpoints, ridu.Endpoint{Method: current, Path: "/method", Handler: func(ctx ridu.EndpointContext) {
			ctx.Writer.Header().Set("X-Custom-Method", current)
			ctx.Writer.WriteHeader(http.StatusNoContent)
		}})
	}
	application, err := ridu.New(ridu.Config{
		Name:        "Endpoint methods",
		Endpoints:   endpoints,
		Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{field.Text("title")}}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	client := handlerClient(application.Handler(ridu.HandlerOptions{}))
	for _, method := range methods {
		request, err := http.NewRequest(method, "http://ridu.test/api/method", nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatalf("%s request: %v", method, err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusNoContent || response.Header.Get("X-Custom-Method") != method {
			t.Fatalf("%s response = %d %#v", method, response.StatusCode, response.Header)
		}
	}
}

func jsonEndpoint(value map[string]string) ridu.EndpointHandler {
	return func(ctx ridu.EndpointContext) { jsonResponse(ctx.Writer, value) }
}

func jsonResponse(writer http.ResponseWriter, value any) {
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(value)
}

func assertJSONField(t *testing.T, response *http.Response, field, expected string) {
	t.Helper()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("response status = %d: %s", response.StatusCode, readBody(t, response))
	}
	var body map[string]string
	decodeResponse(t, response, &body)
	if body[field] != expected {
		t.Fatalf("response %s = %q, want %q", field, body[field], expected)
	}
}
