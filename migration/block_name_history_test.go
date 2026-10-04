package migration

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
)

func TestArtifactHistoryPreservesBlockSchemaBeforeBlockName(t *testing.T) {
	current := blockNameHistoryManifest(t)
	previousSnapshot := current.Snapshot()
	block := &previousSnapshot.Collections[0].Fields[0].Blocks.Types[0]
	block.Fields = block.Fields[:1]
	previous := schema.NewManifest(previousSnapshot)
	previousBytes, err := previous.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := schema.Parse(previousBytes); err == nil {
		t.Fatal("current manifest parser accepted a block without blockName")
	}
	previousDigest, err := DigestManifest(previous)
	if err != nil {
		t.Fatal(err)
	}

	planner := Planner{Name: "example-planner", Version: "1.0.0"}
	initial, err := NewArtifact("initial", planner, nil, current)
	if err != nil {
		t.Fatal(err)
	}
	// A previously committed artifact had this exact after snapshot. Do not
	// rewrite its schema or its digest when reading it with a newer binary.
	initial.After = previousSnapshot
	initial.ToDigest = previousDigest
	blockNameHistoryAssertion(t, &initial)
	initialBytes, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	initialDigest, err := initial.Digest()
	if err != nil {
		t.Fatal(err)
	}
	decodedInitial, err := DecodeArtifact(initialBytes)
	if err != nil {
		t.Fatalf("decode historical head: %v", err)
	}
	decodedDigest, err := decodedInitial.Digest()
	if err != nil || decodedDigest != initialDigest {
		t.Fatalf("historical artifact digest = %q, %v; want %q", decodedDigest, err, initialDigest)
	}
	head, err := decodedInitial.AfterManifest()
	if err != nil {
		t.Fatal(err)
	}
	headBytes, err := head.Bytes()
	if err != nil || !bytes.Equal(headBytes, previousBytes) {
		t.Fatalf("historical after manifest changed: %v\ngot: %s\nwant: %s", err, headBytes, previousBytes)
	}
	if decodedInitial.ToDigest != previousDigest {
		t.Fatalf("historical head digest = %s, want %s", decodedInitial.ToDigest, previousDigest)
	}

	continuation, err := NewArtifact("add-block-name", planner, &head, current)
	if err != nil {
		t.Fatalf("continue historical head: %v", err)
	}
	continuation.PreviousArtifactDigest = initialDigest
	blockNameHistoryAssertion(t, &continuation)
	continuationBytes, err := json.Marshal(continuation)
	if err != nil {
		t.Fatal(err)
	}
	decodedContinuation, err := DecodeArtifact(continuationBytes)
	if err != nil {
		t.Fatalf("decode current continuation: %v", err)
	}
	before, err := decodedContinuation.BeforeManifest()
	if err != nil {
		t.Fatal(err)
	}
	beforeBytes, err := before.Bytes()
	if err != nil || !bytes.Equal(beforeBytes, previousBytes) {
		t.Fatalf("continuation before manifest changed: %v\ngot: %s\nwant: %s", err, beforeBytes, previousBytes)
	}
	if decodedContinuation.FromDigest != previousDigest || decodedContinuation.PreviousArtifactDigest != initialDigest {
		t.Fatalf("continuation lost manifest or artifact lineage: %#v", decodedContinuation)
	}
}

func blockNameHistoryManifest(t *testing.T) schema.Manifest {
	t.Helper()
	path := func(parts ...string) query.Path {
		t.Helper()
		value, err := query.NewPath(parts...)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	nameDefault := ""
	manifest := schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "Block history"},
		Collections: []schema.Collection{{
			ID: "pages", Slug: "pages", Labels: schema.CollectionLabels{Singular: "Page", Plural: "Pages"},
			Fields: []schema.Field{{
				ID: "pages-layout", Name: "layout", Path: path("layout"), Type: schema.FieldTypeBlocks,
				Category: schema.FieldCategoryNested,
				Blocks: &schema.BlocksField{Types: []schema.BlockType{{
					Slug: "card", Labels: schema.BlockLabels{Singular: "Card", Plural: "Cards"},
					Fields: []schema.Field{{
						ID: "pages-layout-card-title", Name: "title", Path: path("layout", "card", "title"),
						Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar, Text: &schema.TextField{},
					}, {
						ID: "pages-layout-card-blockName", Name: "blockName", Path: path("layout", "card", "blockName"),
						Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar, Text: &schema.TextField{}, Default: &nameDefault,
					}},
				}}},
			}},
		}},
		Plugins: []schema.Plugin{},
	})
	encoded, err := manifest.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := schema.Parse(encoded); err != nil {
		t.Fatalf("current block fixture is invalid: %v", err)
	}
	return manifest
}

func blockNameHistoryAssertion(t *testing.T, artifact *Artifact) {
	t.Helper()
	payload, err := MarshalStepPayload(AssertSchemaPayload{})
	if err != nil {
		t.Fatal(err)
	}
	steps := []Step{{
		ID: "step-0001", Kind: StepAssertSchema, ExecutorVersion: 1,
		Name: "verify resulting schema", Payload: payload,
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
}
