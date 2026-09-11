package migration

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/riducms/ridu/schema"
)

func TestNewArtifactPreservesPlannerProvenance(t *testing.T) {
	planner := Planner{Name: "example-planner", Version: "2.3.4"}
	after := schema.NewManifest(schema.Snapshot{
		Version:     schema.CurrentVersion,
		Application: schema.Application{Name: "Planner provenance"},
		Plugins:     []schema.Plugin{},
		Collections: []schema.Collection{},
	})

	artifact, err := NewArtifact("initial", planner, nil, after)
	if err != nil {
		t.Fatal(err)
	}
	if artifact.Planner != planner {
		t.Fatalf("planner = %#v, want %#v", artifact.Planner, planner)
	}
}

func TestNonInitialArtifactPlanRequiresPublicationBindingBeforeIdentity(t *testing.T) {
	planner := Planner{Name: "example-planner", Version: "1.0.0"}
	before := schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "Before"},
	})
	after := schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "After"},
	})
	artifact, err := NewArtifact("change", planner, &before, after)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := artifact.Digest(); err == nil || !strings.Contains(err.Error(), "previous artifact digest is required") {
		t.Fatalf("unbound Digest() error = %v", err)
	}
	if _, err := json.Marshal(artifact); err == nil || !strings.Contains(err.Error(), "previous artifact digest is required") {
		t.Fatalf("unbound MarshalJSON() error = %v", err)
	}
}

func TestInitialArtifactRoundTripsWithoutPredecessor(t *testing.T) {
	planner := Planner{Name: "example-planner", Version: "1.0.0"}
	manifest := schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "Initial"},
	})
	artifact, err := NewArtifact("initial", planner, nil, manifest)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := MarshalStepPayload(AssertSchemaPayload{})
	if err != nil {
		t.Fatal(err)
	}
	steps := []Step{{
		ID: "step-0001", Kind: StepAssertSchema, ExecutorVersion: 1,
		Name: "verify initial schema", Payload: payload,
	}}
	physicalBefore := PhysicalDigestSeed(artifact.FromDigest)
	physicalAfter, err := PhasePhysicalDigest(physicalBefore, PhaseTransaction, steps)
	if err != nil {
		t.Fatal(err)
	}
	artifact.Phases = []Phase{{
		ID: "phase-001", Mode: PhaseTransaction,
		PhysicalContractVersion: PhysicalContractVersion,
		BeforePhysicalDigest:    physicalBefore, AfterPhysicalDigest: physicalAfter, Steps: steps,
	}}
	encoded, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeArtifact(encoded)
	if err != nil {
		t.Fatal(err)
	}
	want, err := artifact.Digest()
	if err != nil {
		t.Fatal(err)
	}
	got, err := decoded.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("initial artifact digest changed through codec: %s != %s", got, want)
	}
}

func TestRetiredPluginSQLStepCannotEnterExecution(t *testing.T) {
	err := validateStepPayload(PhaseTransaction, Step{Kind: "plugin_sql", ExecutorVersion: 1, Payload: json.RawMessage(`{"sql":["DROP TABLE posts"]}`)})
	if err == nil || !strings.Contains(err.Error(), `unknown kind "plugin_sql"`) {
		t.Fatalf("retired SQL step accepted: %v", err)
	}
}
