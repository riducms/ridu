package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	"ariga.io/atlas/sql/migrate"
	atlaspostgres "ariga.io/atlas/sql/postgres"
	_ "ariga.io/atlas/sql/postgres/postgrescheck"
	atlasschema "ariga.io/atlas/sql/schema"
	"ariga.io/atlas/sql/sqlcheck"
	"github.com/riducms/ridu/internal/blockgraph"
	"github.com/riducms/ridu/internal/fieldchange"
	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/internal/postgresmigration"
	"github.com/riducms/ridu/internal/primitivefield"
	"github.com/riducms/ridu/internal/requiredfield"
	"github.com/riducms/ridu/internal/schemadiff"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
)

// SafetyError reports a valid schema transition that requires explicit
// destructive approval or a semantic migration Ridu cannot prove safe.
type SafetyError struct {
	Risks []ridumigration.Risk
}

func (err *SafetyError) Error() string {
	messages := make([]string, len(err.Risks))
	for index, risk := range err.Risks {
		messages[index] = risk.Message
	}
	return "migration requires explicit safety resolution: " + strings.Join(messages, "; ")
}

// BuildArtifact uses Atlas to plan all physical PostgreSQL changes and adds
// ordered Ridu semantic steps for explicitly confirmed rename intent. It binds
// compiled data transforms exactly as the runner regenerates them.
func BuildArtifact(ctx context.Context, name string, before *schema.Manifest, after schema.Manifest, renames []Rename, allowDestructive bool, transforms ...ridumigration.DataTransformDescriptor) (ridumigration.Artifact, error) {
	return planArtifact(ctx, name, before, after, renames, allowDestructive, transforms...)
}

// validateTransformColumnCasts refuses a transform-backed
// change of a column to a type PostgreSQL cannot cast to automatically. The
// physical change runs before the transform, so such an artifact could never
// apply. Every column type casts to text.
func validateTransformColumnCasts(before, after schema.Snapshot) error {
	targets := make(map[schema.StableID]schema.Collection)
	for _, resources := range [][]schema.Collection{after.Collections, after.Globals} {
		for _, resource := range resources {
			targets[resource.ID] = resource
		}
	}
	for _, resources := range [][]schema.Collection{before.Collections, before.Globals} {
		for _, previous := range resources {
			current, exists := targets[previous.ID]
			if !exists {
				continue
			}
			fields := make(map[schema.StableID]schema.Field, len(current.Fields))
			for _, field := range current.Fields {
				fields[field.ID] = field
			}
			for _, field := range previous.Fields {
				changed, exists := fields[field.ID]
				if !exists || field.Localized != changed.Localized {
					continue
				}
				from, to := columnType(field), columnType(changed)
				if from != to && to != "text" {
					return fmt.Errorf("RIDU_FIELD_KIND_CHANGE_REQUIRES_TRANSFORM: PostgreSQL cannot convert %s.%s from %s to %s in place, and a data transform runs after the column changes; add a field with the new kind, copy the values with the data transform, and remove the old field in a later migration", previous.Slug, field.Path.String(), from, to)
				}
			}
		}
	}
	return nil
}

// orderBlockFieldRenames keeps resource renames first, in order, then orders
// block field renames so a definition's renames precede those of the
// definitions it places. Each content step finds its blocks through the after
// schema, so every container above them already has its after name.
func orderBlockFieldRenames(after schema.Snapshot, renames []Rename) []Rename {
	var resources, blocks []Rename
	for _, rename := range renames {
		if rename.Kind == RenameBlockField {
			blocks = append(blocks, rename)
		} else {
			resources = append(resources, rename)
		}
	}
	if len(blocks) == 0 {
		return renames
	}
	depths := blockgraph.New(after).Depths()
	sort.SliceStable(blocks, func(left, right int) bool { return depths[blocks[left].Block] < depths[blocks[right].Block] })
	return append(resources, blocks...)
}

// validateFieldRenamesOnlyRename refuses a confirmed field rename that changes
// more than the field's name; see schemadiff.ValidateFieldRenameOnly. A
// change of the field's own localization would replace its columns and drop
// the values the rename was confirmed to keep.
func validateFieldRenamesOnlyRename(renames []Rename) error {
	for _, rename := range renames {
		if rename.Kind != RenameField && rename.Kind != RenameBlockField || rename.BeforeField == nil || rename.AfterField == nil {
			continue
		}
		if err := schemadiff.ValidateFieldRenameOnly(*rename.BeforeField, *rename.AfterField); err != nil {
			return err
		}
	}
	return nil
}

// CreatedArtifact is the immutable file and plan produced by CreateArtifact.
type CreatedArtifact struct {
	Path     string
	Name     string
	Checksum string
	Artifact ridumigration.Artifact
}

// CreateArtifact derives the predecessor from committed history and writes a
// deterministic artifact for the current PostgreSQL planner.
func CreateArtifact(ctx context.Context, directory, name string, after schema.Manifest, now time.Time, renames []Rename, allowDestructive bool, transforms ...ridumigration.DataTransformDescriptor) (CreatedArtifact, error) {
	files, err := migrationartifact.ReadAll(directory)
	if err != nil {
		return CreatedArtifact{}, err
	}
	if err := validatePostgresSemanticHistory(files); err != nil {
		return CreatedArtifact{}, err
	}
	if err := validatePendingArtifactInspection(ctx, files); err != nil {
		return CreatedArtifact{}, err
	}
	if err := validatePostgresDataTransformIdentities(files, transforms); err != nil {
		return CreatedArtifact{}, err
	}
	var before *schema.Manifest
	if len(files) != 0 {
		latest, err := files[len(files)-1].Artifact.AfterManifest()
		if err != nil {
			return CreatedArtifact{}, err
		}
		before = &latest
	}
	artifact, err := BuildArtifact(ctx, name, before, after, renames, allowDestructive, transforms...)
	if err != nil {
		return CreatedArtifact{}, err
	}
	file, err := migrationartifact.Create(directory, name, artifact, now)
	if err != nil {
		return CreatedArtifact{}, err
	}
	return CreatedArtifact{Path: file.Path, Name: file.Name, Checksum: file.Digest, Artifact: file.Artifact}, nil
}

