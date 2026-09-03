package postgres

import (
	"encoding/json"
	"testing"
)

func TestReferenceBackfillCheckpointRejectsInvalidLedgerProgress(t *testing.T) {
	for name, encoded := range map[string]string{
		"unknown field":        `{"initialized":true,"resource":0,"extra":true}`,
		"duplicate field":      `{"initialized":true,"resource":0,"resource":1}`,
		"trailing value":       `{"initialized":true,"resource":0} {}`,
		"negative resource":    `{"initialized":true,"resource":-1}`,
		"resource past end":    `{"initialized":true,"resource":3}`,
		"uninitialized cursor": `{"initialized":false,"resource":0,"lastID":"skipped"}`,
		"completed cursor":     `{"initialized":true,"resource":2,"lastID":"skipped"}`,
	} {
		t.Run(name, func(t *testing.T) {
			checkpoint := referenceBackfillCheckpoint{}
			decodeError := decodeReferenceBackfillCheckpoint(json.RawMessage(encoded), &checkpoint)
			if decodeError == nil {
				decodeError = validateReferenceBackfillCheckpoint(checkpoint, 2)
			}
			if decodeError == nil {
				t.Fatalf("checkpoint %s was accepted", encoded)
			}
		})
	}

	for _, encoded := range []string{
		`{"initialized":false,"resource":0}`,
		`{"initialized":true,"resource":0}`,
		`{"initialized":true,"resource":1,"lastID":"document-1"}`,
		`{"initialized":true,"resource":2}`,
	} {
		checkpoint := referenceBackfillCheckpoint{}
		if err := decodeReferenceBackfillCheckpoint(json.RawMessage(encoded), &checkpoint); err != nil {
			t.Fatalf("decode valid checkpoint %s: %v", encoded, err)
		}
		if err := validateReferenceBackfillCheckpoint(checkpoint, 2); err != nil {
			t.Fatalf("validate checkpoint %s: %v", encoded, err)
		}
	}
}
