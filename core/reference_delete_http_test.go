package core_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/protocol"
	"github.com/riducms/ridu/store"
)

func TestRESTHardDeleteRestrictionUsesStableNonOracleEnvelope(t *testing.T) {
	ctx := context.Background()
	application, err := ridu.New(ridu.Config{Name: "reference delete REST", Collections: []ridu.Collection{
		{Slug: "users", Fields: []field.Definition{field.Text("name")}},
		{Slug: "posts", Fields: []field.Definition{
			field.Text("title"),
			field.Relationship("protectedOwner", field.To("users"), field.OnDelete(field.ReferenceDeleteRestrict)),
		}},
	}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	target, err := application.Local().Create(ctx, "users", store.Values{"name": store.String("Ada")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := application.Local().Create(ctx, "posts", store.Values{
		"title": store.String("Restricted"), "protectedOwner": store.String(target.ID),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	client := handlerClient(application.Handler(ridu.HandlerOptions{}))
	response := requestJSON(t, client, http.MethodDelete, "http://ridu.test/api/collections/users/"+target.ID, nil, "")
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	var envelope protocol.ErrorEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode delete restriction: %v: %s", err, body)
	}
	if response.StatusCode != http.StatusConflict || envelope.Error.Code != protocol.ErrorDeleteRestricted ||
		envelope.Error.Status != http.StatusConflict || envelope.Error.Message != "document deletion is restricted by current references" {
		t.Fatalf("restriction envelope = status %d, %#v", response.StatusCode, envelope)
	}
	if envelope.Error.RequestID == "" || len(envelope.Error.Issues) != 0 || len(envelope.Error.Details) != 0 {
		t.Fatalf("restriction envelope metadata = %#v", envelope.Error)
	}
	for _, secret := range []string{owner.ID, target.ID, "protectedOwner"} {
		if strings.Contains(string(body), secret) {
			t.Fatalf("restriction envelope disclosed %q: %s", secret, body)
		}
	}
}
