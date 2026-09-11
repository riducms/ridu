package sqlite

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// These artifacts were created by the planner at 35188a1a6b2b, before B2's
// presentation projection. Keep the fixture bytes frozen: recreating them with
// the current planner would omit the historically required transformed risk.
func TestFrozenSQLiteTransformedPresentationHistory(t *testing.T) {
	ctx := context.Background()
	fixture := filepath.Join("testdata", "historical-transformed-presentation")
	files, err := migrationartifact.ReadAll(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || !sqliteArtifactAllowsTransformedSchema(files[1].Artifact) {
		t.Fatalf("historical transformed history = %#v", files)
	}
	wantDigests := []string{
		"b9e26f0890b78d33f48ee0708616e2dbb5105341fa101d596e233364eb0185ea",
		"72f4df1c15450376bf73f26267f65914f7c18a1c18afb6dd6de4f7d6c3521bed",
	}
	for i, file := range files {
		if file.Digest != wantDigests[i] || file.Artifact.Planner.Version != "1.1.0" {
			t.Fatalf("historical artifact %s changed: digest %s, planner %s", file.Name, file.Digest, file.Artifact.Planner.Version)
		}
	}
	directory := t.TempDir()
	original := make(map[string][]byte)
	for _, file := range files {
		encoded, err := os.ReadFile(file.Path)
		if err != nil {
			t.Fatal(err)
		}
		original[file.Name] = encoded
	}
	if err := os.WriteFile(filepath.Join(directory, files[0].Name), original[files[0].Name], 0o600); err != nil {
		t.Fatal(err)
	}
	backend := newSQLiteMigrationStore(t)
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	config := ridu.Config{Name: "Legacy labels", Collections: []ridu.Collection{{Slug: "notes", Fields: field.Fields{field.Text("body")}}}}
	application, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Create(ctx, "notes", store.Values{"body": store.String("Existing content")}, ridu.MutationOptions{}); err != nil {
		t.Fatal(err)
	}
	rows := sqlitePresentationRows(t, backend, []string{"ridu_documents"})
	if err := os.WriteFile(filepath.Join(directory, files[1].Name), original[files[1].Name], 0o600); err != nil {
		t.Fatal(err)
	}
	descriptor := migration.DataTransformDescriptor{Name: "reviewed-label-change", Checksum: migration.DataTransformChecksum([]byte("noop-label-v1"))}
	upCalls := 0
	transform := migration.DataTransform{DataTransformDescriptor: descriptor,
		Up:   func(context.Context, migration.DataTransaction) error { upCalls++; return nil },
		Down: func(context.Context, migration.DataTransaction) error { return nil },
	}
	if err := VerifyArtifacts(ctx, directory); err == nil {
		t.Fatal("historical descriptor accepted without its compiled callback")
	}
	if err := VerifyArtifacts(ctx, directory, transform); err != nil {
		t.Fatalf("historical shadow replay: %v", err)
	}
	upCalls = 0
	if err := backend.ApplyArtifacts(ctx, directory, transform); err != nil {
		t.Fatalf("apply historical label migration to existing content: %v", err)
	}
	if err := backend.ApplyArtifacts(ctx, directory, transform); err != nil {
		t.Fatalf("recheck already applied historical ledger: %v", err)
	}
	if upCalls != 1 {
		t.Fatalf("historical callback executions = %d, want 1", upCalls)
	}
	latest, err := files[1].Artifact.AfterManifest()
	if err != nil {
		t.Fatal(err)
	}
	status, err := backend.ArtifactStatus(ctx, directory, latest)
	if err != nil || len(status) != 2 || !status[1].Applied {
		t.Fatalf("historical status = %#v, %v", status, err)
	}
	sqlitePresentationReady(t, backend, directory, latest)
	config.Collections[0].Fields = field.Fields{field.Text("body").Label("Contents"), field.Text("summary").Index()}
	next, err := ridu.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	created, err := CreateArtifact(ctx, directory, "add-summary", next, time.Unix(3, 0), false)
	if err != nil {
		t.Fatalf("extend historical transformed history: %v", err)
	}
	if err := VerifyArtifacts(ctx, directory, transform); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory, transform); err != nil {
		t.Fatal(err)
	}
	status, err = backend.ArtifactStatus(ctx, directory, next)
	if err != nil || len(status) != 3 || !status[2].Applied || status[2].Checksum != created.Checksum {
		t.Fatalf("extended history status = %#v, %v", status, err)
	}
	sqlitePresentationReady(t, backend, directory, next)
	if got := sqlitePresentationRows(t, backend, []string{"ridu_documents"}); !reflect.DeepEqual(rows, got) {
		t.Fatal("historical label and optional-field migrations changed existing content")
	}
	for name, before := range original {
		after, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil || string(before) != string(after) {
			t.Fatalf("historical artifact %s changed: %v", name, err)
		}
	}
}

