package content

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/store"
)

func TestFieldHookExamplesChangeTheIntendedValues(t *testing.T) {
	backend := teststore.New()
	posts := Posts
	posts.Fields = append(posts.Fields.Snapshot(), SKU, DisplayCode)
	config := ridu.Config{Name: "Hook examples", Collections: []ridu.Collection{posts}}
	app, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	created, err := app.Local().Create(t.Context(), "posts", store.Values{
		"title":       store.String("  Hello, Ridu  "),
		"sku":         store.String("ab-12"),
		"displayCode": store.String("xy-34"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"title": "Hello, Ridu", "sku": "AB-12", "displayCode": "XY-34"} {
		if got, _ := created.Values[name].StringValue(); got != want {
			t.Fatalf("response %s = %q, want %q", name, got, want)
		}
	}

	// Read through the same schema without the display formatter to inspect storage.
	posts.Fields = append(Posts.Fields.Snapshot(), SKU, field.Text("displayCode"))
	config.Collections = []ridu.Collection{posts}
	plain, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := plain.Local().Find(t.Context(), "posts", created.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := stored.Values["displayCode"].StringValue(); got != "xy-34" {
		t.Fatalf("read hook changed saved code: %q", got)
	}
}

func TestAuditAndNotificationExamplesRespectCommit(t *testing.T) {
	for _, test := range []struct {
		name               string
		rejectAudit        bool
		webhookStatus      int
		wantSaved          int
		wantWebhooks       int32
		wantCommittedError bool
	}{
		{name: "successful save", webhookStatus: 204, wantSaved: 1, wantWebhooks: 1},
		{name: "audit rejection rolls back", rejectAudit: true, webhookStatus: 204},
		{name: "webhook failure leaves document saved", webhookStatus: 503, wantSaved: 1, wantWebhooks: 1, wantCommittedError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var event struct {
					ID        string
					Operation string
				}
				if err := json.NewDecoder(r.Body).Decode(&event); err != nil || event.ID == "" || event.Operation != "create" {
					t.Errorf("webhook event = %#v, error = %v", event, err)
				}
				calls.Add(1)
				w.WriteHeader(test.webhookStatus)
			}))
			defer server.Close()
			posts := Posts
			posts.Hooks = ridu.CollectionHooks{
				AfterChange: []ridu.Hook{writeAuditEntry},
				AfterCommit: []ridu.Hook{notifyPosts(server.URL)},
			}
			audit := AuditLog
			if test.rejectAudit {
				audit.Fields = append(audit.Fields.Snapshot(), field.Text("reason").Required())
			}
			app, err := ridu.New(ridu.Config{Name: "Hook delivery", Collections: []ridu.Collection{posts, audit}}, teststore.New())
			if err != nil {
				t.Fatal(err)
			}
			_, err = app.Local().Create(t.Context(), "posts", store.Values{"title": store.String("A post")}, nil)
			if (err != nil) != (test.rejectAudit || test.wantCommittedError) {
				t.Fatalf("create error = %v", err)
			}
			if test.wantCommittedError {
				var failure *ridu.OperationError
				if !errors.As(err, &failure) || !failure.Committed || failure.Code != "hook_failed" {
					t.Fatalf("expected committed-hook error: %v", err)
				}
			}
			for _, collection := range []string{"posts", "audit-log"} {
				page, err := app.Local().List(t.Context(), collection, ridu.ListOptions{})
				if err != nil || len(page.Documents) != test.wantSaved {
					t.Fatalf("saved %s = %d, want %d, error=%v", collection, len(page.Documents), test.wantSaved, err)
				}
			}
			if got := calls.Load(); got != test.wantWebhooks {
				t.Fatalf("webhooks after save and read = %d, want %d", got, test.wantWebhooks)
			}
		})
	}
}
