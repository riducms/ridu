package postgres

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/riducms/ridu/internal/migrationartifact"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
)

func TestCollectionRenameArtifactsBindManifestIdentityAndPhysicalTopology(t *testing.T) {
	t.Run("immutable stable ID slug change requires semantic rewrite only", func(t *testing.T) {
		before := renameBindingManifest(schema.Collection{ID: "articles", Slug: "articles", Fields: []schema.Field{}})
		afterSnapshot := before.Snapshot()
		afterSnapshot.Collections[0].Slug = "posts"
		after := schema.NewManifest(afterSnapshot)
		artifact, err := BuildArtifact(context.Background(), "rename-stable-slug", &before, after, nil, false)
		if err != nil {
			t.Fatal(err)
		}
		if !hasStepKind(t, artifact, ridumigration.StepRenameContent) || hasStepKind(t, artifact, ridumigration.StepRetireResources) ||
			artifactHasTableRename(artifact, "articles", "articles") {
			t.Fatalf("stable identity slug change semantic/physical plan: %#v", artifactTestSteps(t, artifact))
		}
		if err := artifact.Validate(); err != nil {
			t.Fatalf("stable identity slug artifact: %v", err)
		}
		if err := validatePostgresResourceRetirementTopology(artifact); err != nil {
			t.Fatalf("stable identity slug preflight: %v", err)
		}
		if _, err := preflightPendingArtifacts(context.Background(), []migrationartifact.File{{Name: artifact.Name, Artifact: artifact}}, RunnerOptions{AllowMaintenance: true}); err != nil {
			t.Fatalf("stable identity slug deterministic preflight: %v", err)
		}
	})

	t.Run("confirmed derived identity rename has exact physical rename", func(t *testing.T) {
		beforeCollection := schema.Collection{ID: "articles", Slug: "articles", Fields: []schema.Field{}}
		afterCollection := schema.Collection{ID: "posts", Slug: "posts", Fields: []schema.Field{}}
		before := renameBindingManifest(beforeCollection)
		after := renameBindingManifest(afterCollection)
		artifact, err := BuildArtifact(context.Background(), "rename-derived-identity", &before, after, []Rename{{
			Kind: RenameCollection, BeforeCollection: beforeCollection, AfterCollection: afterCollection,
		}}, false)
		if err != nil {
			t.Fatal(err)
		}
		if err := artifact.Validate(); err != nil {
			t.Fatalf("confirmed rename artifact: %v", err)
		}
		if err := validatePostgresResourceRetirementTopology(artifact); err != nil {
			t.Fatalf("confirmed rename preflight: %v", err)
		}
		if _, err := preflightPendingArtifacts(context.Background(), []migrationartifact.File{{Name: artifact.Name, Artifact: artifact}}, RunnerOptions{AllowMaintenance: true}); err != nil {
			t.Fatalf("confirmed rename deterministic planner preflight: %v", err)
		}
		if !artifactHasTableRename(artifact, beforeCollection.ID, afterCollection.ID) {
			t.Fatalf("confirmed rename omitted physical table rename: %s", artifactSQL(t, artifact))
		}

		tampered := cloneRetirementArtifact(artifact)
		replaceTableRenameWithSQL(t, &tampered, beforeCollection.ID, afterCollection.ID, "SELECT 1")
		recomputeTestPhysicalDigests(t, &tampered)
		if err := tampered.Validate(); err != nil {
			t.Fatalf("generic validation unexpectedly owns PostgreSQL physical topology: %v", err)
		}
		if err := validatePostgresResourceRetirementTopology(tampered); err == nil || !strings.Contains(err.Error(), "matching physical table rename") {
			t.Fatalf("physical rename tamper preflight = %v", err)
		}

		lineageTampered := cloneRetirementArtifact(artifact)
		insertSQLBeforeCollectionRename(t, &lineageTampered, beforeCollection.Slug, afterCollection.Slug,
			"DROP TABLE "+quote(collectionTable(afterCollection.ID)),
			"CREATE TABLE "+quote(collectionTable(afterCollection.ID))+" (id text PRIMARY KEY)",
		)
		recomputeTestPhysicalDigests(t, &lineageTampered)
		if err := lineageTampered.Validate(); err != nil {
			t.Fatalf("generic target-table replacement validation: %v", err)
		}
		if err := validatePostgresResourceRetirementTopology(lineageTampered); err == nil || !strings.Contains(err.Error(), "additional source/target table data or identity mutation") {
			t.Fatalf("target-table replacement preflight = %v", err)
		}

	})
}

