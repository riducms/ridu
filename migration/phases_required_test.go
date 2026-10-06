package migration

import (
	"encoding/json"
	"testing"
)

// The required-value audit reads stored documents in the transaction that
// commits the schema change, and names at least one distinct field.
func TestRequiredValueAuditPayloadIsTransactionalAndNamesFields(t *testing.T) {
	valid := Step{Kind: StepAuditRequiredValues, Payload: json.RawMessage(`{"fields":[{"resourceId":"posts","path":"seo.title"}]}`)}
	if err := validateStepPayload(PhaseTransaction, valid); err != nil {
		t.Fatal(err)
	}
	for name, candidate := range map[string]struct {
		mode    PhaseMode
		payload string
	}{
		"outside a transaction": {PhaseNoTransaction, `{"fields":[{"resourceId":"posts","path":"title"}]}`},
		"no fields":             {PhaseTransaction, `{"fields":[]}`},
		"invalid resource":      {PhaseTransaction, `{"fields":[{"resourceId":"Posts!","path":"title"}]}`},
		"empty path":            {PhaseTransaction, `{"fields":[{"resourceId":"posts","path":""}]}`},
		"duplicate field":       {PhaseTransaction, `{"fields":[{"resourceId":"posts","path":"title"},{"resourceId":"posts","path":"title"}]}`},
		"unknown member":        {PhaseTransaction, `{"fields":[{"resourceId":"posts","path":"title"}],"sql":"SELECT 1"}`},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateStepPayload(candidate.mode, Step{Kind: StepAuditRequiredValues, Payload: json.RawMessage(candidate.payload)}); err == nil {
				t.Fatal("malformed audit step was accepted")
			}
		})
	}
}