// planArtifact is the deterministic planner shared by artifact creation and
// the runner's exact regeneration of committed artifacts.
func planArtifact(ctx context.Context, name string, before *schema.Manifest, after schema.Manifest, renames []Rename, allowDestructive bool, transforms ...ridumigration.DataTransformDescriptor) (ridumigration.Artifact, error) {
	if err := validateFieldRenamesOnlyRename(renames); err != nil {
		return ridumigration.Artifact{}, err
	}
	renames = orderBlockFieldRenames(after.Snapshot(), renames)
	if err := primitivefield.ValidateManifestIndexes(after); err != nil {
		return ridumigration.Artifact{}, err
	}
	if err := validatePostgresDataTransformDescriptors(transforms); err != nil {
		return ridumigration.Artifact{}, err
	}
	if before != nil {
		if len(transforms) != 0 {
			if err := validateTransformColumnCasts(before.Snapshot(), after.Snapshot()); err != nil {
				return ridumigration.Artifact{}, err
			}
		}
		var err error
		renames, err = withStableCollectionSlugRenames(before.Snapshot(), after.Snapshot(), renames)
		if err != nil {
			return ridumigration.Artifact{}, err
		}
		if err := schemadiff.RejectVersionsEnable(before.Snapshot(), after.Snapshot(), postgresCollectionRenameIDs(renames)); err != nil {
			return ridumigration.Artifact{}, err
		}
		if len(transforms) == 0 {
			if err := primitivefield.ValidateEvolution(before.Snapshot(), after.Snapshot()); err != nil {
				return ridumigration.Artifact{}, err
			}
		}
		if err := postgresmigration.ValidateVersionedTransition(before.Snapshot(), after.Snapshot(), transforms); err != nil {
			return ridumigration.Artifact{}, err
		}
	}
	artifact, err := ridumigration.NewArtifact(name, atlasPlanner(), before, after)
	if err != nil {
		return ridumigration.Artifact{}, err
	}
	// Newly required columns are added or kept nullable until the audit, which
	// runs after every data transform, has proved that stored rows hold values.
	var requirements []requiredfield.Requirement
	relaxed := after
	if before != nil {
		requirements = requiredfield.Detect(before.Snapshot(), after.Snapshot(), postgresRequiredRenames(renames))
		relaxed = relaxRequiredColumns(after, requirements)
	}
	var operations []ridumigration.Operation
	if before != nil {
		if err := validatePlannedCollectionSlugRewriteNonOverlap(renames); err != nil {
			return ridumigration.Artifact{}, err
		}
		if blocked := unsafeUploadCollectionRemovals(before.Snapshot(), after.Snapshot(), renames); len(blocked) != 0 {
			// Generic schema artifacts cannot atomically delete or transfer objects
			// in an external backend. Unlike ordinary SQL destruction, this is not
			// made safe by acknowledging --allow-destructive.
			return ridumigration.Artifact{}, &SafetyError{Risks: blocked}
		}
		if blocked := unsafeCapabilityDisables(before.Snapshot(), after.Snapshot(), renames); len(blocked) != 0 {
			return ridumigration.Artifact{}, &SafetyError{Risks: blocked}
		}
	}
	if before == nil {
		changes, err := atlaspostgres.DefaultDiff.SchemaDiff(emptyAtlasSchema(), atlasSchema(after, atlasIdentityMap{}))
		if err != nil {
			return ridumigration.Artifact{}, fmt.Errorf("Atlas initial schema diff: %w", err)
		}
		steps, risks, err := atlasSteps(ctx, name, changes)
		if err != nil {
			return ridumigration.Artifact{}, err
		}
		operations = append(operations, steps...)
		artifact.Risks = append(artifact.Risks, physicalChangeRisks(changes)...)
		artifact.Risks = append(artifact.Risks, risks...)
	} else {
		mapping, err := atlasRenameMapping(renames)
		if err != nil {
			return ridumigration.Artifact{}, err
		}
		fieldMapping, err := referenceShapeMappingFromRenames(renames)
		if err != nil {
			return ridumigration.Artifact{}, err
		}
		if len(transforms) == 0 {
			if err := validatePostgresEmbeddedEvolution(before.Snapshot(), after.Snapshot(), fieldMapping, mapping); err != nil {
				return ridumigration.Artifact{}, err
			}
			if path := tightenedBlockBounds(before.Snapshot(), after.Snapshot(), fieldMapping, mapping); path != "" {
				return ridumigration.Artifact{}, fmt.Errorf("PostgreSQL block bounds at %q were tightened; register a compiled data transform that validates or repairs existing values before adopting the new bounds", path)
			}
		}
		renameSteps, renameRisks, err := atlasRenameSteps(ctx, name, *before, mapping, renames)
		if err != nil {
			return ridumigration.Artifact{}, err
		}
		operations = append(operations, renameSteps...)
		artifact.Risks = append(artifact.Risks, renameRisks...)
		for _, rename := range renames {
			intent := renameIntent(rename)
			operations = append(operations, ridumigration.Operation{Kind: ridumigration.StepRenameContent, Name: renameStepName(rename), Rename: &intent})
		}

		changes, err := atlaspostgres.DefaultDiff.SchemaDiff(atlasSchema(*before, mapping), atlasSchema(relaxed, atlasIdentityMap{}))
		if err != nil {
			return ridumigration.Artifact{}, fmt.Errorf("Atlas schema diff: %w", err)
		}
		steps, risks, err := atlasSteps(ctx, name, changes)
		if err != nil {
			return ridumigration.Artifact{}, err
		}
		removed := removedResourceIDs(before.Snapshot(), after.Snapshot(), mapping)
		var purgeVersionOwners []schema.StableID
		if len(removed) != 0 {
			var blocked []ridumigration.Risk
			purgeVersionOwners, blocked = resourceRetirementReferencePolicy(before.Snapshot(), after.Snapshot(), mapping, steps, removed)
			if len(blocked) != 0 {
				return ridumigration.Artifact{}, &SafetyError{Risks: blocked}
			}
		}
		if blocked := unsafeReferenceShapeDecreases(
			before.Snapshot(), after.Snapshot(), mapping, fieldMapping,
			steps, removed, purgeVersionOwners,
		); len(blocked) != 0 {
			// Acknowledging ordinary SQL destruction cannot prove that dormant
			// current JSON/scalar values and version snapshots were retired. The
			// only generic safe case is the existing same-artifact resource
			// retirement contract: the complete physical root is dropped and the
			// owner's complete version history is purged transactionally.
			return ridumigration.Artifact{}, &SafetyError{Risks: blocked}
		}
		if len(transforms) == 0 {
			if err := fieldchange.RequireTransform(before.Snapshot(), after.Snapshot()); err != nil {
				return ridumigration.Artifact{}, &SafetyError{Risks: []ridumigration.Risk{{
					Code: "RIDU_FIELD_KIND_CHANGE_REQUIRES_TRANSFORM", Level: ridumigration.RiskDestructive, Message: err.Error(),
				}}}
			}
		}
		if len(removed) != 0 {
			steps, err = insertResourceRetirementBeforeDrop(steps, removed, purgeVersionOwners)
			if err != nil {
				return ridumigration.Artifact{}, err
			}
			artifact.Risks = append(artifact.Risks, ridumigration.Risk{
				Code: "RIDU_RETIRE_RESOURCE_STATE", Level: ridumigration.RiskDestructive,
				Message: fmt.Sprintf("permanently retire framework-owned authentication, preference, version, job, task, lock, and reference state for removed resources %s before dropping their storage", joinStableIDs(removed)),
			})
			if len(purgeVersionOwners) != 0 {
				artifact.Risks = append(artifact.Risks, ridumigration.Risk{
					Code: "RIDU_RETIRE_DEPENDENT_VERSION_HISTORY", Level: ridumigration.RiskDestructive,
					Message: fmt.Sprintf("permanently purge complete version histories for surviving resources %s because their removed reference fields could otherwise restore retired identities", joinStableIDs(purgeVersionOwners)),
				})
			}
		}
		operations = append(operations, steps...)
		artifact.Risks = append(artifact.Risks, physicalChangeRisks(changes)...)
		artifact.Risks = append(artifact.Risks, risks...)
	}
	afterSnapshot := after.Snapshot()
	if before != nil && referenceIndexTopologyChanged(before.Snapshot(), afterSnapshot) {
		operations = append(operations, ridumigration.Operation{
			Kind: ridumigration.StepBackfillReferences, Name: "rebuild current document reference index",
		})
		artifact.Risks = append(artifact.Risks, ridumigration.Risk{
			Code: "RIDU_REFERENCE_INDEX_REBUILD", Level: ridumigration.RiskWarning,
			Message: "rebuild the derived current-document reference index with a whole-dataset scan of active and trashed content; coordinate this migration with deployment traffic",
		})
	}
	if before != nil && artifact.FromDigest == artifact.ToDigest && len(transforms) == 0 {
		return ridumigration.Artifact{}, fmt.Errorf("%w; no migration steps were planned", migrationartifact.ErrSchemaCurrent)
	}
	artifact.Risks = append(artifact.Risks, requiredfield.Risks(requirements)...)
	artifact.Risks = normalizeRisks(artifact.Risks)
	if !allowDestructive {
		var blocked []ridumigration.Risk
		for _, risk := range artifact.Risks {
			if risk.Level == ridumigration.RiskDestructive {
				blocked = append(blocked, risk)
			}
		}
		if len(blocked) != 0 {
			return ridumigration.Artifact{}, &SafetyError{Risks: blocked}
		}
	}
	if len(requirements) != 0 {
		// The audit and the NOT NULL constraints it admits share the final
		// transaction phase, after every data transform; a failure rolls them
		// back together.
		tighten, err := atlaspostgres.DefaultDiff.SchemaDiff(atlasSchema(relaxed, atlasIdentityMap{}), atlasSchema(after, atlasIdentityMap{}))
		if err != nil {
			return ridumigration.Artifact{}, fmt.Errorf("Atlas required-column diff: %w", err)
		}
		constraints, _, err := atlasSteps(ctx, name, tighten)
		if err != nil {
			return ridumigration.Artifact{}, err
		}
		operations = append(operations, ridumigration.Operation{
			Kind: ridumigration.StepAuditRequiredValues, Name: requiredfield.StepName, RequiredFields: requiredfield.Addresses(requirements),
		})
		operations = append(operations, constraints...)
	}
	operations = append(operations, ridumigration.Operation{Kind: ridumigration.StepAssertSchema, Name: "verify resulting PostgreSQL schema"})
	artifact.Phases, err = phasesFromOperations(artifact.FromDigest, operations)
	if err != nil {
		return ridumigration.Artifact{}, err
	}
	artifact, err = postgresmigration.BindDataTransforms(artifact, transforms)
	if err != nil {
		return ridumigration.Artifact{}, err
	}
	if err := artifact.Validate(); err != nil {
		return ridumigration.Artifact{}, err
	}
	return artifact, nil
}