func TestCollectionRenameOrdinarySQLIsBoundToDeterministicAtlasPlan(t *testing.T) {
	beforeCollection := schema.Collection{ID: "articles", Slug: "articles", Fields: []schema.Field{}}
	afterCollection := schema.Collection{ID: "posts", Slug: "posts", Fields: []schema.Field{}}
	before := renameBindingManifest(beforeCollection)
	after := renameBindingManifest(afterCollection)
	original, err := BuildArtifact(context.Background(), "rename-sql-binding", &before, after, []Rename{{
		Kind: RenameCollection, BeforeCollection: beforeCollection, AfterCollection: afterCollection,
	}}, false)
	if err != nil {
		t.Fatal(err)
	}
	target := quote(collectionTable(afterCollection.ID))
	statements := map[string]string{
		"cte update":                   "WITH changed AS (UPDATE " + target + " SET updated_at = updated_at + interval '1 hour' RETURNING 1) SELECT count(*) FROM changed",
		"comment wrapped update":       "/* reviewed physical DDL */ UPDATE " + target + " SET updated_at = now()",
		"procedural update":            "DO $ridu$ BEGIN UPDATE " + target + " SET updated_at = now(); END $ridu$",
		"delete and reinsert same IDs": "WITH removed AS (DELETE FROM " + target + " RETURNING id) INSERT INTO " + target + " (id) SELECT id FROM removed",
	}
	for name, statement := range statements {
		t.Run(name, func(t *testing.T) {
			artifact := cloneRetirementArtifact(original)
			insertSQLBeforeCollectionRename(t, &artifact, beforeCollection.Slug, afterCollection.Slug, statement)
			if name == "cte update" {
				// Planner provenance is review metadata, not an escape hatch from
				// exact regeneration. A forged older version must still fail closed.
				artifact.Planner.Version = "0.0.0-hostile"
			}
			recomputeTestPhysicalDigests(t, &artifact)
			if err := artifact.Validate(); err != nil {
				t.Fatalf("generic hostile fixture validation: %v", err)
			}
			if err := validatePostgresResourceRetirementTopology(artifact); err != nil {
				t.Fatalf("hostile fixture should exercise exact planner binding: %v", err)
			}
			_, err := preflightPendingArtifacts(context.Background(), []migrationartifact.File{{Name: artifact.Name, Artifact: artifact}}, RunnerOptions{AllowMaintenance: true})
			if err == nil || !strings.Contains(err.Error(), CodeMigrationPlanMismatch) {
				t.Fatalf("hostile SQL preflight = %v", err)
			}
		})
	}
}

