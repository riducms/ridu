package protocol_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/riducms/ridu/protocol"
)

func TestErrorEnvelopeConformanceFixture(t *testing.T) {
	envelope := protocol.ErrorEnvelope{Error: protocol.ErrorPayload{
		Code:      protocol.ErrorValidation,
		Status:    422,
		Message:   "document validation failed",
		RequestID: "req_fixture",
		Issues: []protocol.ValidationIssue{{
			Code:    "required",
			Path:    "title",
			Message: "Title is required",
		}},
		Details: json.RawMessage(`{"operation":"create"}`),
	}}
	actual, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	actual = append(actual, '\n')
	expected, err := os.ReadFile(filepath.Join("..", "testdata", "protocol", "error.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actual, expected) {
		t.Fatalf("error fixture drift\nexpected:\n%s\nactual:\n%s", expected, actual)
	}
}
