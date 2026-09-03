package mongodb

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu/internal/migrationartifact"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
)

func TestFrozenMongoDBV1HistoryReplansWithCurrentPlanner(t *testing.T) {
	directory := filepath.Join("testdata", "historical-v1")
	files, err := migrationartifact.ReadAll(directory)
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		name, digest, physicalPlanDigest string
		phases                           int
		risks                            int
	}{
		{name: "20260831120000.000000000_initial.ridu.json", digest: "67ef7ec7d91e9378389abb4299275ed33466931fabe79d16fa2a9f185d512eb4", physicalPlanDigest: "96c7135ece752141077cf6dbdacb7d6fcd06a6e9191f554aae3356a192fc42c9", phases: 22, risks: 9},
		{name: "20260831120001.000000000_add-title-index.ridu.json", digest: "54f86c6d0db5f34410e08c9f02def5b4e128f1ee13f5750bc8c3e76572183c09", physicalPlanDigest: "068fdd5e9ed281903c812ec362d156c197c7eadcfe0d719a938dd8fcddecddf6", phases: 2, risks: 1},
		{name: "20260831120002.000000000_add-summary.ridu.json", digest: "fd89ab34bc7b928ee356fbae91c1ea0379933b4a123e428009f8f4210b668824", physicalPlanDigest: "068fdd5e9ed281903c812ec362d156c197c7eadcfe0d719a938dd8fcddecddf6", phases: 1, risks: 0},
	}
	if len(files) != len(want) {
		t.Fatalf("frozen MongoDB history contains %d artifacts, want %d", len(files), len(want))
	}
	for index, expected := range want {
		file := files[index]
		if file.Name != expected.name || file.Digest != expected.digest {
			t.Fatalf("frozen MongoDB artifact %d = %s/%s, want %s/%s", index, file.Name, file.Digest, expected.name, expected.digest)
		}
		if file.Artifact.Version != 1 || file.Artifact.MinimumRunnerContract != 1 ||
			file.Artifact.Planner.Name != mongoDBPlannerName || file.Artifact.Planner.Version != mongoDBPlannerVersion {
			t.Fatalf("frozen MongoDB artifact %s contracts = v%d runner %d planner %#v", file.Name, file.Artifact.Version, file.Artifact.MinimumRunnerContract, file.Artifact.Planner)
		}
		if len(file.Artifact.Phases) != expected.phases || len(file.Artifact.Risks) != expected.risks {
			t.Fatalf("frozen MongoDB artifact %s topology = %d phases/%d risks, want %d/%d", file.Name, len(file.Artifact.Phases), len(file.Artifact.Risks), expected.phases, expected.risks)
		}
		after, err := file.Artifact.AfterManifest()
		if err != nil {
			t.Fatal(err)
		}
		physical, err := mongoPhysicalIndexPlans(after)
		if err != nil {
			t.Fatal(err)
		}
		if digest := frozenMongoDBPhysicalPlanDigest(t, physical); digest != expected.physicalPlanDigest {
			t.Fatalf("frozen MongoDB artifact %s physical plan digest = %s, want %s", file.Name, digest, expected.physicalPlanDigest)
		}
		lastPhase := file.Artifact.Phases[len(file.Artifact.Phases)-1]
		lastStep := lastPhase.Steps[len(lastPhase.Steps)-1]
		if lastPhase.Mode != ridumigration.PhaseNoTransaction || lastStep.Kind != ridumigration.StepMongoDBAssertSchema {
			t.Fatalf("frozen MongoDB artifact %s does not end in the MongoDB assertion", file.Name)
		}
		if index == 0 {
			if file.Artifact.Before != nil || file.Artifact.PreviousArtifactDigest != "" {
				t.Fatalf("frozen initial MongoDB artifact has predecessor state")
			}
		} else if file.Artifact.PreviousArtifactDigest != files[index-1].Digest || file.Artifact.FromDigest != files[index-1].Artifact.ToDigest {
			t.Fatalf("frozen MongoDB artifact %s does not continue its exact predecessor", file.Name)
		}
	}
	if files[1].Artifact.Phases[0].Steps[0].Kind != ridumigration.StepMongoDBCreateIndex ||
		files[2].Artifact.Phases[0].Steps[0].Kind != ridumigration.StepMongoDBAssertSchema {
		t.Fatal("frozen additive-index or manifest-only topology changed")
	}
	if err := validateMongoDBArtifactHistory(context.Background(), files); err != nil {
		t.Fatalf("exactly replan frozen MongoDB history: %v", err)
	}

	generated := t.TempDir()
	generatedFiles := createFrozenMongoDBHistory(t, generated)
	for index := range files {
		if generatedFiles[index].Name != files[index].Name || generatedFiles[index].Digest != files[index].Digest {
			t.Fatalf("regenerated MongoDB artifact %d = %s/%s, frozen %s/%s", index, generatedFiles[index].Name, generatedFiles[index].Digest, files[index].Name, files[index].Digest)
		}
	}
	for _, file := range files {
		encoded, err := os.ReadFile(file.Path)
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"mongodb://", "mongodb+srv://", `"keys"`, `"partialFilter"`, `"command"`, `"action"`} {
			if strings.Contains(string(encoded), forbidden) {
				t.Fatalf("frozen MongoDB artifact %s contains forbidden runtime or private-plan material %q", file.Name, forbidden)
			}
		}
	}
}