func TestInitialArtifactOrdinarySQLIsBoundToCompleteDeterministicAtlasPlan(t *testing.T) {
	manifest := renameBindingManifest(schema.Collection{ID: "articles", Slug: "articles", Fields: []schema.Field{}})
	original, err := BuildArtifact(context.Background(), "initial-sql-binding", nil, manifest, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if !artifactHasOrdinarySQL(original) {
		t.Fatal("initial artifact unexpectedly has no ordinary Atlas SQL")
	}
	file := migrationartifact.File{Name: original.Name, Artifact: original}
	if _, err := preflightPendingArtifacts(context.Background(), []migrationartifact.File{file}, RunnerOptions{}); err != nil {
		t.Fatalf("initial deterministic planner preflight: %v", err)
	}

	tampered := cloneRetirementArtifact(original)
	replaced := false
	for phaseIndex := range tampered.Phases {
		for stepIndex := range tampered.Phases[phaseIndex].Steps {
			step := &tampered.Phases[phaseIndex].Steps[stepIndex]
			if step.Kind != ridumigration.StepSQL {
				continue
			}
			step.Payload, err = ridumigration.MarshalStepPayload(ridumigration.SQLPayload{SQL: "SELECT 1"})
			if err != nil {
				t.Fatal(err)
			}
			replaced = true
			break
		}
		if replaced {
			break
		}
	}
	if !replaced {
		t.Fatal("initial artifact has no SQL step to tamper")
	}
	recomputeTestPhysicalDigests(t, &tampered)
	if err := tampered.Validate(); err != nil {
		t.Fatalf("generic initial SQL tamper fixture: %v", err)
	}
	file.Artifact = tampered
	if _, err := preflightPendingArtifacts(context.Background(), []migrationartifact.File{file}, RunnerOptions{}); err == nil || !strings.Contains(err.Error(), "execution phases do not exactly match deterministic Atlas plan") {
		t.Fatalf("initial SQL tamper preflight = %v", err)
	}
}

func TestMatchingPlanFromDifferentAtlasVersionEmitsProvenanceNotice(t *testing.T) {
	manifest := renameBindingManifest(schema.Collection{ID: "articles", Slug: "articles", Fields: []schema.Field{}})
	artifact, err := BuildArtifact(context.Background(), "atlas-provenance", nil, manifest, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	artifact.Planner.Version = "0.99.0-reviewed"
	if err := artifact.Validate(); err != nil {
		t.Fatal(err)
	}
	notices, err := preflightPendingArtifacts(context.Background(), []migrationartifact.File{{Name: artifact.Name, Artifact: artifact}}, RunnerOptions{})
	if err != nil {
		t.Fatalf("matching cross-version plan was rejected: %v", err)
	}
	if len(notices) != 1 || notices[0].Code != NoticeAtlasProvenance {
		t.Fatalf("provenance notices = %#v", notices)
	}
}

func TestStructuredConcurrentIndexPlanIsExactlyBound(t *testing.T) {
	indexed := atlasTextField("posts-title", "title")
	indexed.Index = true
	manifestSnapshot := atlasTestManifest(indexed).Snapshot()
	manifestSnapshot.Plugins = []schema.Plugin{{
		Key: "guard", Version: "1.0.0", GoPackage: "example.com/guard", APIVersion: schema.CurrentPluginAPIVersion,
		Ridu: &schema.PluginCompatibility{Minimum: "0.0.0-dev"},
	}}
	manifest := schema.NewManifest(manifestSnapshot)
	original, err := BuildArtifact(context.Background(), "initial-concurrent-binding", nil, manifest, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	file := migrationartifact.File{Name: original.Name, Artifact: original}
	if _, err := preflightPendingArtifacts(context.Background(), []migrationartifact.File{file}, RunnerOptions{}); err != nil {
		t.Fatalf("untampered concurrent-index preflight: %v", err)
	}

	t.Run("mutated index targets excluded plugin table", func(t *testing.T) {
		artifact := cloneRetirementArtifact(original)
		mutated := false
		for phaseIndex := range artifact.Phases {
			for stepIndex := range artifact.Phases[phaseIndex].Steps {
				step := &artifact.Phases[phaseIndex].Steps[stepIndex]
				if step.Kind != ridumigration.StepConcurrentIndex {
					continue
				}
				var payload ridumigration.ConcurrentIndexPayload
				if err := json.Unmarshal(step.Payload, &payload); err != nil {
					t.Fatal(err)
				}
				if payload.Action != ridumigration.ConcurrentIndexCreate {
					continue
				}
				payload.Name = "hostile_plugin_state_idx"
				payload.Table = "ridu_plugin_guard_state"
				step.Payload, err = ridumigration.MarshalStepPayload(payload)
				if err != nil {
					t.Fatal(err)
				}
				mutated = true
				break
			}
			if mutated {
				break
			}
		}
		if !mutated {
			t.Fatal("initial indexed artifact has no concurrent create to mutate")
		}
		recomputeTestPhysicalDigests(t, &artifact)
		if err := artifact.Validate(); err != nil {
			t.Fatalf("generic mutated concurrent-index fixture: %v", err)
		}
		file.Artifact = artifact
		if _, err := preflightPendingArtifacts(context.Background(), []migrationartifact.File{file}, RunnerOptions{}); err == nil || !strings.Contains(err.Error(), "execution phases do not exactly match deterministic Atlas plan") {
			t.Fatalf("mutated concurrent-index preflight = %v", err)
		}
	})

	t.Run("extra index targets excluded migration ledger", func(t *testing.T) {
		artifact := cloneRetirementArtifact(original)
		payload, err := ridumigration.MarshalStepPayload(ridumigration.ConcurrentIndexPayload{
			Action: ridumigration.ConcurrentIndexCreate, Name: "hostile_migration_digest_idx",
			Table: "ridu_migrations", Method: "BTREE", Parts: []string{quote("artifact_digest")},
		})
		if err != nil {
			t.Fatal(err)
		}
		hostile := ridumigration.Phase{
			ID: "phase-hostile-index", Mode: ridumigration.PhaseNoTransaction,
			PhysicalContractVersion: ridumigration.PhysicalContractVersion,
			Steps: []ridumigration.Step{{
				ID: "step-hostile-index", Kind: ridumigration.StepConcurrentIndex, ExecutorVersion: 1,
				Name: "create hostile migration-ledger index", Payload: payload,
			}},
		}
		assertionPhase := len(artifact.Phases) - 1
		artifact.Phases = append(artifact.Phases, ridumigration.Phase{})
		copy(artifact.Phases[assertionPhase+1:], artifact.Phases[assertionPhase:])
		artifact.Phases[assertionPhase] = hostile
		recomputeTestPhysicalDigests(t, &artifact)
		if err := artifact.Validate(); err != nil {
			t.Fatalf("generic extra concurrent-index fixture: %v", err)
		}
		file.Artifact = artifact
		if _, err := preflightPendingArtifacts(context.Background(), []migrationartifact.File{file}, RunnerOptions{}); err == nil || !strings.Contains(err.Error(), "execution phases do not exactly match deterministic Atlas plan") {
			t.Fatalf("extra concurrent-index preflight = %v", err)
		}
	})
}

func TestPlannerRisksCannotBeRemovedOrDowngraded(t *testing.T) {
	beforeField := atlasTextField("posts-title", "title")
	beforeField.Unique = true
	afterField := atlasTextField("posts-title", "title")
	before := atlasTestManifest(beforeField)
	after := atlasTestManifest(afterField)
	original, err := BuildArtifact(context.Background(), "drop-unique-risk-binding", &before, after, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	riskIndex := -1
	for index, risk := range original.Risks {
		if risk.Code == "RIDU_DROP_UNIQUE_INDEX" && risk.Level == ridumigration.RiskDestructive {
			riskIndex = index
			break
		}
	}
	if riskIndex == -1 {
		t.Fatalf("fixture lacks destructive unique-index risk: %#v", original.Risks)
	}
	file := migrationartifact.File{Name: original.Name, Artifact: original}
	if _, err := preflightPendingArtifacts(context.Background(), []migrationartifact.File{file}, RunnerOptions{}); err != nil {
		t.Fatalf("untampered risk preflight: %v", err)
	}

	for _, variant := range []string{"removed", "downgraded"} {
		t.Run(variant, func(t *testing.T) {
			artifact := cloneRetirementArtifact(original)
			if variant == "removed" {
				artifact.Risks = append(artifact.Risks[:riskIndex], artifact.Risks[riskIndex+1:]...)
			} else {
				artifact.Risks[riskIndex].Level = ridumigration.RiskWarning
			}
			if err := artifact.Validate(); err != nil {
				t.Fatalf("generic risk-tamper fixture: %v", err)
			}
			file.Artifact = artifact
			if _, err := preflightPendingArtifacts(context.Background(), []migrationartifact.File{file}, RunnerOptions{}); err == nil || !strings.Contains(err.Error(), "migration risks do not exactly match deterministic Atlas findings") {
				t.Fatalf("%s risk preflight = %v", variant, err)
			}
		})
	}
}

func TestCollectionRenameTamperingCannotDisguiseResourceRetirement(t *testing.T) {
	before := renameBindingManifest(
		schema.Collection{ID: "keepers", Slug: "keepers", Fields: []schema.Field{}},
		schema.Collection{ID: "retired-a", Slug: "retired-a", Fields: []schema.Field{}},
	)
	after := renameBindingManifest(schema.Collection{ID: "keepers", Slug: "keepers", Fields: []schema.Field{}})
	original, err := BuildArtifact(context.Background(), "rename-retirement-tamper", &before, after, nil, true)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("retirement deletion remains visible to both validators", func(t *testing.T) {
		artifact := cloneRetirementArtifact(original)
		removeRetirementStep(t, &artifact)
		recomputeTestPhysicalDigests(t, &artifact)
		if err := artifact.Validate(); err == nil || !strings.Contains(err.Error(), "resource retirement does not exactly match") {
			t.Fatalf("generic missing-retirement validation = %v", err)
		}
		if err := validatePostgresResourceRetirementTopology(artifact); err == nil || !strings.Contains(err.Error(), "require an exact resource-retirement step") {
			t.Fatalf("PostgreSQL missing-retirement preflight = %v", err)
		}
	})

	t.Run("forged rename to existing survivor is not identity continuity", func(t *testing.T) {
		artifact := cloneRetirementArtifact(original)
		replaceRetirementWithCollectionRename(t, &artifact, ridumigration.Rename{
			CollectionBefore: "retired-a",
			CollectionAfter:  "keepers",
		})
		recomputeTestPhysicalDigests(t, &artifact)
		if !artifactContainsSQL(t, artifact, "DROP TABLE "+quote(collectionTable("retired-a"))) {
			t.Fatal("hostile fixture no longer preserves the reviewed physical drop")
		}
		if err := artifact.Validate(); err == nil || !strings.Contains(err.Error(), "target keepers already existed") {
			t.Fatalf("generic forged-rename validation = %v", err)
		}
		if err := validatePostgresResourceRetirementTopology(artifact); err == nil || !strings.Contains(err.Error(), "target keepers already existed") {
			t.Fatalf("PostgreSQL forged-rename preflight = %v", err)
		}
	})
}

func TestCollectionRenameBindingsAreOneToOne(t *testing.T) {
	beforeA := schema.Collection{ID: "alpha", Slug: "alpha", Fields: []schema.Field{}}
	beforeC := schema.Collection{ID: "charlie", Slug: "charlie", Fields: []schema.Field{}}
	afterB := schema.Collection{ID: "bravo", Slug: "bravo", Fields: []schema.Field{}}
	afterD := schema.Collection{ID: "delta", Slug: "delta", Fields: []schema.Field{}}
	before := renameBindingManifest(beforeA, beforeC)
	after := renameBindingManifest(afterB, afterD)
	original, err := BuildArtifact(context.Background(), "two-collection-renames", &before, after, []Rename{
		{Kind: RenameCollection, BeforeCollection: beforeA, AfterCollection: afterB},
		{Kind: RenameCollection, BeforeCollection: beforeC, AfterCollection: afterD},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := validatePostgresResourceRetirementTopology(original); err != nil {
		t.Fatalf("legitimate one-to-one renames: %v", err)
	}

	artifact := cloneRetirementArtifact(original)
	renames := 0
	for phaseIndex := range artifact.Phases {
		for stepIndex := range artifact.Phases[phaseIndex].Steps {
			step := &artifact.Phases[phaseIndex].Steps[stepIndex]
			if step.Kind != ridumigration.StepRenameContent {
				continue
			}
			renames++
			if renames != 2 {
				continue
			}
			var payload ridumigration.RenamePayload
			if err := json.Unmarshal(step.Payload, &payload); err != nil {
				t.Fatal(err)
			}
			payload.Rename.CollectionAfter = afterB.Slug
			step.Payload, _ = ridumigration.MarshalStepPayload(payload)
		}
	}
	if renames != 2 {
		t.Fatalf("rename steps = %d, want 2", renames)
	}
	recomputeTestPhysicalDigests(t, &artifact)
	if err := artifact.Validate(); err == nil || !strings.Contains(err.Error(), "duplicates rename target") {
		t.Fatalf("generic duplicate-target validation = %v", err)
	}
	if err := validatePostgresResourceRetirementTopology(artifact); err == nil || !strings.Contains(err.Error(), "target bravo is mapped more than once") {
		t.Fatalf("PostgreSQL duplicate-target preflight = %v", err)
	}
}

func TestPlannerTargetNormalizationDoesNotAuthorizeMismatchedRename(t *testing.T) {
	beforeTarget, afterTarget, beforeOwner, afterOwner, ownerFields := crossOwnerReferenceRenameCollections(t)
	afterTarget.Fields = []schema.Field{atlasTextField("posts-title", "title")}
	before := renameBindingManifest(beforeTarget, beforeOwner)
	after := renameBindingManifest(afterTarget, afterOwner)
	artifact, err := BuildArtifact(context.Background(), "mapped-target-shape-mismatch", &before, after, []Rename{
		{Kind: RenameCollection, BeforeCollection: beforeTarget, AfterCollection: afterTarget},
		{Kind: RenameCollection, BeforeCollection: beforeOwner, AfterCollection: afterOwner, Fields: ownerFields},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = preflightPendingArtifacts(context.Background(), []migrationartifact.File{{Name: artifact.Name, Artifact: artifact}}, RunnerOptions{AllowMaintenance: true})
	if err == nil || !strings.Contains(err.Error(), CodeMigrationPlanMismatch) || !strings.Contains(err.Error(), "rename articles -> posts does not exactly match") {
		t.Fatalf("mismatched mapped target preflight = %v", err)
	}
}

func renameBindingManifest(collections ...schema.Collection) schema.Manifest {
	return schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion,
		Application: schema.Application{
			Name: "Rename binding",
		},
		Collections: collections,
		Plugins:     []schema.Plugin{},
	})
}

func artifactHasTableRename(artifact ridumigration.Artifact, beforeID, afterID schema.StableID) bool {
	for _, phase := range artifact.Phases {
		for _, step := range phase.Steps {
			if step.Kind != ridumigration.StepSQL {
				continue
			}
			var payload ridumigration.SQLPayload
			if json.Unmarshal(step.Payload, &payload) == nil && isRenameTableStatement(payload.SQL, collectionTable(beforeID), collectionTable(afterID)) {
				return true
			}
		}
	}
	return false
}

func replaceTableRenameWithSQL(t *testing.T, artifact *ridumigration.Artifact, beforeID, afterID schema.StableID, replacement string) {
	t.Helper()
	for phaseIndex := range artifact.Phases {
		for stepIndex := range artifact.Phases[phaseIndex].Steps {
			step := &artifact.Phases[phaseIndex].Steps[stepIndex]
			if step.Kind != ridumigration.StepSQL {
				continue
			}
			var payload ridumigration.SQLPayload
			if json.Unmarshal(step.Payload, &payload) != nil || !isRenameTableStatement(payload.SQL, collectionTable(beforeID), collectionTable(afterID)) {
				continue
			}
			step.Payload, _ = ridumigration.MarshalStepPayload(ridumigration.SQLPayload{SQL: replacement})
			return
		}
	}
	t.Fatal("matching physical table rename not found")
}

func insertSQLBeforeCollectionRename(t *testing.T, artifact *ridumigration.Artifact, before, after schema.CollectionSlug, statements ...string) {
	t.Helper()
	for phaseIndex := range artifact.Phases {
		phase := &artifact.Phases[phaseIndex]
		for stepIndex, step := range phase.Steps {
			if step.Kind != ridumigration.StepRenameContent {
				continue
			}
			var payload ridumigration.RenamePayload
			if json.Unmarshal(step.Payload, &payload) != nil || payload.Rename.CollectionBefore != before || payload.Rename.CollectionAfter != after || payload.Rename.FieldBefore != "" {
				continue
			}
			inserted := make([]ridumigration.Step, 0, len(statements))
			for index, statement := range statements {
				encoded, err := ridumigration.MarshalStepPayload(ridumigration.SQLPayload{SQL: statement})
				if err != nil {
					t.Fatal(err)
				}
				inserted = append(inserted, ridumigration.Step{
					ID: "step-hostile-lineage-" + string(rune('a'+index)), Kind: ridumigration.StepSQL,
					ExecutorVersion: 1, Name: "hostile table lineage replacement", Payload: encoded,
				})
			}
			updated := make([]ridumigration.Step, 0, len(phase.Steps)+len(inserted))
			updated = append(updated, phase.Steps[:stepIndex]...)
			updated = append(updated, inserted...)
			updated = append(updated, phase.Steps[stepIndex:]...)
			phase.Steps = updated
			return
		}
	}
	t.Fatal("collection content rename not found")
}

func removeRetirementStep(t *testing.T, artifact *ridumigration.Artifact) {
	t.Helper()
	for phaseIndex := range artifact.Phases {
		for stepIndex, step := range artifact.Phases[phaseIndex].Steps {
			if step.Kind != ridumigration.StepRetireResources {
				continue
			}
			artifact.Phases[phaseIndex].Steps = append(artifact.Phases[phaseIndex].Steps[:stepIndex], artifact.Phases[phaseIndex].Steps[stepIndex+1:]...)
			return
		}
	}
	t.Fatal("resource-retirement step not found")
}

func replaceRetirementWithCollectionRename(t *testing.T, artifact *ridumigration.Artifact, rename ridumigration.Rename) {
	t.Helper()
	payload, err := ridumigration.MarshalStepPayload(ridumigration.RenamePayload{Rename: rename})
	if err != nil {
		t.Fatal(err)
	}
	for phaseIndex := range artifact.Phases {
		for stepIndex := range artifact.Phases[phaseIndex].Steps {
			step := &artifact.Phases[phaseIndex].Steps[stepIndex]
			if step.Kind != ridumigration.StepRetireResources {
				continue
			}
			step.Kind = ridumigration.StepRenameContent
			step.Name = "forge removed resource into existing survivor"
			step.Payload = payload
			return
		}
	}
	t.Fatal("resource-retirement step not found")
}
