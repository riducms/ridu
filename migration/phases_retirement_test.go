package migration

import (
	"encoding/json"
	"testing"

	"github.com/riducms/ridu/schema"
)

func TestRetireResourcesPayloadRequiresCanonicalStableIDsAndTransaction(t *testing.T) {
	step := func(payload string) Step {
		return Step{Kind: StepRetireResources, Payload: json.RawMessage(payload)}
	}
	if err := validateStepPayload(PhaseTransaction, step(`{"resourceIds":["global-old","users-old"]}`)); err != nil {
		t.Fatalf("canonical resource retirement payload: %v", err)
	}
	for name, candidate := range map[string]struct {
		mode    PhaseMode
		payload string
	}{
		"empty":                     {PhaseTransaction, `{"resourceIds":[]}`},
		"duplicate":                 {PhaseTransaction, `{"resourceIds":["users-old","users-old"]}`},
		"not sorted":                {PhaseTransaction, `{"resourceIds":["users-old","global-old"]}`},
		"invalid ID":                {PhaseTransaction, `{"resourceIds":["Users"]}`},
		"version owners not sorted": {PhaseTransaction, `{"resourceIds":["users-old"],"purgeVersionOwnerIds":["posts","entries"]}`},
		"version owner overlaps":    {PhaseTransaction, `{"resourceIds":["users-old"],"purgeVersionOwnerIds":["users-old"]}`},
		"unknown field":             {PhaseTransaction, `{"resourceIds":["users-old"],"future":true}`},
		"batch phase":               {PhaseBatch, `{"resourceIds":["users-old"]}`},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateStepPayload(candidate.mode, step(candidate.payload)); err == nil {
				t.Fatal("invalid resource retirement payload was admitted")
			}
		})
	}

	payload, err := MarshalStepPayload(RetireResourcesPayload{
		ResourceIDs: []schema.StableID{"global-old", "users-old"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) != `{"resourceIds":["global-old","users-old"]}` {
		t.Fatalf("canonical resource retirement payload = %s", payload)
	}
}
