package migration

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/riducms/ridu/schema"
)

func TestMongoDBCreateIndexPayloadIsClosedAndNoTransactionOnly(t *testing.T) {
	valid := func() Step {
		payload, err := MarshalStepPayload(MongoDBCreateIndexPayload{
			Collection: "z_c_0123456789abcdef", Index: "z_i_0123456789abcdef",
		})
		if err != nil {
			t.Fatal(err)
		}
		return Step{Kind: StepMongoDBCreateIndex, Payload: payload}
	}
	if err := validateStepPayload(PhaseNoTransaction, valid()); err != nil {
		t.Fatalf("valid payload: %v", err)
	}

	for name, candidate := range map[string]struct {
		mode    PhaseMode
		payload string
	}{
		"transaction phase":  {PhaseTransaction, `{"collection":"z_c_0123456789abcdef","index":"z_i_0123456789abcdef"}`},
		"batch phase":        {PhaseBatch, `{"collection":"z_c_0123456789abcdef","index":"z_i_0123456789abcdef"}`},
		"null payload":       {PhaseNoTransaction, `null`},
		"array payload":      {PhaseNoTransaction, `[]`},
		"trailing JSON":      {PhaseNoTransaction, `{"collection":"z_c_0123456789abcdef","index":"z_i_0123456789abcdef"}{}`},
		"missing collection": {PhaseNoTransaction, `{"collection":"","index":"z_i_0123456789abcdef"}`},
		"missing index":      {PhaseNoTransaction, `{"collection":"z_c_0123456789abcdef","index":""}`},
		"unsafe collection":  {PhaseNoTransaction, `{"collection":"z_c.bad","index":"z_i_0123456789abcdef"}`},
		"numeric collection": {PhaseNoTransaction, `{"collection":"1_z_c","index":"z_i_0123456789abcdef"}`},
		"unsafe index":       {PhaseNoTransaction, `{"collection":"z_c_0123456789abcdef","index":"z_i.bad"}`},
		"native index":       {PhaseNoTransaction, `{"collection":"z_c_0123456789abcdef","index":"_id_"}`},
		"action field":       {PhaseNoTransaction, `{"collection":"z_c_0123456789abcdef","index":"z_i_0123456789abcdef","action":"drop"}`},
		"keys field":         {PhaseNoTransaction, `{"collection":"z_c_0123456789abcdef","index":"z_i_0123456789abcdef","keys":{}}`},
		"partial filter":     {PhaseNoTransaction, `{"collection":"z_c_0123456789abcdef","index":"z_i_0123456789abcdef","partialFilter":{}}`},
		"command field":      {PhaseNoTransaction, `{"collection":"z_c_0123456789abcdef","index":"z_i_0123456789abcdef","command":{}}`},
		"duplicate field":    {PhaseNoTransaction, `{"collection":"z_c_0123456789abcdef","collection":"z_other","index":"z_i_0123456789abcdef"}`},
	} {
		t.Run(name, func(t *testing.T) {
			step := Step{Kind: StepMongoDBCreateIndex, Payload: json.RawMessage(candidate.payload)}
			if err := validateStepPayload(candidate.mode, step); err == nil {
				t.Fatal("invalid MongoDB index payload was admitted")
			}
		})
	}

	tooLong := strings.Repeat("a", 128)
	step := Step{Kind: StepMongoDBCreateIndex, Payload: json.RawMessage(`{"collection":"` + tooLong + `","index":"z_i_0123456789abcdef"}`)}
	if err := validateStepPayload(PhaseNoTransaction, step); err == nil {
		t.Fatal("overlong MongoDB collection identity was admitted")
	}
	step = Step{Kind: StepMongoDBCreateIndex, Payload: json.RawMessage(`{"collection":"z_c_0123456789abcdef","index":"` + tooLong + `"}`)}
	if err := validateStepPayload(PhaseNoTransaction, step); err == nil {
		t.Fatal("overlong MongoDB index identity was admitted")
	}
}

func TestMongoDBAssertSchemaIsCanonicalAndNoTransactionOnly(t *testing.T) {
	if err := validateStepPayload(PhaseNoTransaction, Step{Kind: StepMongoDBAssertSchema, Payload: json.RawMessage(`{}`)}); err != nil {
		t.Fatalf("canonical assertion payload: %v", err)
	}
	for name, candidate := range map[string]struct {
		mode    PhaseMode
		payload string
	}{
		"transaction phase":   {PhaseTransaction, `{}`},
		"batch phase":         {PhaseBatch, `{}`},
		"null payload":        {PhaseNoTransaction, `null`},
		"unknown field":       {PhaseNoTransaction, `{"unexpected":true}`},
		"leading whitespace":  {PhaseNoTransaction, ` {}`},
		"trailing whitespace": {PhaseNoTransaction, "{}\n"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateStepPayload(candidate.mode, Step{Kind: StepMongoDBAssertSchema, Payload: json.RawMessage(candidate.payload)}); err == nil {
				t.Fatal("invalid MongoDB schema assertion was admitted")
			}
		})
	}
}

func TestMongoDBCreateRequiresMongoDBFinalAssertion(t *testing.T) {
	manifest := schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "MongoDB migration contract"},
		Collections: []schema.Collection{}, Plugins: []schema.Plugin{},
	})
	artifact, err := NewArtifact("initial", Planner{Name: "mongodb", Version: "1.0.0"}, nil, manifest)
	if err != nil {
		t.Fatal(err)
	}
	indexPayload, err := MarshalStepPayload(MongoDBCreateIndexPayload{Collection: "z_ridu_tasks", Index: "z_task_due"})
	if err != nil {
		t.Fatal(err)
	}
	assertPayload, err := MarshalStepPayload(AssertSchemaPayload{})
	if err != nil {
		t.Fatal(err)
	}
	physical := PhysicalDigestSeed(artifact.FromDigest)
	steps := []Step{
		{ID: "step-0001", Kind: StepMongoDBCreateIndex, ExecutorVersion: 1, Name: "create task due index", Payload: indexPayload},
		{ID: "step-0002", Kind: StepAssertSchema, ExecutorVersion: 1, Name: "generic assertion", Payload: assertPayload},
	}
	for index, step := range steps {
		mode := PhaseNoTransaction
		if step.Kind == StepAssertSchema {
			mode = PhaseTransaction
		}
		after, digestErr := PhasePhysicalDigest(physical, mode, []Step{step})
		if digestErr != nil {
			t.Fatal(digestErr)
		}
		artifact.Phases = append(artifact.Phases, Phase{
			ID: fmt.Sprintf("phase-%03d", index+1), Mode: mode, PhysicalContractVersion: PhysicalContractVersion,
			BeforePhysicalDigest: physical, AfterPhysicalDigest: after, Steps: []Step{step},
		})
		physical = after
	}
	if err := artifact.Validate(); err == nil || !strings.Contains(err.Error(), "must end with a MongoDB schema assertion") {
		t.Fatalf("mixed assertion error = %v", err)
	}
}
