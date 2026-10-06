package core_test

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/protocol"
	"github.com/riducms/ridu/store"
)

// A REST error's code agrees with its status, clients can act on
// publish_required and bad_query, and observations keep the specific reason
// behind the public code for monitoring.
func TestRESTErrorCodesAgreeWithStatusAndKeepTheirReason(t *testing.T) {
	application, err := ridu.New(ridu.Config{
		Name: "Error codes",
		Collections: []ridu.Collection{{
			Slug: "pages", Versions: true,
			Access: ridu.CollectionAccess{
				Read:   func(ridu.AccessContext) (ridu.AccessDecision, error) { return ridu.Allow(), nil },
				Update: func(ridu.AccessContext) (ridu.AccessDecision, error) { return ridu.Allow(), nil },
			},
			Fields: field.Fields{field.Text("title").Required()},
		}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	page, err := application.Local().Create(context.Background(), "pages", store.Values{"title": store.String("About")}, ridu.MutationOptions{System: true})
	if err != nil {
		t.Fatal(err)
	}
	var mutex sync.Mutex
	observed := map[string]ridu.RequestObservation{}
	client := handlerClient(application.Handler(ridu.HandlerOptions{
		MaxBodyBytes: 256,
		Observe: func(observation ridu.RequestObservation) {
			mutex.Lock()
			defer mutex.Unlock()
			observed[observation.Method+" "+observation.Path] = observation
		},
	}))

	where := url.Values{"where": {`{"title":{"bogus":"x"}}`}}.Encode()
	for _, test := range []struct {
		name, method, target, body string
		status                     int
		code                       protocol.ErrorCode
		reason                     string
	}{
		{"unknown collection", http.MethodGet, "/api/collections/missing", "", http.StatusNotFound, protocol.ErrorNotFound, "unknown_collection"},
		{"update of a published page", http.MethodPatch, "/api/collections/pages/" + page.ID, `{"title":"About us"}`, http.StatusConflict, protocol.ErrorPublishRequired, "publish_required"},
		{"refused filter", http.MethodGet, "/api/collections/pages?" + where, "", http.StatusBadRequest, protocol.ErrorBadQuery, "bad_query"},
		{"body over the limit", http.MethodPatch, "/api/collections/pages/" + page.ID, `{"title":"` + strings.Repeat("x", 512) + `"}`, http.StatusRequestEntityTooLarge, protocol.ErrorBadRequest, "body_too_large"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var body io.Reader
			if test.body != "" {
				body = strings.NewReader(test.body)
			}
			response := requestJSON(t, client, test.method, "http://ridu.test"+test.target, body, "")
			var envelope protocol.ErrorEnvelope
			decodeResponse(t, response, &envelope)
			if response.StatusCode != test.status || envelope.Error.Code != test.code {
				t.Fatalf("%s %s = %d %q, want %d %q", test.method, test.target, response.StatusCode, envelope.Error.Code, test.status, test.code)
			}
			path := strings.SplitN(test.target, "?", 2)[0]
			mutex.Lock()
			observation := observed[test.method+" "+path]
			mutex.Unlock()
			if observation.ErrorCode != string(test.code) || observation.ErrorReason != test.reason {
				t.Fatalf("observation = code %q reason %q, want %q and %q", observation.ErrorCode, observation.ErrorReason, test.code, test.reason)
			}
		})
	}
}