func TestSQLiteHistoricalPresentationReconstructionRejectsUnplannedRisks(t *testing.T) {
	ctx := context.Background()
	fixture := filepath.Join("testdata", "historical-transformed-presentation")
	files, err := migrationartifact.ReadAll(fixture)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []string{"risk message", "extra risk", "missing transform"} {
		t.Run(change, func(t *testing.T) {
			directory := t.TempDir()
			if err := os.CopyFS(directory, os.DirFS(fixture)); err != nil {
				t.Fatal(err)
			}
			encoded, err := os.ReadFile(files[1].Path)
			if err != nil {
				t.Fatal(err)
			}
			artifact, err := migration.DecodeArtifact(encoded)
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "risk message":
				artifact.Risks[0].Message = "Edited historical warning"
			case "extra risk":
				artifact.Risks = append(artifact.Risks, migration.Risk{Code: "UNPLANNED", Level: migration.RiskDestructive, Message: "Unplanned risk"})
			case "missing transform":
				phase := &artifact.Phases[0]
				phase.Steps = phase.Steps[1:]
				phase.Steps[0].ID = "step-0001"
				phase.AfterPhysicalDigest, err = migration.PhasePhysicalDigest(phase.BeforePhysicalDigest, phase.Mode, phase.Steps)
				if err != nil {
					t.Fatal(err)
				}
			}
			encoded, err = json.MarshalIndent(artifact, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(directory, files[1].Name), encoded, 0o600); err != nil {
				t.Fatal(err)
			}
			wantError := "does not match planner"
			if change == "missing transform" {
				wantError = "against planner"
			}
			if err := VerifyArtifacts(ctx, directory); err == nil || !strings.Contains(err.Error(), wantError) {
				t.Fatalf("unplanned historical artifact accepted: %v", err)
			}
		})
	}

	// A transform attached to an ordinary optional-field addition never required
	// this risk under either comparison. A recorded marker cannot force a new
	// planner branch merely because it names a known risk.
	before, err := files[0].Artifact.AfterManifest()
	if err != nil {
		t.Fatal(err)
	}
	config := ridu.Config{Name: "Legacy labels", Collections: []ridu.Collection{{Slug: "notes", Fields: field.Fields{field.Text("body"), field.Text("summary")}}}}
	after, err := ridu.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	descriptor := migration.DataTransformDescriptor{Name: "reviewed-label-change", Checksum: migration.DataTransformChecksum([]byte("noop-label-v1"))}
	artifact, err := planArtifact(ctx, "optional", &before, after, false, descriptor)
	if err != nil {
		t.Fatal(err)
	}
	artifact.Risks = append(artifact.Risks, files[1].Artifact.Risks...)
	directory := t.TempDir()
	if _, err := CreateArtifact(ctx, directory, "initial", before, time.Unix(1, 0), false); err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "optional", artifact, time.Unix(2, 0)); err != nil {
		t.Fatal(err)
	}
	if err := VerifyArtifacts(ctx, directory); err == nil || !strings.Contains(err.Error(), "does not match planner") {
		t.Fatalf("forged transformed-schema classification accepted: %v", err)
	}
}

func TestSQLitePresentationPreservesJoinAndEndpointOperations(t *testing.T) {
	config := ridu.Config{Name: "Operational boundaries", Endpoints: []ridu.Endpoint{{Method: "GET", Path: "/example", Summary: "Example", Handler: func(ridu.EndpointContext) {}}}, Collections: []ridu.Collection{
		{Slug: "authors", Versions: true, Fields: field.Fields{field.Text("name"), field.Join("posts", "posts", "author")}},
		{Slug: "posts", Fields: field.Fields{field.Text("title"), field.Relationship("author", "authors")}},
	}}
	before, err := ridu.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*schema.Snapshot){
		"join limit":    func(s *schema.Snapshot) { s.Collections[0].Fields[1].Join.Limit++ },
		"join sort":     func(s *schema.Snapshot) { s.Collections[0].Fields[1].Join.DefaultSort = "-title" },
		"endpoint path": func(s *schema.Snapshot) { s.Application.Endpoints[0].Path = "/changed" },
		"endpoint verb": func(s *schema.Snapshot) { s.Application.Endpoints[0].Method = "POST" },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			snapshot := before.Snapshot()
			snapshot.Application.Endpoints[0].Summary = "Cosmetic description"
			snapshot.Collections[0].Fields[1].Join.DefaultColumns = []string{"title"}
			change(&snapshot)
			after := schema.NewManifest(snapshot)
			for _, allow := range []bool{false, true} {
				if _, err := planArtifact(context.Background(), "operational", &before, after, allow); err == nil {
					t.Fatalf("operational transition accepted (allow-destructive=%v)", allow)
				}
			}
		})
	}
}
