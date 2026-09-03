package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"ariga.io/atlas/sql/migrate"
	atlaspostgres "ariga.io/atlas/sql/postgres"
	_ "ariga.io/atlas/sql/postgres/postgrescheck"
	atlasschema "ariga.io/atlas/sql/schema"
	"ariga.io/atlas/sql/sqlcheck"
	"github.com/riducms/ridu/internal/postgresmigration"
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
// ordered Ridu semantic steps for explicitly confirmed rename intent.
func BuildArtifact(ctx context.Context, name string, before *schema.Manifest, after schema.Manifest, renames []Rename, allowDestructive bool) (ridumigration.Artifact, error) {
	contract := currentAtlasPlannerContract()
	return buildArtifactWithPlannerContracts(ctx, name, before, after, renames, allowDestructive, contract, contract)
}

// BuildArtifactWithPreviousPlanner plans against the exact physical contract
// recorded by the preceding immutable artifact. Ordinary callers should use
// BuildArtifact; migration-history creation uses this entry point when a
// reviewed planner upgrade must emit semantic data work.
func BuildArtifactWithPreviousPlanner(ctx context.Context, name string, before *schema.Manifest, after schema.Manifest, renames []Rename, allowDestructive bool, previousPlannerVersion string) (ridumigration.Artifact, error) {
	target := currentAtlasPlannerContract()
	if before == nil {
		if previousPlannerVersion != "" {
			return ridumigration.Artifact{}, fmt.Errorf("initial PostgreSQL artifact cannot have previous planner version %q", previousPlannerVersion)
		}
		return buildArtifactWithPlannerContracts(ctx, name, before, after, renames, allowDestructive, target, target)
	}
	source, supported := atlasPlannerContractFor(previousPlannerVersion)
	if !supported {
		return ridumigration.Artifact{}, fmt.Errorf("unsupported previous PostgreSQL planner version %q", previousPlannerVersion)
	}
	if source.version != target.version && !(source.version == atlasVersionV1 && target.version == AtlasVersion) {
		return ridumigration.Artifact{}, fmt.Errorf("unsupported PostgreSQL planner transition %q -> %q", source.version, target.version)
	}
	return buildArtifactWithPlannerContracts(ctx, name, before, after, renames, allowDestructive, source, target)
}