// withStableCollectionSlugRenames makes a public slug change on an immutable
// collection identity an explicit content migration. Physical storage keeps
// the same table, but polymorphic and plugin-owned JSON stores public slugs;
// leaving the old value dormant would let a later collection reuse bind it to
// the wrong identity. The rewrite is deterministic and does not require human
// rename inference because the stable ID already proves continuity.
func withStableCollectionSlugRenames(before, after schema.Snapshot, renames []Rename) ([]Rename, error) {
	beforeByID := make(map[schema.StableID]schema.Collection, len(before.Collections))
	for _, collection := range before.Collections {
		beforeByID[collection.ID] = collection
	}
	afterByID := make(map[schema.StableID]schema.Collection, len(after.Collections))
	for _, collection := range after.Collections {
		afterByID[collection.ID] = collection
	}

	result := append([]Rename(nil), renames...)
	declared := make(map[schema.StableID]int)
	for _, rename := range result {
		if rename.Kind != RenameCollection || rename.BeforeCollection.ID != rename.AfterCollection.ID {
			continue
		}
		previous, beforeExists := beforeByID[rename.BeforeCollection.ID]
		next, afterExists := afterByID[rename.AfterCollection.ID]
		if !beforeExists || !afterExists || previous.Slug == next.Slug ||
			!reflect.DeepEqual(rename.BeforeCollection, previous) || !reflect.DeepEqual(rename.AfterCollection, next) ||
			len(rename.Fields) != 0 || rename.BeforeField != nil || rename.AfterField != nil {
			return nil, fmt.Errorf("RIDU_COLLECTION_SLUG_REWRITE_MISMATCH: same-identity collection rename %s -> %s does not exactly match one slug-only manifest transition", rename.BeforeCollection.Slug, rename.AfterCollection.Slug)
		}
		declared[previous.ID]++
		if declared[previous.ID] != 1 {
			return nil, fmt.Errorf("RIDU_COLLECTION_SLUG_REWRITE_MISMATCH: same-identity collection %s has more than one slug rewrite", previous.ID)
		}
	}

	var ids []schema.StableID
	for id, previous := range beforeByID {
		if next, exists := afterByID[id]; exists && previous.Slug != next.Slug {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(left, right int) bool { return ids[left] < ids[right] })
	for _, id := range ids {
		if declared[id] != 0 {
			continue
		}
		result = append(result, Rename{
			Kind: RenameCollection, BeforeCollection: beforeByID[id], AfterCollection: afterByID[id],
		})
	}
	return result, nil
}

func unsafeUploadCollectionRemovals(before, after schema.Snapshot, renames []Rename) []ridumigration.Risk {
	afterByID := make(map[schema.StableID]schema.Collection, len(after.Collections))
	for _, collection := range after.Collections {
		afterByID[collection.ID] = collection
	}
	renameTargets := make(map[schema.StableID]schema.StableID)
	for _, rename := range renames {
		if rename.Kind == RenameCollection {
			renameTargets[rename.BeforeCollection.ID] = rename.AfterCollection.ID
		}
	}
	var risks []ridumigration.Risk
	for _, previous := range before.Collections {
		if previous.Upload == nil && !previous.Capabilities.Upload {
			continue
		}
		next, exists := afterByID[previous.ID]
		if targetID, renamed := renameTargets[previous.ID]; !exists && renamed {
			next, exists = afterByID[targetID]
		}
		settingsDisabled := previous.Upload != nil && next.Upload == nil
		flagDisabled := previous.Capabilities.Upload && !next.Capabilities.Upload
		if exists && !settingsDisabled && !flagDisabled {
			continue
		}
		risks = append(risks, ridumigration.Risk{
			Code:    "RIDU_UPLOAD_COLLECTION_REMOVAL_UNSAFE",
			Level:   ridumigration.RiskDestructive,
			Message: fmt.Sprintf("removing upload capability from collection %s would discard database ownership metadata while external objects may remain; generic artifacts cannot verify object-store retirement, so retain the collection and perform migration/deletion through a separately reviewed application-owned lifecycle", previous.Slug),
		})
	}
	return risks
}

func unsafeCapabilityDisables(before, after schema.Snapshot, renames []Rename) []ridumigration.Risk {
	afterCollections := make(map[schema.StableID]schema.Collection, len(after.Collections))
	for _, collection := range after.Collections {
		afterCollections[collection.ID] = collection
	}
	renameTargets := postgresCollectionRenameIDs(renames)
	afterGlobals := make(map[schema.StableID]schema.Global, len(after.Globals))
	for _, global := range after.Globals {
		afterGlobals[global.ID] = global
	}
	var risks []ridumigration.Risk
	inspect := func(previous, next schema.Collection) {
		add := func(code, capability, state string) {
			risks = append(risks, ridumigration.Risk{
				Code: code, Level: ridumigration.RiskDestructive,
				Message: fmt.Sprintf("disabling %s on surviving resource %s would retain %s that can reactivate if the capability is re-enabled; migrate or explicitly retire that state through a separately reviewed lifecycle before changing the capability", capability, previous.ID, state),
			})
		}
		if (previous.Auth != nil && next.Auth == nil) || (previous.Capabilities.Auth && !next.Capabilities.Auth) {
			add("RIDU_AUTH_DISABLE_STATE_UNSAFE", "authentication", "credentials, sessions, tokens, API keys, and preferences")
		} else if previous.Auth != nil && next.Auth != nil {
			if previous.Auth.APIKeys && !next.Auth.APIKeys {
				add("RIDU_API_KEYS_DISABLE_STATE_UNSAFE", "API keys", "API-key rows")
			}
			if previous.Auth.PasswordReset && !next.Auth.PasswordReset {
				add("RIDU_PASSWORD_RESET_DISABLE_STATE_UNSAFE", "password reset", "password-reset tokens")
			}
			if previous.Auth.VerifyEmail && !next.Auth.VerifyEmail {
				add("RIDU_VERIFY_EMAIL_DISABLE_STATE_UNSAFE", "email verification", "verification tokens")
			}
		}
		if (previous.Versions != nil && next.Versions == nil) || (previous.Capabilities.Versions && !next.Capabilities.Versions) {
			add("RIDU_VERSIONS_DISABLE_STATE_UNSAFE", "versions", "historical version rows")
		} else if previous.Versions != nil && next.Versions != nil && previous.Versions.Drafts && !next.Versions.Drafts {
			add("RIDU_DRAFTS_DISABLE_STATE_UNSAFE", "drafts", "draft documents and draft version rows")
		}
		if (previous.DocumentLock != nil && next.DocumentLock == nil) || (previous.Capabilities.Locking && !next.Capabilities.Locking) {
			add("RIDU_DOCUMENT_LOCK_DISABLE_STATE_UNSAFE", "document locking", "target and owner lock rows")
		}
		if previous.Capabilities.Trash && !next.Capabilities.Trash {
			add("RIDU_TRASH_DISABLE_STATE_UNSAFE", "trash", "soft-deleted documents")
		}
	}
	for _, previous := range before.Collections {
		next, exists := afterCollections[previous.ID]
		if targetID, renamed := renameTargets[previous.ID]; !exists && renamed {
			next, exists = afterCollections[targetID]
		}
		if exists {
			inspect(previous, next)
		}
	}
	for _, previous := range before.Globals {
		if next, exists := afterGlobals[previous.ID]; exists {
			inspect(previous, next)
		}
	}
	return risks
}

func postgresCollectionRenameIDs(renames []Rename) map[schema.StableID]schema.StableID {
	result := make(map[schema.StableID]schema.StableID)
	for _, rename := range renames {
		if rename.Kind == RenameCollection {
			result[rename.BeforeCollection.ID] = rename.AfterCollection.ID
		}
	}
	return result
}

func phasesFromOperations(fromDigest string, steps []ridumigration.Operation) ([]ridumigration.Phase, error) {
	physicalDigest := ridumigration.PhysicalDigestSeed(fromDigest)
	phases := make([]ridumigration.Phase, 0, len(steps))
	stepNumber := 0
	for _, operation := range steps {
		mode := ridumigration.PhaseTransaction
		kind := operation.Kind
		var payload json.RawMessage
		var err error
		if operation.Kind == ridumigration.StepBackfillReferences {
			mode = ridumigration.PhaseBatch
		}
		if operation.Kind == ridumigration.StepSQL {
			if concurrent, ok := concurrentIndexPayload(operation.SQL); ok {
				mode, kind = ridumigration.PhaseNoTransaction, ridumigration.StepConcurrentIndex
				payload, err = ridumigration.MarshalStepPayload(concurrent)
			}
		}
		if len(payload) == 0 {
			payload, err = payloadFromOperation(operation)
		}
		if err != nil {
			return nil, err
		}
		stepNumber++
		step := ridumigration.Step{
			ID: fmt.Sprintf("step-%04d", stepNumber), Kind: kind,
			ExecutorVersion: 1, Name: operation.Name, Payload: payload,
		}
		if len(phases) == 0 || phases[len(phases)-1].Mode != mode || mode != ridumigration.PhaseTransaction {
			phases = append(phases, ridumigration.Phase{
				ID: fmt.Sprintf("phase-%03d", len(phases)+1), Mode: mode,
				PhysicalContractVersion: ridumigration.PhysicalContractVersion,
				BeforePhysicalDigest:    physicalDigest, AfterPhysicalDigest: physicalDigest,
				Steps: []ridumigration.Step{},
			})
		}
		phase := &phases[len(phases)-1]
		phase.Steps = append(phase.Steps, step)
		digest, err := ridumigration.PhasePhysicalDigest(phase.BeforePhysicalDigest, phase.Mode, phase.Steps)
		if err != nil {
			return nil, err
		}
		phase.AfterPhysicalDigest = digest
		physicalDigest = phase.AfterPhysicalDigest
	}
	return phases, nil
}

var (
	createIndexStatement = regexp.MustCompile(`^CREATE (UNIQUE )?INDEX "([a-z_][a-z0-9_]*)" ON "([a-z_][a-z0-9_]*)"(?: USING ([A-Za-z]+))? (.+)$`)
	dropIndexStatement   = regexp.MustCompile(`^DROP INDEX "([a-z_][a-z0-9_]*)"$`)
)

func concurrentIndexPayload(statement string) (ridumigration.ConcurrentIndexPayload, bool) {
	statement = strings.TrimSuffix(strings.TrimSpace(statement), ";")
	if match := createIndexStatement.FindStringSubmatch(statement); len(match) != 0 {
		definition, predicate, ok := splitCreateIndexDefinition(match[5])
		if !ok {
			return ridumigration.ConcurrentIndexPayload{}, false
		}
		parts, ok := splitIndexParts(definition)
		if !ok {
			return ridumigration.ConcurrentIndexPayload{}, false
		}
		return ridumigration.ConcurrentIndexPayload{
			Action: ridumigration.ConcurrentIndexCreate, Name: match[2], Table: match[3],
			Unique: match[1] != "", Method: strings.ToUpper(match[4]), Parts: parts, Predicate: predicate,
		}, true
	}
	if match := dropIndexStatement.FindStringSubmatch(statement); len(match) != 0 {
		return ridumigration.ConcurrentIndexPayload{Action: ridumigration.ConcurrentIndexDrop, Name: match[1]}, true
	}
	return ridumigration.ConcurrentIndexPayload{}, false
}

func splitCreateIndexDefinition(value string) (definition, predicate string, ok bool) {
	if len(value) < 2 || value[0] != '(' {
		return "", "", false
	}
	depth := 0
	quoted := false
	for index := 0; index < len(value); index++ {
		switch value[index] {
		case '\'':
			if quoted && index+1 < len(value) && value[index+1] == '\'' {
				index++
				continue
			}
			quoted = !quoted
		case '(':
			if !quoted {
				depth++
			}
		case ')':
			if quoted {
				continue
			}
			depth--
			if depth < 0 {
				return "", "", false
			}
			if depth != 0 {
				continue
			}
			definition = strings.TrimSpace(value[1:index])
			remainder := value[index+1:]
			if remainder == "" {
				return definition, "", definition != ""
			}
			if !strings.HasPrefix(remainder, " WHERE ") {
				return "", "", false
			}
			predicate = strings.TrimSpace(strings.TrimPrefix(remainder, " WHERE "))
			return definition, predicate, definition != "" && predicate != ""
		}
	}
	return "", "", false
}

func splitIndexParts(value string) ([]string, bool) {
	var parts []string
	depth := 0
	quoted := false
	start := 0
	for index := 0; index < len(value); index++ {
		switch value[index] {
		case '\'':
			if quoted && index+1 < len(value) && value[index+1] == '\'' {
				index++
				continue
			}
			quoted = !quoted
		case '(':
			if !quoted {
				depth++
			}
		case ')':
			if !quoted {
				depth--
				if depth < 0 {
					return nil, false
				}
			}
		case ',':
			if !quoted && depth == 0 {
				part := strings.TrimSpace(value[start:index])
				if part == "" {
					return nil, false
				}
				parts = append(parts, part)
				start = index + 1
			}
		}
	}
	last := strings.TrimSpace(value[start:])
	if quoted || depth != 0 || last == "" {
		return nil, false
	}
	return append(parts, last), true
}

func payloadFromOperation(step ridumigration.Operation) (json.RawMessage, error) {
	switch step.Kind {
	case ridumigration.StepSQL:
		return ridumigration.MarshalStepPayload(ridumigration.SQLPayload{SQL: step.SQL})
	case ridumigration.StepRenameContent:
		if step.Rename == nil {
			return nil, fmt.Errorf("content step %q has no rename payload", step.Name)
		}
		return ridumigration.MarshalStepPayload(ridumigration.RenamePayload{Rename: *step.Rename})
	case ridumigration.StepBackfillReferences:
		return ridumigration.MarshalStepPayload(ridumigration.BackfillReferencesPayload{BatchSize: 500})
	case ridumigration.StepRetireResources:
		return ridumigration.MarshalStepPayload(ridumigration.RetireResourcesPayload{
			ResourceIDs:          append([]schema.StableID(nil), step.ResourceIDs...),
			PurgeVersionOwnerIDs: append([]schema.StableID(nil), step.PurgeVersionOwnerIDs...),
		})
	case ridumigration.StepAuditRequiredValues:
		return ridumigration.MarshalStepPayload(ridumigration.AuditRequiredValuesPayload{Fields: step.RequiredFields})
	case ridumigration.StepAssertSchema:
		return ridumigration.MarshalStepPayload(ridumigration.AssertSchemaPayload{})
	default:
		return nil, fmt.Errorf("step %q has unsupported kind %q", step.Name, step.Kind)
	}
}

// postgresRequiredRenames binds confirmed renames for required-field
// detection, so a renamed field keeps the requiredness it already had.
func postgresRequiredRenames(renames []Rename) requiredfield.Renames {
	result := requiredfield.Renames{Resources: make(map[schema.StableID]schema.StableID), Fields: make(map[schema.StableID]schema.Field), Blocks: make(map[string]map[schema.StableID]schema.Field)}
	for _, rename := range renames {
		if rename.Kind == RenameCollection && rename.BeforeCollection.ID != rename.AfterCollection.ID {
			result.Resources[rename.BeforeCollection.ID] = rename.AfterCollection.ID
		}
		if rename.Kind == RenameBlockField && rename.BeforeField != nil && rename.AfterField != nil {
			if result.Blocks[rename.Block] == nil {
				result.Blocks[rename.Block] = make(map[schema.StableID]schema.Field)
			}
			result.Blocks[rename.Block][rename.AfterField.ID] = *rename.BeforeField
			continue
		}
		if rename.BeforeField != nil && rename.AfterField != nil {
			result.Fields[rename.AfterField.ID] = *rename.BeforeField
		}
		for _, pair := range rename.Fields {
			result.Fields[pair.After.ID] = pair.Before
		}
	}
	return result
}

// relaxRequiredColumns returns after with the top-level fields that
// requirements make required kept optional, so their columns stay nullable
// until the required-value audit admits the NOT NULL constraint.
func relaxRequiredColumns(after schema.Manifest, requirements []requiredfield.Requirement) schema.Manifest {
	relaxed := make(map[schema.StableID]map[schema.StableID]bool)
	for _, requirement := range requirements {
		if !requirement.TopLevel() {
			continue
		}
		if relaxed[requirement.Resource.ID] == nil {
			relaxed[requirement.Resource.ID] = make(map[schema.StableID]bool)
		}
		relaxed[requirement.Resource.ID][requirement.Field.ID] = true
	}
	if len(relaxed) == 0 {
		return after
	}
	snapshot := after.Snapshot()
	for _, resources := range [][]schema.Collection{snapshot.Collections, snapshot.Globals} {
		for resourceIndex := range resources {
			fields := relaxed[resources[resourceIndex].ID]
			for fieldIndex := range resources[resourceIndex].Fields {
				if fields[resources[resourceIndex].Fields[fieldIndex].ID] {
					resources[resourceIndex].Fields[fieldIndex].Required = false
				}
			}
		}
	}
	return schema.NewManifest(snapshot)
}

func removedResourceIDs(before, after schema.Snapshot, mapping atlasIdentityMap) []schema.StableID {
	// Collection and global identities are scoped by resource kind even though
	// both use the same physical table helper. Moving an ID across kinds is not a
	// supported rename and must not preserve private shared-table state.
	afterCollectionIDs := make(map[schema.StableID]struct{}, len(after.Collections))
	for _, resource := range after.Collections {
		afterCollectionIDs[resource.ID] = struct{}{}
	}
	afterGlobalIDs := make(map[schema.StableID]struct{}, len(after.Globals))
	for _, resource := range after.Globals {
		afterGlobalIDs[resource.ID] = struct{}{}
	}
	removed := make([]schema.StableID, 0)
	for _, resource := range before.Collections {
		if _, exists := afterCollectionIDs[mapping.collection(resource.ID)]; !exists {
			removed = append(removed, resource.ID)
		}
	}
	for _, resource := range before.Globals {
		if _, exists := afterGlobalIDs[resource.ID]; !exists {
			removed = append(removed, resource.ID)
		}
	}
	sort.Slice(removed, func(left, right int) bool { return removed[left] < removed[right] })
	return removed
}

func resourceRetirementReferencePolicy(before, after schema.Snapshot, mapping atlasIdentityMap, steps []ridumigration.Operation, removed []schema.StableID) ([]schema.StableID, []ridumigration.Risk) {
	removedSet := make(map[schema.StableID]bool, len(removed))
	for _, id := range removed {
		removedSet[id] = true
	}
	pluginTargets := make(map[schema.StableID]bool, len(before.Collections))
	for _, collection := range before.Collections {
		pluginTargets[collection.ID] = true
	}
	var locales []schema.LocaleCode
	if before.Application.Localization != nil {
		locales = before.Application.Localization.LocaleCodes()
	}
	purgeOwners := make(map[schema.StableID]bool)
	var blocked []ridumigration.Risk
	roots := newRemovedReferenceRoots(before, removedSet, pluginTargets)
	inspect := func(resource schema.Collection, current []schema.Collection, mappedID schema.StableID) {
		survives := false
		var survivor schema.Collection
		for _, candidate := range current {
			if candidate.ID == mappedID {
				survives, survivor = true, candidate
				break
			}
		}
		if !survives {
			return
		}
		// Both surviving document tables must drop the complete root.
		tables := documentTables(survivor, mappedID)
		for _, root := range resource.Fields {
			if !roots.root(root) {
				continue
			}
			if resource.Versions != nil {
				purgeOwners[mappedID] = true
			}
			mappedFieldID := mapping.field(resource.ID, root.ID)
			columns := []string{fieldColumn(mappedFieldID)}
			if root.Localized {
				columns = columns[:0]
				for _, locale := range locales {
					columns = append(columns, localizedFieldColumn(mappedFieldID, locale))
				}
			}
			for _, column := range columns {
				dropped := true
				for _, table := range tables {
					dropped = dropped && physicalColumnIsDropped(steps, table, column)
				}
				if dropped {
					continue
				}
				blocked = append(blocked, ridumigration.Risk{
					Code: "RIDU_RESOURCE_REMOVAL_REFERENCE_ROOT_UNSAFE", Level: ridumigration.RiskDestructive,
					Message: fmt.Sprintf("cannot remove referenced resources %s while surviving resource %s retains physical root field %s; remove the complete root field and its stored nested/group/array/block values in a separately reviewed migration before retiring the target", joinStableIDs(removed), mappedID, root.Name),
				})
				break
			}
		}
	}
	for _, resource := range before.Collections {
		inspect(resource, after.Collections, mapping.collection(resource.ID))
	}
	for _, resource := range before.Globals {
		inspect(resource, after.Globals, resource.ID)
	}
	owners := make([]schema.StableID, 0, len(purgeOwners))
	for id := range purgeOwners {
		owners = append(owners, id)
	}
	sort.Slice(owners, func(left, right int) bool { return owners[left] < owners[right] })
	return owners, blocked
}

type referenceShapeFieldIdentity struct {
	ID   schema.StableID
	Path string
}

// referenceShapeMapping records only explicitly confirmed field identity
// changes. A missing entry is intentionally not inferred from structural
// similarity: an unconfirmed remove/add pair must be treated as a reference
// shape decrease because its old stored values can become reachable again.
type referenceShapeMapping struct {
	fields  map[string]referenceShapeFieldIdentity
	targets map[string]string
}

func referenceShapeMappingFromRenames(renames []Rename) (referenceShapeMapping, error) {
	result := referenceShapeMapping{
		fields: make(map[string]referenceShapeFieldIdentity), targets: make(map[string]string),
	}
	for _, rename := range renames {
		if rename.Kind == RenameField && rename.BeforeField != nil && rename.AfterField != nil {
			if err := result.add(rename.BeforeCollection.ID, *rename.BeforeField, *rename.AfterField); err != nil {
				return referenceShapeMapping{}, err
			}
		}
		if rename.Kind == RenameBlockField && rename.BeforeField != nil && rename.AfterField != nil {
			if err := result.add(definitionOwner(rename.Block), *rename.BeforeField, *rename.AfterField); err != nil {
				return referenceShapeMapping{}, err
			}
		}
		for _, pair := range rename.Fields {
			if err := result.add(rename.BeforeCollection.ID, pair.Before, pair.After); err != nil {
				return referenceShapeMapping{}, err
			}
		}
	}
	return result, nil
}

func (mapping *referenceShapeMapping) add(ownerID schema.StableID, before, after schema.Field) error {
	if mapping.fields == nil {
		mapping.fields = make(map[string]referenceShapeFieldIdentity)
	}
	if mapping.targets == nil {
		mapping.targets = make(map[string]string)
	}
	sourceKey := referenceShapeFieldKey(ownerID, before.ID)
	target := referenceShapeFieldIdentity{ID: after.ID, Path: after.Path.String()}
	if existing, duplicate := mapping.fields[sourceKey]; duplicate && existing != target {
		return fmt.Errorf("RIDU_REFERENCE_RENAME_MAPPING_AMBIGUOUS: field rename source %s.%s maps to both %s and %s", ownerID, before.Path, existing.Path, target.Path)
	}
	targetKey := referenceShapeTargetKey(ownerID, target)
	if existingSource, duplicate := mapping.targets[targetKey]; duplicate && existingSource != sourceKey {
		return fmt.Errorf("RIDU_REFERENCE_RENAME_MAPPING_AMBIGUOUS: field rename target %s.%s receives more than one source", ownerID, after.Path)
	}
	mapping.fields[sourceKey] = target
	mapping.targets[targetKey] = sourceKey
	// A renamed group or array carries its children's identities. Block
	// definitions keep theirs: they are keyed by definition, not placement.
	if before.Nested != nil && after.Nested != nil {
		if err := mapping.addMatchingChildren(ownerID, before.Nested.ResolvedFields(), after.Nested.ResolvedFields()); err != nil {
			return err
		}
	}
	return nil
}

func (mapping *referenceShapeMapping) addMatchingChildren(ownerID schema.StableID, before, after []schema.Field) error {
	afterByName := make(map[string]schema.Field, len(after))
	for _, field := range after {
		afterByName[field.Name] = field
	}
	for _, field := range before {
		if next, exists := afterByName[field.Name]; exists {
			if err := mapping.add(ownerID, field, next); err != nil {
				return err
			}
		}
	}
	return nil
}

func (mapping referenceShapeMapping) field(ownerID schema.StableID, field schema.Field) referenceShapeFieldIdentity {
	if mapped, exists := mapping.fields[referenceShapeFieldKey(ownerID, field.ID)]; exists {
		return mapped
	}
	return referenceShapeFieldIdentity{ID: field.ID, Path: field.Path.String()}
}

func referenceShapeFieldKey(ownerID, fieldID schema.StableID) string {
	return string(ownerID) + "\x00" + string(fieldID)
}

func referenceShapeTargetKey(ownerID schema.StableID, field referenceShapeFieldIdentity) string {
	return string(ownerID) + "\x00" + string(field.ID) + "\x00" + field.Path
}

func pluginReferenceTargets(collections []schema.Collection, mapping atlasIdentityMap, normalize bool) []schema.StableID {
	targets := make([]schema.StableID, 0, len(collections))
	for _, collection := range collections {
		id := collection.ID
		if normalize {
			id = mapping.collection(id)
		}
		targets = append(targets, id)
	}
	sort.Slice(targets, func(left, right int) bool { return targets[left] < targets[right] })
	return targets
}

func removedReferenceLocales(before, after schema.Snapshot) []string {
	if before.Application.Localization == nil {
		return nil
	}
	afterLocales := make(map[schema.LocaleCode]bool)
	if after.Application.Localization != nil {
		for _, locale := range after.Application.Localization.LocaleCodes() {
			afterLocales[locale] = true
		}
	}
	var removed []string
	for _, locale := range before.Application.Localization.LocaleCodes() {
		if !afterLocales[locale] {
			removed = append(removed, string(locale))
		}
	}
	return removed
}

func appendReferenceContainer(containers []string, kind string, localized bool) []string {
	result := append([]string(nil), containers...)
	return append(result, fmt.Sprintf("%s:%t", kind, localized))
}

func relationshipReferenceTargets(relationship *schema.RelationshipField, mapping atlasIdentityMap, normalize bool) []schema.StableID {
	var targets []schema.StableID
	if relationship.Polymorphic {
		for _, target := range relationship.Targets {
			targetID := target.CollectionID
			if normalize {
				targetID = mapping.collection(targetID)
			}
			targets = append(targets, targetID)
		}
	} else {
		target := relationship.CollectionID
		if normalize {
			target = mapping.collection(target)
		}
		targets = append(targets, target)
	}
	sort.Slice(targets, func(left, right int) bool { return targets[left] < targets[right] })
	return targets
}

func referenceShapeDecreaseIsRetired(
	shape persistedReferenceShape,
	removed, purgedOwners map[schema.StableID]bool,
	physicalSteps []ridumigration.Operation,
	locales []schema.LocaleCode,
) bool {
	retiresTarget := false
	for _, target := range shape.Targets {
		if removed[target] {
			retiresTarget = true
			break
		}
	}
	if !retiresTarget || (shape.OwnerVersioned && !purgedOwners[shape.OwnerID]) {
		return false
	}
	columns := []string{fieldColumn(shape.RootID)}
	if shape.RootLocalized {
		if len(locales) == 0 {
			return false
		}
		columns = columns[:0]
		for _, locale := range locales {
			columns = append(columns, localizedFieldColumn(shape.RootID, locale))
		}
	}
	live := publishedCollectionTable(shape.OwnerID)
	liveDropped := !shape.OwnerVersioned || physicalTableIsDropped(physicalSteps, live)
	for _, column := range columns {
		if !physicalColumnIsDropped(physicalSteps, collectionTable(shape.OwnerID), column) {
			return false
		}
		if !liveDropped && !physicalColumnIsDropped(physicalSteps, live, column) {
			return false
		}
	}
	return true
}

func physicalTableIsDropped(steps []ridumigration.Operation, table string) bool {
	for _, step := range steps {
		if step.Kind == ridumigration.StepSQL && isDropTableStatement(step.SQL, table) {
			return true
		}
	}
	return false
}

func physicalColumnIsDropped(steps []ridumigration.Operation, table, column string) bool {
	for _, step := range steps {
		if step.Kind == ridumigration.StepSQL && isDropColumnStatement(step.SQL, table, column) {
			return true
		}
	}
	return false
}

func insertResourceRetirementBeforeDrop(steps []ridumigration.Operation, resourceIDs, purgeVersionOwnerIDs []schema.StableID) ([]ridumigration.Operation, error) {
	dropIndex := -1
	dropped := make(map[schema.StableID]bool, len(resourceIDs))
	for index, step := range steps {
		if step.Kind != ridumigration.StepSQL {
			continue
		}
		for _, id := range resourceIDs {
			if isDropTableStatement(step.SQL, collectionTable(id)) {
				dropped[id] = true
				if dropIndex < 0 {
					dropIndex = index
				}
			}
		}
	}
	for _, id := range resourceIDs {
		if !dropped[id] {
			return nil, fmt.Errorf("removed resource %s has no reviewed physical table drop", id)
		}
	}
	retirement := ridumigration.Operation{
		Kind:                 ridumigration.StepRetireResources,
		Name:                 "retire framework state for removed resources",
		ResourceIDs:          append([]schema.StableID(nil), resourceIDs...),
		PurgeVersionOwnerIDs: append([]schema.StableID(nil), purgeVersionOwnerIDs...),
	}
	result := make([]ridumigration.Operation, 0, len(steps)+1)
	result = append(result, steps[:dropIndex]...)
	result = append(result, retirement)
	result = append(result, steps[dropIndex:]...)
	return result, nil
}

func isDropTableStatement(statement, table string) bool {
	return strings.TrimSuffix(strings.TrimSpace(statement), ";") == "DROP TABLE "+quote(table)
}

func isDropColumnStatement(statement, table, column string) bool {
	return strings.TrimSuffix(strings.TrimSpace(statement), ";") == "ALTER TABLE "+quote(table)+" DROP COLUMN "+quote(column)
}

func joinStableIDs(ids []schema.StableID) string {
	values := make([]string, len(ids))
	for index, id := range ids {
		values[index] = string(id)
	}
	return strings.Join(values, ", ")
}

func physicalChangeRisks(changes []atlasschema.Change) []ridumigration.Risk {
	var risks []ridumigration.Risk
	var inspect func([]atlasschema.Change)
	inspect = func(current []atlasschema.Change) {
		for _, change := range current {
			switch typed := change.(type) {
			case *atlasschema.DropTable:
				risks = append(risks, ridumigration.Risk{Code: "RIDU_DROP_TABLE", Level: ridumigration.RiskDestructive, Message: fmt.Sprintf("drop table %s and all of its stored documents", typed.T.Name)})
			case *atlasschema.DropColumn:
				risks = append(risks, ridumigration.Risk{Code: "RIDU_DROP_COLUMN", Level: ridumigration.RiskDestructive, Message: fmt.Sprintf("drop column %s and all values stored in it", typed.C.Name)})
			case *atlasschema.ModifyColumn:
				if typed.Change.Is(atlasschema.ChangeType) {
					risks = append(risks, ridumigration.Risk{Code: "RIDU_CHANGE_COLUMN_TYPE", Level: ridumigration.RiskDestructive, Message: fmt.Sprintf("change column %s type; existing values may be rejected or converted with loss", typed.From.Name)})
				}
				if typed.Change.Is(atlasschema.ChangeNull) && typed.From.Type.Null && !typed.To.Type.Null {
					risks = append(risks, ridumigration.Risk{Code: "RIDU_REQUIRE_COLUMN", Level: ridumigration.RiskWarning, Message: fmt.Sprintf("make column %s required; the migration fails if existing rows contain null", typed.From.Name)})
				}
			case *atlasschema.AddIndex:
				code := "RIDU_BUILD_INDEX"
				message := fmt.Sprintf("build index %s; review table size and the planned PostgreSQL lock window", typed.I.Name)
				if typed.I.Unique {
					code = "RIDU_BUILD_UNIQUE_INDEX"
					message = fmt.Sprintf("build unique index %s; the migration fails if existing non-null values conflict", typed.I.Name)
				}
				risks = append(risks, ridumigration.Risk{Code: code, Level: ridumigration.RiskWarning, Message: message})
			case *atlasschema.DropIndex:
				code, level := "RIDU_DROP_INDEX", ridumigration.RiskWarning
				message := fmt.Sprintf("drop index %s; queries or sorts that rely on it may regress", typed.I.Name)
				if typed.I.Unique {
					code, level = "RIDU_DROP_UNIQUE_INDEX", ridumigration.RiskDestructive
					message = fmt.Sprintf("drop unique index %s and remove its database-enforced integrity guarantee", typed.I.Name)
				}
				risks = append(risks, ridumigration.Risk{Code: code, Level: level, Message: message})
			case *atlasschema.ModifyTable:
				inspect(typed.Changes)
			}
		}
	}
	inspect(changes)
	return risks
}

func atlasRenameMapping(renames []Rename) (atlasIdentityMap, error) {
	mapping := atlasIdentityMap{collections: make(map[schema.StableID]schema.StableID), fields: make(map[string]schema.StableID)}
	for _, rename := range renames {
		switch rename.Kind {
		case RenameCollection:
			mapping.collections[rename.BeforeCollection.ID] = rename.AfterCollection.ID
			for _, pair := range rename.Fields {
				if len(pair.Before.Path.Segments()) == 1 && len(pair.After.Path.Segments()) == 1 {
					mapping.fields[statementFieldKey(rename.BeforeCollection.ID, pair.Before.ID)] = pair.After.ID
				}
			}
		case RenameField:
			if rename.BeforeField == nil || rename.AfterField == nil {
				return atlasIdentityMap{}, fmt.Errorf("field rename requires before and after fields")
			}
			if len(rename.BeforeField.Path.Segments()) == 1 && len(rename.AfterField.Path.Segments()) == 1 {
				mapping.fields[statementFieldKey(rename.BeforeCollection.ID, rename.BeforeField.ID)] = rename.AfterField.ID
			}
		case RenameBlockField:
			// A block's fields live inside JSONB columns; no column changes.
			if rename.BeforeField == nil || rename.AfterField == nil || rename.Block == "" {
				return atlasIdentityMap{}, fmt.Errorf("block field rename requires a block and before and after fields")
			}
		default:
			return atlasIdentityMap{}, fmt.Errorf("unknown rename kind %q", rename.Kind)
		}
	}
	return mapping, nil
}

func atlasRenameSteps(ctx context.Context, name string, before schema.Manifest, mapping atlasIdentityMap, renames []Rename) ([]ridumigration.Operation, []ridumigration.Risk, error) {
	original := atlasSchema(before, atlasIdentityMap{})
	rebased := atlasSchema(before, mapping)
	var locales []schema.LocaleCode
	if localization := before.Snapshot().Application.Localization; localization != nil {
		locales = localization.LocaleCodes()
	}
	var steps []ridumigration.Operation
	var risks []ridumigration.Risk
	for _, rename := range renames {
		if rename.Kind == RenameBlockField {
			continue
		}
		tableBeforeID, tableAfterID := rename.BeforeCollection.ID, rename.AfterCollection.ID
		oldTable, oldOK := original.Table(collectionTable(tableBeforeID))
		newTable, newOK := rebased.Table(collectionTable(tableAfterID))
		if !oldOK || !newOK {
			return nil, nil, fmt.Errorf("rename %s cannot resolve its physical collection", renameStepName(rename))
		}
		pairs := []renameTablePair{{before: oldTable, after: newTable, name: func(name string) string { return name }}}
		// A versioned resource's live table is generated from the same
		// definition and is renamed identically, with its derived names.
		if oldLive, exists := original.Table(publishedCollectionTable(tableBeforeID)); exists {
			if newLive, exists := rebased.Table(publishedCollectionTable(tableAfterID)); exists {
				pairs = append(pairs, renameTablePair{before: oldLive, after: newLive, name: livePhysicalName})
			}
		}
		for _, pair := range pairs {
			planned, found, err := atlasRenameTableSteps(ctx, name, pair, rename, tableBeforeID, tableAfterID, locales)
			if err != nil {
				return nil, nil, err
			}
			steps, risks = append(steps, planned...), append(risks, found...)
		}
	}
	return steps, risks, nil
}

type renameTablePair struct {
	before, after *atlasschema.Table
	// name maps a working-table index or constraint name to this table's.
	name func(string) string
}

func atlasRenameTableSteps(ctx context.Context, name string, pair renameTablePair, rename Rename, tableBeforeID, tableAfterID schema.StableID, locales []schema.LocaleCode) ([]ridumigration.Operation, []ridumigration.Risk, error) {
	var steps []ridumigration.Operation
	var risks []ridumigration.Risk
	oldTable, newTable := pair.before, pair.after
	if oldTable.Name != newTable.Name {
		planned, found, err := atlasSteps(ctx, name, []atlasschema.Change{&atlasschema.RenameTable{From: oldTable, To: newTable}})
		if err != nil {
			return nil, nil, err
		}
		steps, risks = append(steps, planned...), append(risks, found...)
	}
	var beforeColumnChanges, tableChanges, afterColumnChanges []atlasschema.Change
	renamedIndexes := make(map[string]struct{})
	for _, field := range physicalFieldPairs(rename) {
		for _, locale := range atlasRenameLocales(field.Before, locales) {
			oldColumn, oldExists := oldTable.Column(atlasRenameColumn(field.Before.ID, locale))
			newColumn, newExists := newTable.Column(atlasRenameColumn(field.After.ID, locale))
			if !oldExists || !newExists || oldColumn.Name == newColumn.Name {
				continue
			}
			tableChanges = append(tableChanges, &atlasschema.RenameColumn{From: oldColumn, To: newColumn})
			if field.Before.Unique {
				oldName := pair.name("z_u_" + identifierHash(atlasRenameFieldKey(tableBeforeID, field.Before.ID, locale)))
				newName := pair.name("z_u_" + identifierHash(atlasRenameFieldKey(tableAfterID, field.After.ID, locale)))
				oldIndex, oldIndexExists := oldTable.Index(oldName)
				newIndex, newIndexExists := newTable.Index(newName)
				if oldIndexExists && newIndexExists && oldName != newName {
					tableChanges = append(tableChanges, &atlasschema.RenameIndex{From: oldIndex, To: newIndex})
					renamedIndexes[oldName] = struct{}{}
				}
			}
			if hasForeignKey(field.Before) {
				oldName := pair.name("z_fk_" + identifierHash(atlasRenameFieldKey(tableBeforeID, field.Before.ID, locale)))
				newName := pair.name("z_fk_" + identifierHash(atlasRenameFieldKey(tableAfterID, field.After.ID, locale)))
				oldForeignKey, oldForeignKeyExists := oldTable.ForeignKey(oldName)
				newForeignKey, newForeignKeyExists := newTable.ForeignKey(newName)
				if oldForeignKeyExists && newForeignKeyExists && oldName != newName {
					beforeColumnChanges = append(beforeColumnChanges, &atlasschema.DropForeignKey{F: oldForeignKey})
					afterColumnChanges = append(afterColumnChanges, &atlasschema.AddForeignKey{F: newForeignKey})
				}
			}
		}
	}
	// Stable-ID rebasing changes deterministic ordinary and compound index
	// names too. Match definitions after normalizing column identities; never
	// pair indexes by position because topology can evolve independently.
	rebasedIndexes := make(map[string][]*atlasschema.Index, len(newTable.Indexes))
	for _, candidate := range newTable.Indexes {
		signature := atlasIndexDefinitionSignature(candidate, newTable)
		rebasedIndexes[signature] = append(rebasedIndexes[signature], candidate)
	}
	for _, oldIndex := range oldTable.Indexes {
		if _, exists := renamedIndexes[oldIndex.Name]; exists {
			continue
		}
		matches := rebasedIndexes[atlasIndexDefinitionSignature(oldIndex, oldTable)]
		if len(matches) != 1 || oldIndex.Name == matches[0].Name {
			continue
		}
		tableChanges = append(tableChanges, &atlasschema.RenameIndex{From: oldIndex, To: matches[0]})
		renamedIndexes[oldIndex.Name] = struct{}{}
	}
	for _, orderedChanges := range [][]atlasschema.Change{beforeColumnChanges, tableChanges, afterColumnChanges} {
		if len(orderedChanges) == 0 {
			continue
		}
		planned, found, err := atlasSteps(ctx, name, []atlasschema.Change{&atlasschema.ModifyTable{T: newTable, Changes: orderedChanges}})
		if err != nil {
			return nil, nil, err
		}
		steps, risks = append(steps, planned...), append(risks, found...)
	}
	return steps, risks, nil
}

func atlasIndexDefinitionSignature(index *atlasschema.Index, table *atlasschema.Table) string {
	parts := []string{fmt.Sprintf("unique=%t", index.Unique)}
	for _, part := range index.Parts {
		if part.C != nil {
			parts = append(parts, fmt.Sprintf("column=%d:desc=%t", atlasColumnPosition(table, part.C.Name), part.Desc))
			continue
		}
		expression := fmt.Sprintf("%T", part.X)
		if raw, ok := part.X.(*atlasschema.RawExpr); ok {
			expression = normalizeAtlasIndexSQL(raw.X, table)
		}
		parts = append(parts, fmt.Sprintf("expression=%s:desc=%t", expression, part.Desc))
	}
	for _, attribute := range index.Attrs {
		if predicate, ok := attribute.(*atlaspostgres.IndexPredicate); ok {
			parts = append(parts, "predicate="+normalizeAtlasIndexSQL(predicate.P, table))
			continue
		}
		parts = append(parts, fmt.Sprintf("attribute=%T", attribute))
	}
	return strings.Join(parts, "\x00")
}

func atlasColumnPosition(table *atlasschema.Table, name string) int {
	for index, column := range table.Columns {
		if column.Name == name {
			return index
		}
	}
	return -1
}

func normalizeAtlasIndexSQL(statement string, table *atlasschema.Table) string {
	type replacement struct {
		value string
		with  string
	}
	replacements := make([]replacement, 0, len(table.Columns)*2)
	for index, column := range table.Columns {
		token := fmt.Sprintf("$column%d", index)
		replacements = append(replacements,
			replacement{value: quote(column.Name), with: token},
			replacement{value: column.Name, with: token},
		)
	}
	sort.Slice(replacements, func(left, right int) bool { return len(replacements[left].value) > len(replacements[right].value) })
	for _, candidate := range replacements {
		statement = strings.ReplaceAll(statement, candidate.value, candidate.with)
	}
	return statement
}

func atlasRenameLocales(field schema.Field, locales []schema.LocaleCode) []schema.LocaleCode {
	if field.Localized {
		return locales
	}
	return []schema.LocaleCode{""}
}

func atlasRenameColumn(fieldID schema.StableID, locale schema.LocaleCode) string {
	if locale != "" {
		return localizedFieldColumn(fieldID, locale)
	}
	return fieldColumn(fieldID)
}

func atlasRenameFieldKey(collectionID, fieldID schema.StableID, locale schema.LocaleCode) string {
	key := string(collectionID) + ":" + string(fieldID)
	if locale != "" {
		key += ":" + string(locale)
	}
	return key
}

func physicalFieldPairs(rename Rename) []FieldRename {
	if rename.Kind == RenameField && rename.BeforeField != nil && rename.AfterField != nil {
		return []FieldRename{{Before: *rename.BeforeField, After: *rename.AfterField}}
	}
	var pairs []FieldRename
	for _, pair := range rename.Fields {
		if len(pair.Before.Path.Segments()) == 1 && len(pair.After.Path.Segments()) == 1 {
			pairs = append(pairs, pair)
		}
	}
	return pairs
}

func atlasSteps(ctx context.Context, name string, changes []atlasschema.Change) ([]ridumigration.Operation, []ridumigration.Risk, error) {
	if len(changes) == 0 {
		return nil, nil, nil
	}
	plan, err := atlaspostgres.DefaultPlan.PlanChanges(ctx, name, changes, func(options *migrate.PlanOptions) {
		options.Mode = migrate.PlanModeInPlace
		// Artifacts target the connection's current_schema(), which keeps tenant,
		// test, and shadow schemas isolated instead of hard-coding public.
		empty := ""
		options.SchemaQualifier = &empty
	})
	if err != nil {
		return nil, nil, fmt.Errorf("Atlas PostgreSQL plan: %w", err)
	}
	steps := make([]ridumigration.Operation, len(plan.Changes))
	checks := make([]*sqlcheck.Change, len(plan.Changes))
	for index, change := range plan.Changes {
		steps[index] = ridumigration.Operation{Kind: ridumigration.StepSQL, Name: change.Comment, SQL: strings.TrimSuffix(strings.TrimSpace(change.Cmd), ";")}
		checks[index] = &sqlcheck.Change{Stmt: &migrate.Stmt{Text: change.Cmd, Pos: index + 1}, Changes: atlasschema.Changes{change.Source}}
	}
	var reports []sqlcheck.Report
	analyzers, err := sqlcheck.AnalyzerFor(atlaspostgres.DriverName, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("load Atlas PostgreSQL analyzers: %w", err)
	}
	pass := &sqlcheck.Pass{
		File:     &sqlcheck.File{File: atlasLintFile{name: name + ".sql"}, Changes: checks, Sum: changes},
		Reporter: sqlcheck.ReportWriterFunc(func(report sqlcheck.Report) { reports = append(reports, report) }),
	}
	if err := sqlcheck.Analyzers(analyzers).Analyze(ctx, pass); err != nil && len(reports) == 0 {
		return nil, nil, fmt.Errorf("Atlas migration lint: %w", err)
	}
	return steps, atlasRisks(reports), nil
}

type atlasLintFile struct {
	name string
	migrate.File
}

func (file atlasLintFile) Name() string { return file.name }

func atlasRisks(reports []sqlcheck.Report) []ridumigration.Risk {
	var risks []ridumigration.Risk
	for _, report := range reports {
		for _, diagnostic := range report.Diagnostics {
			level := ridumigration.RiskWarning
			if strings.HasPrefix(diagnostic.Code, "DS") {
				level = ridumigration.RiskDestructive
			}
			code := diagnostic.Code
			if code == "" {
				code = "ATLAS"
			}
			risks = append(risks, ridumigration.Risk{Code: code, Level: level, Message: diagnostic.Text})
		}
	}
	return risks
}

func normalizeRisks(risks []ridumigration.Risk) []ridumigration.Risk {
	seen := make(map[string]bool, len(risks))
	result := make([]ridumigration.Risk, 0, len(risks))
	for _, risk := range risks {
		key := risk.Code + "\x00" + string(risk.Level) + "\x00" + risk.Message
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, risk)
	}
	sort.Slice(result, func(left, right int) bool {
		return string(result[left].Level)+result[left].Code+result[left].Message < string(result[right].Level)+result[right].Code+result[right].Message
	})
	return result
}

