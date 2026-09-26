package postgres

import (
	"database/sql"
	"encoding/json"
	"testing"
)

func TestRewriteScheduledPublicationTaskCollectionIDPreservesUnknownInput(t *testing.T) {
	row := scheduledPublicationTaskRenameRow{
		ID:                      "task-1",
		TargetCollectionID:      sql.NullString{String: "users-old", Valid: true},
		TargetDocumentID:        sql.NullString{String: "post-1", Valid: true},
		RequestedByCollectionID: sql.NullString{String: "users-old", Valid: true},
		RequestedByDocumentID:   sql.NullString{String: "user-1", Valid: true},
		Input: json.RawMessage(`{
            "collectionID":"users-old",
            "documentID":"post-1",
            "expectedRevision":3,
            "requestedByCollectionID":"users-old",
            "requestedByUserID":"user-1",
            "future":{"keep":true}
        }`),
		ConcurrencyKey: sql.NullString{String: "users-old:post-1", Valid: true},
	}
	encoded, concurrencyKey, target, requester, err := rewriteScheduledPublicationTaskCollectionID(row, "users-old", "users-new")
	if err != nil {
		t.Fatal(err)
	}
	if target != "users-new" || requester != "users-new" {
		t.Fatalf("identity rewrite = target %#v requester %#v", target, requester)
	}
	if want := scheduledPublicationConcurrencyKey("users-new", "post-1"); concurrencyKey != want {
		t.Fatalf("concurrency key = %q, want %q", concurrencyKey, want)
	}
	var input map[string]any
	if err := json.Unmarshal(encoded, &input); err != nil {
		t.Fatal(err)
	}
	if input["collectionID"] != "users-new" || input["requestedByCollectionID"] != "users-new" {
		t.Fatalf("rewritten input = %#v", input)
	}
	future, ok := input["future"].(map[string]any)
	if !ok || future["keep"] != true || input["expectedRevision"] != float64(3) {
		t.Fatalf("unknown input was not preserved: %#v", input)
	}
}

func TestRewriteScheduledPublicationTaskCollectionIDFailsClosedOnDivergentIdentity(t *testing.T) {
	row := scheduledPublicationTaskRenameRow{
		ID:                 "task-1",
		TargetCollectionID: sql.NullString{String: "users-old", Valid: true},
		TargetDocumentID:   sql.NullString{String: "post-1", Valid: true},
		Input:              json.RawMessage(`{"collectionID":"different","documentID":"post-1","expectedRevision":3}`),
	}
	if _, _, _, _, err := rewriteScheduledPublicationTaskCollectionID(row, "users-old", "users-new"); err == nil {
		t.Fatal("expected divergent persisted identity to fail closed")
	}
}