func buildArtifactWithPlannerContracts(ctx context.Context, name string, before *schema.Manifest, after schema.Manifest, renames []Rename, allowDestructive bool, source, target atlasPlannerContract, transforms ...ridumigration.DataTransformDescriptor) (ridumigration.Artifact, error) {
	if err := validatePostgresDataTransformDescriptors(transforms); err != nil {
		return ridumigration.Artifact{}, err
	}
	if before != nil {
		if err := postgresmigration.ValidateVersionedTransition(before.Snapshot(), after.Snapshot(), transforms); err != nil {
			return ridumigration.Artifact{}, err
		}
	}
	artifact, err := ridumigration.NewArtifact(name, atlasPlannerForContract(target), before, after)
	if err != nil {
		return ridumigration.Artifact{}, err
	}
	var operations []ridumigration.Operation
	if before != nil {
		renames, err = withStableCollectionSlugRenames(before.Snapshot(), after.Snapshot(), renames)
		if err != nil {
			return ridumigration.Artifact{}, err
		}
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
		if source.version == atlasVersionV1 && target.version == AtlasVersion && len(ridumigration.AuthIdentityResources(before.Snapshot())) != 0 && len(ridumigration.RetainedAuthIdentityResources(before.Snapshot(), after.Snapshot())) == 0 {
			return ridumigration.Artifact{}, fmt.Errorf("PostgreSQL planner upgrade cannot replace or remove every legacy auth identity field; create the canonical auth identity artifact before that schema transition")
		}
	}
	if before == nil {
		changes, err := atlaspostgres.DefaultDiff.SchemaDiff(emptyAtlasSchema(), atlasSchemaForContract(after, atlasIdentityMap{}, target))
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
		if source.version != target.version && len(renames) != 0 {
			return ridumigration.Artifact{}, fmt.Errorf("PostgreSQL planner upgrade cannot be combined with collection or field renames; create the canonical auth identity artifact first")
		}
		renameSteps, renameRisks, err := atlasRenameSteps(ctx, name, *before, mapping, renames, source)
		if err != nil {
			return ridumigration.Artifact{}, err
		}
		operations = append(operations, renameSteps...)
		artifact.Risks = append(artifact.Risks, renameRisks...)
		for _, rename := range renames {
			intent := renameIntent(rename)
			operations = append(operations, ridumigration.Operation{Kind: ridumigration.StepRenameContent, Name: renameStepName(rename), Rename: &intent})
		}

		changes, err := atlaspostgres.DefaultDiff.SchemaDiff(atlasSchemaForContract(*before, mapping, source), atlasSchemaForContract(after, atlasIdentityMap{}, target))
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
		if postgresAuthIdentityUpgradeRequired(source, target, before.Snapshot(), after.Snapshot()) {
			operations = append(operations, ridumigration.Operation{
				Kind: ridumigration.StepCanonicalizeAuthIdentities, Name: "canonicalize authored authentication identities",
				AuthIdentities: ridumigration.RetainedAuthIdentityResources(before.Snapshot(), after.Snapshot()),
			})
			artifact.Risks = append(artifact.Risks, ridumigration.Risk{
				Code: "RIDU_AUTH_IDENTITY_CANONICALIZATION", Level: ridumigration.RiskWarning,
				Message: "rewrite authored authentication identities to the shared lowercase-and-trimmed key after a collision preflight; coordinate the migration with application writers",
			})
		}
		operations = append(operations, steps...)
		artifact.Risks = append(artifact.Risks, physicalChangeRisks(changes)...)
		artifact.Risks = append(artifact.Risks, risks...)
	}
	pluginSteps, pluginRisks, err := pluginMigrationSteps(before, after)
	if err != nil {
		return ridumigration.Artifact{}, err
	}
	operations = append(operations, pluginSteps...)
	artifact.Risks = append(artifact.Risks, pluginRisks...)
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
	if before != nil && artifact.FromDigest == artifact.ToDigest && !postgresAuthIdentityUpgradeRequired(source, target, before.Snapshot(), after.Snapshot()) && len(transforms) == 0 {
		return ridumigration.Artifact{}, fmt.Errorf("schema is current; no migration steps were planned")
	}
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

func postgresAuthIdentityUpgradeRequired(source, target atlasPlannerContract, before, after schema.Snapshot) bool {
	return source.version == atlasVersionV1 && target.version == AtlasVersion && len(ridumigration.RetainedAuthIdentityResources(before, after)) != 0
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
	renameTargets := make(map[schema.StableID]schema.StableID)
	for _, rename := range renames {
		if rename.Kind == RenameCollection {
			renameTargets[rename.BeforeCollection.ID] = rename.AfterCollection.ID
		}
	}
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
	case ridumigration.StepPluginSQL:
		if step.Plugin == nil {
			return nil, fmt.Errorf("plugin step %q has no plugin payload", step.Name)
		}
		return ridumigration.MarshalStepPayload(ridumigration.PluginPayload{Plugin: *step.Plugin})
	case ridumigration.StepBackfillReferences:
		return ridumigration.MarshalStepPayload(ridumigration.BackfillReferencesPayload{BatchSize: 500})
	case ridumigration.StepRetireResources:
		return ridumigration.MarshalStepPayload(ridumigration.RetireResourcesPayload{
			ResourceIDs:          append([]schema.StableID(nil), step.ResourceIDs...),
			PurgeVersionOwnerIDs: append([]schema.StableID(nil), step.PurgeVersionOwnerIDs...),
		})
	case ridumigration.StepCanonicalizeAuthIdentities:
		return ridumigration.MarshalStepPayload(ridumigration.CanonicalizeAuthIdentitiesPayload{
			Resources: append([]ridumigration.AuthIdentityResource(nil), step.AuthIdentities...),
		})
	case ridumigration.StepAssertSchema:
		return ridumigration.MarshalStepPayload(ridumigration.AssertSchemaPayload{})
	default:
		return nil, fmt.Errorf("step %q has unsupported kind %q", step.Name, step.Kind)
	}
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
	inspect := func(resource schema.Collection, current []schema.Collection, mappedID schema.StableID) {
		survives := false
		for _, candidate := range current {
			if candidate.ID == mappedID {
				survives = true
				break
			}
		}
		if !survives {
			return
		}
		for _, root := range resource.Fields {
			if !fieldRootReferencesRemovedResource(root, removedSet, pluginTargets) {
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
				if physicalColumnIsDropped(steps, collectionTable(mappedID), column) {
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

func fieldRootReferencesRemovedResource(field schema.Field, removed, pluginTargets map[schema.StableID]bool) bool {
	if field.Relationship != nil {
		if !field.Relationship.Polymorphic && removed[field.Relationship.CollectionID] {
			return true
		}
		for _, target := range field.Relationship.Targets {
			if removed[target.CollectionID] {
				return true
			}
		}
	}
	if field.Upload != nil && removed[field.Upload.CollectionID] {
		return true
	}
	// A plugin reference key declares that matching JSON properties contain
	// collection slugs, but intentionally does not narrow the possible target
	// collections. Treat every collection in the source snapshot as a potential
	// target. If any collection is retired, the complete plugin root must leave
	// physical storage with it; otherwise current or historical nested values can
	// become live again after the slug/stable identity is reused.
	if field.Plugin != nil && len(field.Plugin.ReferenceKeys) != 0 {
		for target := range pluginTargets {
			if removed[target] {
				return true
			}
		}
	}
	if field.Nested != nil {
		for _, child := range field.Nested.Fields {
			if fieldRootReferencesRemovedResource(child, removed, pluginTargets) {
				return true
			}
		}
	}
	if field.Blocks != nil {
		for _, block := range field.Blocks.Types {
			for _, child := range block.Fields {
				if fieldRootReferencesRemovedResource(child, removed, pluginTargets) {
					return true
				}
			}
		}
	}
	return false
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
	if before.Nested != nil && after.Nested != nil {
		if err := mapping.addMatchingChildren(ownerID, before.Nested.Fields, after.Nested.Fields); err != nil {
			return err
		}
	}
	if before.Blocks != nil && after.Blocks != nil {
		afterBlocks := make(map[string]schema.BlockType, len(after.Blocks.Types))
		for _, block := range after.Blocks.Types {
			afterBlocks[block.Key] = block
		}
		for _, block := range before.Blocks.Types {
			if next, exists := afterBlocks[block.Key]; exists {
				if err := mapping.addMatchingChildren(ownerID, block.Fields, next.Fields); err != nil {
					return err
				}
			}
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

type persistedReferenceShape struct {
	OwnerID          schema.StableID
	OwnerVersioned   bool
	RootID           schema.StableID
	RootName         string
	RootLocalized    bool
	FieldID          schema.StableID
	FieldPath        string
	LeafLocalized    bool
	UsesLocalization bool
	Containers       string
	Kind             string
	Targets          []schema.StableID
	HasMany          bool
	Polymorphic      bool
	PluginKey        string
	PluginReference  string
}

func (shape persistedReferenceShape) key() string {
	return string(shape.FieldID) + "\x00" + shape.FieldPath + "\x00" + shape.PluginReference
}

// unsafeReferenceShapeDecreases rejects the transition at which reference
// semantics disappear or narrow, rather than waiting for a later target
// removal. Otherwise a two-artifact sequence can hide values from the
// immediate retirement comparison while leaving current storage or version
// snapshots ready to reattach after the old shape and identity are restored.
//
// The one generic safe case is deliberately narrow: a target referenced by
// the old leaf is retired in this artifact, the complete physical root column
// is dropped, and every versioned owner is included in the same retirement's
// full-history purge. That is the contract already enforced by
// retire_resources; ordinary --allow-destructive is not a substitute.
func unsafeReferenceShapeDecreases(
	before, after schema.Snapshot,
	mapping atlasIdentityMap,
	fieldMapping referenceShapeMapping,
	physicalSteps []ridumigration.Operation,
	removed, purgeVersionOwners []schema.StableID,
) []ridumigration.Risk {
	removedSet := make(map[schema.StableID]bool, len(removed))
	for _, id := range removed {
		removedSet[id] = true
	}
	purgedOwners := make(map[schema.StableID]bool, len(purgeVersionOwners))
	for _, id := range purgeVersionOwners {
		purgedOwners[id] = true
	}
	var locales []schema.LocaleCode
	if before.Application.Localization != nil {
		locales = before.Application.Localization.LocaleCodes()
	}
	removedLocales := removedReferenceLocales(before, after)
	beforePluginTargets := pluginReferenceTargets(before.Collections, mapping, true)
	afterPluginTargets := pluginReferenceTargets(after.Collections, atlasIdentityMap{}, false)
	afterCollections := make(map[schema.StableID]schema.Collection, len(after.Collections))
	for _, resource := range after.Collections {
		afterCollections[resource.ID] = resource
	}
	afterGlobals := make(map[schema.StableID]schema.Global, len(after.Globals))
	for _, resource := range after.Globals {
		afterGlobals[resource.ID] = resource
	}

	var risks []ridumigration.Risk
	inspect := func(previous, next schema.Collection, previousOwnerID, nextOwnerID schema.StableID) {
		beforeShapes := persistedReferenceShapes(previous, previousOwnerID, nextOwnerID, mapping, fieldMapping, beforePluginTargets, true)
		afterShapes := persistedReferenceShapes(next, nextOwnerID, nextOwnerID, atlasIdentityMap{}, referenceShapeMapping{}, afterPluginTargets, false)
		afterByKey := make(map[string]persistedReferenceShape, len(afterShapes))
		for _, shape := range afterShapes {
			afterByKey[shape.key()] = shape
		}
		for _, oldShape := range beforeShapes {
			newShape, exists := afterByKey[oldShape.key()]
			decreased, reason := referenceShapeDecreased(oldShape, newShape, exists)
			if !decreased && oldShape.UsesLocalization && len(removedLocales) != 0 {
				decreased = true
				reason = "remove application locale " + strings.Join(removedLocales, ", ")
			}
			if !decreased {
				continue
			}
			if referenceShapeDecreaseIsRetired(oldShape, removedSet, purgedOwners, physicalSteps, locales) {
				continue
			}
			risks = append(risks, ridumigration.Risk{
				Code: "RIDU_REFERENCE_SHAPE_DECREASE_UNSAFE", Level: ridumigration.RiskDestructive,
				Message: fmt.Sprintf("cannot %s for stored reference %s.%s in a generic schema artifact; current values or version snapshots can become reachable again after the old reference shape and target identity are restored. Keep the reference shape, or retire a referenced target and drop the complete physical root in the same resource-retirement artifact so versioned owner history is purged; otherwise use a separately reviewed application-owned data migration", reason, nextOwnerID, oldShape.FieldPath),
			})
		}
	}
	for _, previous := range before.Collections {
		nextID := mapping.collection(previous.ID)
		if next, exists := afterCollections[nextID]; exists {
			inspect(previous, next, previous.ID, nextID)
		}
	}
	for _, previous := range before.Globals {
		if next, exists := afterGlobals[previous.ID]; exists {
			inspect(previous, next, previous.ID, previous.ID)
		}
	}
	sort.Slice(risks, func(left, right int) bool { return risks[left].Message < risks[right].Message })
	return risks
}

func persistedReferenceShapes(
	resource schema.Collection,
	previousOwnerID, ownerID schema.StableID,
	collectionMapping atlasIdentityMap,
	fieldMapping referenceShapeMapping,
	pluginTargets []schema.StableID,
	normalizeBefore bool,
) []persistedReferenceShape {
	var result []persistedReferenceShape
	var inspect func(schema.Field, schema.Field, []string, bool)
	inspect = func(field, root schema.Field, containers []string, ancestorLocalized bool) {
		identity := referenceShapeFieldIdentity{ID: field.ID, Path: field.Path.String()}
		rootIdentity := referenceShapeFieldIdentity{ID: root.ID, Path: root.Path.String()}
		if normalizeBefore {
			identity = fieldMapping.field(previousOwnerID, field)
			rootIdentity.ID = collectionMapping.field(previousOwnerID, root.ID)
		}
		base := persistedReferenceShape{
			OwnerID: ownerID, OwnerVersioned: resource.Versions != nil,
			RootID: rootIdentity.ID, RootName: root.Name, RootLocalized: root.Localized,
			FieldID: identity.ID, FieldPath: identity.Path, LeafLocalized: field.Localized,
			UsesLocalization: root.Localized || field.Localized || ancestorLocalized,
			Containers:       strings.Join(containers, "/"),
		}
		if field.Relationship != nil {
			base.Kind = "relationship"
			base.HasMany, base.Polymorphic = field.Relationship.HasMany, field.Relationship.Polymorphic
			base.Targets = relationshipReferenceTargets(field.Relationship, collectionMapping, normalizeBefore)
			result = append(result, base)
		}
		if field.Upload != nil {
			base.Kind = "upload"
			base.HasMany = field.Upload.HasMany
			target := field.Upload.CollectionID
			if normalizeBefore {
				target = collectionMapping.collection(target)
			}
			base.Targets = []schema.StableID{target}
			result = append(result, base)
		}
		if field.Plugin != nil {
			for _, key := range field.Plugin.ReferenceKeys {
				plugin := base
				plugin.Kind = "plugin"
				plugin.PluginKey = field.Plugin.Key
				plugin.PluginReference = key
				plugin.Targets = append([]schema.StableID(nil), pluginTargets...)
				result = append(result, plugin)
			}
		}
		if field.Nested != nil {
			nested := appendReferenceContainer(containers, string(field.Type), field.Localized)
			for _, child := range field.Nested.Fields {
				inspect(child, root, nested, ancestorLocalized || field.Localized)
			}
		}
		if field.Blocks != nil {
			for _, block := range field.Blocks.Types {
				blockContainers := appendReferenceContainer(containers, string(field.Type)+":"+block.Key, field.Localized)
				for _, child := range block.Fields {
					inspect(child, root, blockContainers, ancestorLocalized || field.Localized)
				}
			}
		}
	}
	for _, root := range resource.Fields {
		inspect(root, root, nil, false)
	}
	return result
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

func referenceShapeDecreased(before, after persistedReferenceShape, exists bool) (bool, string) {
	if !exists {
		return true, "remove or retype the field"
	}
	if before.Kind != after.Kind {
		return true, "change the reference kind"
	}
	if before.PluginKey != after.PluginKey {
		return true, "change the plugin reference contract"
	}
	if before.RootID != after.RootID || before.RootLocalized != after.RootLocalized ||
		before.LeafLocalized != after.LeafLocalized || before.Containers != after.Containers {
		return true, "change the physical root or nested container shape"
	}
	if before.HasMany != after.HasMany || before.Polymorphic != after.Polymorphic {
		return true, "change relationship cardinality or polymorphic shape"
	}
	afterTargets := make(map[schema.StableID]bool, len(after.Targets))
	for _, target := range after.Targets {
		afterTargets[target] = true
	}
	for _, target := range before.Targets {
		if !afterTargets[target] {
			return true, "remove or replace target " + string(target)
		}
	}
	return false, ""
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
	for _, column := range columns {
		if !physicalColumnIsDropped(physicalSteps, collectionTable(shape.OwnerID), column) {
			return false
		}
	}
	return true
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

type referenceIndexTopology struct {
	Locales   []schema.LocaleCode
	Resources []referenceIndexResource
}

type referenceIndexResource struct {
	ID     schema.StableID
	Fields []referenceIndexField
}

type referenceIndexField struct {
	ID           schema.StableID
	Name         string
	Type         schema.FieldType
	Localized    bool
	Relationship *referenceIndexRelationship
	Upload       *referenceIndexUpload
	Nested       []referenceIndexField
	Blocks       []referenceIndexBlock
}

type referenceIndexRelationship struct {
	CollectionID   schema.StableID
	CollectionSlug schema.CollectionSlug
	Targets        []schema.RelationshipTarget
	HasMany        bool
	Polymorphic    bool
}

type referenceIndexUpload struct {
	CollectionID   schema.StableID
	CollectionSlug schema.CollectionSlug
	HasMany        bool
}

type referenceIndexBlock struct {
	Key    string
	Fields []referenceIndexField
}

func referenceIndexTopologyChanged(before, after schema.Snapshot) bool {
	return !reflect.DeepEqual(referenceTopology(before), referenceTopology(after))
}

func referenceTopology(snapshot schema.Snapshot) referenceIndexTopology {
	topology := referenceIndexTopology{}
	if snapshot.Application.Localization != nil {
		topology.Locales = append([]schema.LocaleCode(nil), snapshot.Application.Localization.LocaleCodes()...)
	}
	resources := append(append([]schema.Collection(nil), snapshot.Collections...), snapshot.Globals...)
	for _, resource := range resources {
		fields := referenceTopologyFields(resource.Fields)
		if len(fields) != 0 {
			topology.Resources = append(topology.Resources, referenceIndexResource{ID: resource.ID, Fields: fields})
		}
	}
	return topology
}

func referenceTopologyFields(fields []schema.Field) []referenceIndexField {
	var result []referenceIndexField
	for _, field := range fields {
		candidate := referenceIndexField{ID: field.ID, Name: field.Name, Type: field.Type, Localized: field.Localized}
		if field.Relationship != nil {
			candidate.Relationship = &referenceIndexRelationship{
				CollectionID: field.Relationship.CollectionID, CollectionSlug: field.Relationship.CollectionSlug,
				Targets: append([]schema.RelationshipTarget(nil), field.Relationship.Targets...),
				HasMany: field.Relationship.HasMany, Polymorphic: field.Relationship.Polymorphic,
			}
		}
		if field.Upload != nil {
			candidate.Upload = &referenceIndexUpload{
				CollectionID: field.Upload.CollectionID, CollectionSlug: field.Upload.CollectionSlug,
				HasMany: field.Upload.HasMany,
			}
		}
		if field.Nested != nil {
			candidate.Nested = referenceTopologyFields(field.Nested.Fields)
		}
		if field.Blocks != nil {
			for _, block := range field.Blocks.Types {
				children := referenceTopologyFields(block.Fields)
				if len(children) != 0 {
					candidate.Blocks = append(candidate.Blocks, referenceIndexBlock{Key: block.Key, Fields: children})
				}
			}
		}
		if candidate.Relationship != nil || candidate.Upload != nil || len(candidate.Nested) != 0 || len(candidate.Blocks) != 0 {
			result = append(result, candidate)
		}
	}
	return result
}

func pluginMigrationSteps(before *schema.Manifest, after schema.Manifest) ([]ridumigration.Operation, []ridumigration.Risk, error) {
	beforePlugins := make(map[string]schema.Plugin)
	if before != nil {
		for _, plugin := range before.Snapshot().Plugins {
			beforePlugins[plugin.Key] = plugin
		}
	}
	afterPlugins := make(map[string]schema.Plugin)
	for _, plugin := range after.Snapshot().Plugins {
		afterPlugins[plugin.Key] = plugin
	}
	keys := make(map[string]struct{}, len(beforePlugins)+len(afterPlugins))
	for key := range beforePlugins {
		keys[key] = struct{}{}
	}
	for key := range afterPlugins {
		keys[key] = struct{}{}
	}
	ordered := make([]string, 0, len(keys))
	for key := range keys {
		ordered = append(ordered, key)
	}
	sort.Strings(ordered)
	var steps []ridumigration.Operation
	var risks []ridumigration.Risk
	for _, key := range ordered {
		previous, hadPrevious := beforePlugins[key]
		next, hasNext := afterPlugins[key]
		var previousMigrations, nextMigrations []schema.PluginMigration
		if hadPrevious && previous.HasDatabaseContributions() {
			contribution, supported := previous.DatabaseContribution(schema.PluginDatabaseAdapterPostgres)
			if !supported {
				return nil, nil, fmt.Errorf("plugin %q has private database schema but does not support postgres; contribute ordinary collections for portable plugin data or add a postgres database contribution", key)
			}
			if err := validatePostgresPluginMigrationSQL(key, contribution.Migrations); err != nil {
				return nil, nil, err
			}
			previousMigrations = contribution.Migrations
		}
		if hasNext && next.HasDatabaseContributions() {
			contribution, supported := next.DatabaseContribution(schema.PluginDatabaseAdapterPostgres)
			if !supported {
				return nil, nil, fmt.Errorf("plugin %q has private database schema but does not support postgres; contribute ordinary collections for portable plugin data or add a postgres database contribution", key)
			}
			if err := validatePostgresPluginMigrationSQL(key, contribution.Migrations); err != nil {
				return nil, nil, err
			}
			nextMigrations = contribution.Migrations
		}
		shared := len(previousMigrations)
		if len(nextMigrations) < shared {
			shared = len(nextMigrations)
		}
		for index := 0; index < shared; index++ {
			if pluginMigrationChecksum(schema.PluginDatabaseAdapterPostgres, key, previousMigrations[index]) != pluginMigrationChecksum(schema.PluginDatabaseAdapterPostgres, key, nextMigrations[index]) {
				return nil, nil, fmt.Errorf("plugin %s postgres migration %d changed after publication", key, index+1)
			}
		}
		if hasNext && len(nextMigrations) > len(previousMigrations) {
			for index := len(previousMigrations); index < len(nextMigrations); index++ {
				migration := nextMigrations[index]
				pluginStep := ridumigration.PluginStep{Adapter: schema.PluginDatabaseAdapterPostgres, Plugin: key, Version: migration.Version, Direction: "up", SQL: append([]string(nil), migration.UpSQL...)}
				pluginStep.Checksum = ridumigration.PluginStepChecksum(pluginStep.Adapter, key, migration.Version, pluginStep.Direction, pluginStep.SQL)
				steps = append(steps, ridumigration.Operation{Kind: ridumigration.StepPluginSQL, Name: fmt.Sprintf("plugin %s migration %d %s up", key, migration.Version, migration.Name), Plugin: &pluginStep})
			}
		}
		if hadPrevious && (!hasNext || len(nextMigrations) < len(previousMigrations)) {
			minimum := len(nextMigrations)
			for index := len(previousMigrations) - 1; index >= minimum; index-- {
				migration := previousMigrations[index]
				pluginStep := ridumigration.PluginStep{Adapter: schema.PluginDatabaseAdapterPostgres, Plugin: key, Version: migration.Version, Direction: "down", SQL: append([]string(nil), migration.DownSQL...)}
				pluginStep.Checksum = ridumigration.PluginStepChecksum(pluginStep.Adapter, key, migration.Version, pluginStep.Direction, pluginStep.SQL)
				steps = append(steps, ridumigration.Operation{Kind: ridumigration.StepPluginSQL, Name: fmt.Sprintf("plugin %s migration %d %s down", key, migration.Version, migration.Name), Plugin: &pluginStep})
				risks = append(risks, ridumigration.Risk{Code: "RIDU_PLUGIN_MIGRATION_DOWN", Level: ridumigration.RiskDestructive, Message: fmt.Sprintf("run plugin %s migration %d down; plugin-owned data may be removed", key, migration.Version)})
			}
		}
	}
	return steps, risks, nil
}

func validatePostgresPluginMigrationSQL(key string, migrations []schema.PluginMigration) error {
	for _, pluginMigration := range migrations {
		for _, statement := range append(append([]string(nil), pluginMigration.UpSQL...), pluginMigration.DownSQL...) {
			if !schema.IsValidPluginMigrationSQL(schema.PluginDatabaseAdapterPostgres, statement) {
				return fmt.Errorf("plugin %q postgres migration %d contains invalid SQL or transaction control", key, pluginMigration.Version)
			}
		}
	}
	return nil
}

func pluginMigrationChecksum(adapter schema.PluginDatabaseAdapter, key string, migration schema.PluginMigration) string {
	up := ridumigration.PluginStepChecksum(adapter, key, migration.Version, "up", migration.UpSQL)
	down := ridumigration.PluginStepChecksum(adapter, key, migration.Version, "down", migration.DownSQL)
	return up + ":" + down + ":" + migration.Name
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
		default:
			return atlasIdentityMap{}, fmt.Errorf("unknown rename kind %q", rename.Kind)
		}
	}
	return mapping, nil
}

func atlasRenameSteps(ctx context.Context, name string, before schema.Manifest, mapping atlasIdentityMap, renames []Rename, contract atlasPlannerContract) ([]ridumigration.Operation, []ridumigration.Risk, error) {
	original := atlasSchemaForContract(before, atlasIdentityMap{}, contract)
	rebased := atlasSchemaForContract(before, mapping, contract)
	var locales []schema.LocaleCode
	if localization := before.Snapshot().Application.Localization; localization != nil {
		locales = localization.LocaleCodes()
	}
	var steps []ridumigration.Operation
	var risks []ridumigration.Risk
	for _, rename := range renames {
		var tableBeforeID, tableAfterID schema.StableID
		switch rename.Kind {
		case RenameCollection:
			tableBeforeID, tableAfterID = rename.BeforeCollection.ID, rename.AfterCollection.ID
		case RenameField:
			tableBeforeID, tableAfterID = rename.BeforeCollection.ID, rename.AfterCollection.ID
		}
		oldTable, oldOK := original.Table(collectionTable(tableBeforeID))
		newTable, newOK := rebased.Table(collectionTable(tableAfterID))
		if !oldOK || !newOK {
			return nil, nil, fmt.Errorf("rename %s cannot resolve its physical collection", renameStepName(rename))
		}
		if oldTable.Name != newTable.Name {
			planned, found, err := atlasSteps(ctx, name, []atlasschema.Change{&atlasschema.RenameTable{From: oldTable, To: newTable}})
			if err != nil {
				return nil, nil, err
			}
			steps, risks = append(steps, planned...), append(risks, found...)
		}
		var beforeColumnChanges, tableChanges, afterColumnChanges []atlasschema.Change
		renamedIndexes := make(map[string]struct{})
		for _, pair := range physicalFieldPairs(rename) {
			for _, locale := range atlasRenameLocales(pair.Before, locales) {
				oldColumn, oldExists := oldTable.Column(atlasRenameColumn(pair.Before.ID, locale))
				newColumn, newExists := newTable.Column(atlasRenameColumn(pair.After.ID, locale))
				if !oldExists || !newExists || oldColumn.Name == newColumn.Name {
					continue
				}
				tableChanges = append(tableChanges, &atlasschema.RenameColumn{From: oldColumn, To: newColumn})
				if pair.Before.Unique {
					oldName := "z_u_" + identifierHash(atlasRenameFieldKey(tableBeforeID, pair.Before.ID, locale))
					newName := "z_u_" + identifierHash(atlasRenameFieldKey(tableAfterID, pair.After.ID, locale))
					oldIndex, oldIndexExists := oldTable.Index(oldName)
					newIndex, newIndexExists := newTable.Index(newName)
					if oldIndexExists && newIndexExists && oldName != newName {
						tableChanges = append(tableChanges, &atlasschema.RenameIndex{From: oldIndex, To: newIndex})
						renamedIndexes[oldName] = struct{}{}
					}
				}
				if hasForeignKey(pair.Before) {
					oldName := "z_fk_" + identifierHash(atlasRenameFieldKey(tableBeforeID, pair.Before.ID, locale))
					newName := "z_fk_" + identifierHash(atlasRenameFieldKey(tableAfterID, pair.After.ID, locale))
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
	if rename.Kind == RenameField && rename.BeforeField != nil && rename.AfterField != nil {
		return fmt.Sprintf("preserve field content %s.%s -> %s.%s", rename.BeforeCollection.Slug, rename.BeforeField.Path, rename.AfterCollection.Slug, rename.AfterField.Path)
	}
	return fmt.Sprintf("preserve collection content %s -> %s", rename.BeforeCollection.Slug, rename.AfterCollection.Slug)
}