// frozenMongoDBPhysicalPlanDigest pins planner-private collection identities,
// index identities, keys, uniqueness, partial filters, and locale inputs for
// the committed v1 history. Artifact digests deliberately exclude this private
// material, so this test-only digest prevents one planner version from silently
// assigning new physical meaning to an accepted artifact.
func frozenMongoDBPhysicalPlanDigest(t *testing.T, plans mongoPhysicalIndexPlanSet) string {
	t.Helper()
	type planRecord struct {
		kind        string
		physical    string
		owner       string
		locales     []string
		definitions []mongoIndexDefinition
	}
	records := make([]planRecord, 0, len(plans.collections)+len(plans.system))
	for _, plan := range plans.collections {
		locales := make([]string, len(plan.locales))
		for index, locale := range plan.locales {
			locales[index] = string(locale)
		}
		records = append(records, planRecord{
			kind: "content", physical: plan.physicalName, owner: string(plan.collection.ID),
			locales: locales, definitions: plan.definitions,
		})
	}
	for _, plan := range plans.system {
		records = append(records, planRecord{
			kind: "system:" + strconv.Itoa(int(plan.kind)), physical: plan.physicalName,
			owner: string(plan.collectionID), definitions: plan.definitions,
		})
	}
	sort.Slice(records, func(left, right int) bool {
		if records[left].kind != records[right].kind {
			return records[left].kind < records[right].kind
		}
		if records[left].physical != records[right].physical {
			return records[left].physical < records[right].physical
		}
		return records[left].owner < records[right].owner
	})

	digest := sha256.New()
	writeUint64 := func(value uint64) {
		var encoded [8]byte
		binary.BigEndian.PutUint64(encoded[:], value)
		_, _ = digest.Write(encoded[:])
	}
	writeString := func(value string) {
		writeUint64(uint64(len(value)))
		_, _ = digest.Write([]byte(value))
	}
	writeUint64(uint64(len(records)))
	for _, record := range records {
		writeString(record.kind)
		writeString(record.physical)
		writeString(record.owner)
		writeUint64(uint64(len(record.locales)))
		for _, locale := range record.locales {
			writeString(locale)
		}
		writeUint64(uint64(len(record.definitions)))
		for _, definition := range record.definitions {
			fingerprint, err := mongoIndexFingerprint([]mongoIndexDefinition{definition})
			if err != nil {
				t.Fatalf("fingerprint MongoDB index %s/%s: %v", record.physical, definition.name, err)
			}
			writeString(definition.identity)
			writeString(fingerprint)
		}
	}
	return hex.EncodeToString(digest.Sum(nil))
}

func createFrozenMongoDBHistory(t *testing.T, directory string) []migrationartifact.File {
	t.Helper()
	initial, indexed, summary := frozenMongoDBManifests(t)
	inputs := []struct {
		name     string
		manifest schema.Manifest
		now      time.Time
	}{
		{name: "initial", manifest: initial, now: time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)},
		{name: "add-title-index", manifest: indexed, now: time.Date(2026, 8, 31, 12, 0, 1, 0, time.UTC)},
		{name: "add-summary", manifest: summary, now: time.Date(2026, 8, 31, 12, 0, 2, 0, time.UTC)},
	}
	for _, input := range inputs {
		if _, err := CreateArtifact(context.Background(), directory, input.name, input.manifest, input.now); err != nil {
			t.Fatalf("create frozen MongoDB fixture %s: %v", input.name, err)
		}
	}
	files, err := migrationartifact.ReadAll(directory)
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func frozenMongoDBManifests(t *testing.T) (schema.Manifest, schema.Manifest, schema.Manifest) {
	t.Helper()
	path, err := query.NewPath("title")
	if err != nil {
		t.Fatal(err)
	}
	initial := schema.NewManifest(schema.Snapshot{
		Version:     schema.CurrentVersion,
		Application: schema.Application{Name: "MongoDB migrations"},
		Collections: []schema.Collection{{
			ID: "posts", Slug: "posts", Labels: schema.CollectionLabels{Singular: "Post", Plural: "Posts"},
			Fields: []schema.Field{{
				ID: "posts-title", Name: "title", Path: path, Type: schema.FieldTypeText,
				Category: schema.FieldCategoryScalar, Text: &schema.TextField{},
			}},
		}},
		Plugins: []schema.Plugin{},
	})
	indexedSnapshot := initial.Snapshot()
	indexedSnapshot.Collections[0].Fields[0].Index = true
	indexed := schema.NewManifest(indexedSnapshot)
	summaryPath, err := query.NewPath("summary")
	if err != nil {
		t.Fatal(err)
	}
	summarySnapshot := indexed.Snapshot()
	summarySnapshot.Collections[0].Fields = append(summarySnapshot.Collections[0].Fields, schema.Field{
		ID: "posts-summary", Name: "summary", Path: summaryPath, Type: schema.FieldTypeText,
		Category: schema.FieldCategoryScalar, Text: &schema.TextField{},
	})
	return initial, indexed, schema.NewManifest(summarySnapshot)
}