func renameIntent(rename Rename) ridumigration.Rename {
	if rename.Kind == RenameBlockField && rename.BeforeField != nil && rename.AfterField != nil {
		return ridumigration.Rename{Block: rename.Block, FieldBefore: rename.BeforeField.Path.String(), FieldAfter: rename.AfterField.Path.String()}
	}
	intent := ridumigration.Rename{CollectionBefore: rename.BeforeCollection.Slug, CollectionAfter: rename.AfterCollection.Slug}
	if rename.Kind == RenameField && rename.BeforeField != nil && rename.AfterField != nil {
		intent.FieldBefore, intent.FieldAfter = rename.BeforeField.Path.String(), rename.AfterField.Path.String()
	}
	for _, pair := range rename.Fields {
		if pair.Before.Path.String() != pair.After.Path.String() {
			intent.Fields = append(intent.Fields, ridumigration.FieldRename{Before: pair.Before.Path.String(), After: pair.After.Path.String()})
		}
	}
	return intent
}

func renameStepName(rename Rename) string {
	if rename.Kind == RenameBlockField && rename.BeforeField != nil && rename.AfterField != nil {
		return fmt.Sprintf("preserve block field content %s.%s -> %s.%s", rename.Block, rename.BeforeField.Path, rename.Block, rename.AfterField.Path)
	}
	if rename.Kind == RenameField && rename.BeforeField != nil && rename.AfterField != nil {
		return fmt.Sprintf("preserve field content %s.%s -> %s.%s", rename.BeforeCollection.Slug, rename.BeforeField.Path, rename.AfterCollection.Slug, rename.AfterField.Path)
	}
	return fmt.Sprintf("preserve collection content %s -> %s", rename.BeforeCollection.Slug, rename.AfterCollection.Slug)
}
