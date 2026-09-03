package migration

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/riducms/ridu/schema"
)

// RunnerContractVersion is the execution contract implemented by this build.
const RunnerContractVersion uint32 = 1

// PhysicalContractVersion identifies the meaning of intermediate physical digests.
const PhysicalContractVersion uint32 = 1

// Planner records the immutable provenance of an artifact.
type Planner struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// PhaseMode determines the transaction boundary used to execute one phase.
type PhaseMode string

const (
	// PhaseTransaction executes every phase step and its ledger completion in
	// one database transaction.
	PhaseTransaction PhaseMode = "transaction"
	// PhaseBatch executes one typed keyset executor in bounded transactions.
	PhaseBatch PhaseMode = "batch"
	// PhaseNoTransaction executes one typed physical operation which cannot run
	// inside a database transaction. Each operation is its own resumable phase.
	PhaseNoTransaction PhaseMode = "no_transaction"
)

// Phase is one resumable execution boundary in an immutable artifact.
type Phase struct {
	ID                      string    `json:"id"`
	Mode                    PhaseMode `json:"mode"`
	PhysicalContractVersion uint32    `json:"physicalContractVersion"`
	BeforePhysicalDigest    string    `json:"beforePhysicalDigest"`
	AfterPhysicalDigest     string    `json:"afterPhysicalDigest"`
	Steps                   []Step    `json:"steps"`
}

// StepConcurrentIndex executes one structured PostgreSQL concurrent-index
// operation outside a database transaction.
const StepConcurrentIndex StepKind = "concurrent_index"

// StepMongoDBCreateIndex creates one planner-owned MongoDB index outside a
// database transaction. The payload freezes physical identity only; the
// adapter must reconstruct and exactly match the definition from the embedded
// manifest and versioned planner before touching a database.
const StepMongoDBCreateIndex StepKind = "mongodb_create_index"

// StepMongoDBDropIndex removes one planner-owned MongoDB index outside a
// database transaction. The adapter reconstructs the exact physical
// collection and index definition from the embedded manifests; the payload
// never carries an arbitrary command.
const StepMongoDBDropIndex StepKind = "mongodb_drop_index"

// StepMongoDBRenameResource moves one collection identity to another outside
// a database transaction. Only stable manifest identities are persisted; the
// MongoDB adapter derives the exact content and version namespaces.
const StepMongoDBRenameResource StepKind = "mongodb_rename_resource"

// StepMongoDBDropResources removes the content and version namespaces for one
// exact, sorted set of reviewed retired stable identities. Framework-owned
// shared state is removed by the preceding transactional retirement step.
const StepMongoDBDropResources StepKind = "mongodb_drop_resources"

// StepMongoDBAssertSchema verifies the complete planner-owned MongoDB catalog
// outside a database transaction after every physical step has completed.
const StepMongoDBAssertSchema StepKind = "mongodb_assert_schema"

// Step is one stable, independently ledgered executor invocation.
type Step struct {
	ID              string          `json:"id"`
	Kind            StepKind        `json:"kind"`
	ExecutorVersion uint32          `json:"executorVersion"`
	Name            string          `json:"name"`
	Payload         json.RawMessage `json:"payload"`
}

// SQLPayload contains SQL which is permitted only in a transaction phase.
type SQLPayload struct {
	SQL string `json:"sql"`
}

// RenamePayload contains one frozen content-identity rewrite.
type RenamePayload struct {
	Rename Rename `json:"rename"`
}

// PluginPayload contains one checksum-protected plugin migration.
type PluginPayload struct {
	Plugin PluginStep `json:"plugin"`
}

// DataTransformPayload identifies one application-compiled callback without
// serializing executable code into the immutable artifact.
type DataTransformPayload struct {
	Transform DataTransformDescriptor `json:"transform"`
}

// BackfillReferencesPayload configures the one supported keyset batch
// executor. BatchSize is an artifact property, not a runner-side guess.
type BackfillReferencesPayload struct {
	BatchSize uint32 `json:"batchSize"`
}

// RetireResourcesPayload identifies removed collection or global resources
// by their immutable stable IDs. IDs are strictly sorted so the artifact and
// its physical contract remain deterministic.
type RetireResourcesPayload struct {
	ResourceIDs          []schema.StableID `json:"resourceIds"`
	PurgeVersionOwnerIDs []schema.StableID `json:"purgeVersionOwnerIds,omitempty"`
}

// CanonicalizeAuthIdentitiesPayload identifies every auth identity field
// whose authored values and derived uniqueness keys move to the shared
// canonical-key contract.
type CanonicalizeAuthIdentitiesPayload struct {
	Resources []AuthIdentityResource `json:"resources"`
}

// AssertSchemaPayload is deliberately empty. The expected schema is the
// artifact's immutable after manifest.
type AssertSchemaPayload struct{}

// ConcurrentIndexAction selects typed concurrent index creation or removal.
type ConcurrentIndexAction string

const (
	ConcurrentIndexCreate ConcurrentIndexAction = "create"
	ConcurrentIndexDrop   ConcurrentIndexAction = "drop"
)

// ConcurrentIndexPayload is the closed concurrent-index vocabulary. Parts
// are PostgreSQL expressions emitted by the frozen planner; the runner quotes
// identifiers and never accepts a free-form no-transaction statement.
type ConcurrentIndexPayload struct {
	Action    ConcurrentIndexAction `json:"action"`
	Name      string                `json:"name"`
	Table     string                `json:"table,omitempty"`
	Unique    bool                  `json:"unique,omitempty"`
	Method    string                `json:"method,omitempty"`
	Parts     []string              `json:"parts,omitempty"`
	Predicate string                `json:"predicate,omitempty"`
}

// MongoDBCreateIndexPayload identifies one planner-owned physical index without
// serializing BSON, commands, or a portable schema language into the artifact.
type MongoDBCreateIndexPayload struct {
	Collection string `json:"collection"`
	Index      string `json:"index"`
}

// MongoDBDropIndexPayload identifies one adapter-owned index by the resource
// stable identity and deterministic index name.
type MongoDBDropIndexPayload struct {
	CollectionID schema.StableID `json:"collectionId"`
	Version      bool            `json:"version,omitempty"`
	Index        string          `json:"index"`
}

// MongoDBRenameResourcePayload binds one explicit collection identity rename.
type MongoDBRenameResourcePayload struct {
	BeforeID schema.StableID `json:"beforeId"`
	AfterID  schema.StableID `json:"afterId"`
}

// MongoDBDropResourcesPayload binds physical namespace removal to the same
// reviewed resource identities as the typed semantic retirement step.
type MongoDBDropResourcesPayload struct {
	ResourceIDs []schema.StableID `json:"resourceIds"`
}

var artifactID = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// MarshalStepPayload emits one compact, deterministic payload object.
func MarshalStepPayload(value any) (json.RawMessage, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(encoded), nil
}

// PhysicalDigestSeed returns the canonical starting identity for the
// artifact's reviewed physical execution contract.
func PhysicalDigestSeed(fromManifestDigest string) string {
	digest := sha256.Sum256([]byte("ridu-physical-v1\x00" + fromManifestDigest))
	return hex.EncodeToString(digest[:])
}

// PhasePhysicalDigest advances the reviewed physical execution contract by
// one complete phase. The database is additionally inspected against the
// embedded manifests at the artifact boundaries.
func PhasePhysicalDigest(before string, mode PhaseMode, steps []Step) (string, error) {
	encoded, err := json.Marshal(struct {
		Before string    `json:"before"`
		Mode   PhaseMode `json:"mode"`
		Steps  []Step    `json:"steps"`
	}{Before: before, Mode: mode, Steps: steps})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func (artifact Artifact) validate() error {
	if artifact.MinimumRunnerContract != RunnerContractVersion {
		return fmt.Errorf("migration %s requires unsupported runner contract %d", artifact.Name, artifact.MinimumRunnerContract)
	}
	if len(artifact.Phases) == 0 {
		return fmt.Errorf("migration %s has no phases", artifact.Name)
	}
	if !artifactID.MatchString(artifact.Planner.Name) {
		return fmt.Errorf("migration %s planner name %q is malformed", artifact.Name, artifact.Planner.Name)
	}
	phaseIDs := make(map[string]struct{}, len(artifact.Phases))
	stepIDs := make(map[string]struct{})
	assertions := 0
	mongoDBSteps := 0
	retirementSteps := 0
	mongoDBDropResourceSteps := 0
	canonicalAuthSteps := 0
	var canonicalAuthResources []AuthIdentityResource
	var retiredResourceIDs []schema.StableID
	var mongoDBDroppedResourceIDs []schema.StableID
	var mongoDBResourceRenames []MongoDBRenameResourcePayload
	retirementPhaseIndex, retirementStepIndex := -1, -1
	mongoDBDropPhaseIndex := -1
	var pluginSteps []PluginStep
	lastStepKind := StepKind("")
	previousPhysicalDigest := ""
	for phaseIndex, phase := range artifact.Phases {
		if !artifactID.MatchString(phase.ID) {
			return fmt.Errorf("migration %s phase %d has malformed id %q", artifact.Name, phaseIndex, phase.ID)
		}
		if _, duplicate := phaseIDs[phase.ID]; duplicate {
			return fmt.Errorf("migration %s has duplicate phase id %q", artifact.Name, phase.ID)
		}
		phaseIDs[phase.ID] = struct{}{}
		if phase.PhysicalContractVersion != PhysicalContractVersion {
			return fmt.Errorf("migration %s phase %s has unsupported physical contract %d", artifact.Name, phase.ID, phase.PhysicalContractVersion)
		}
		if !validDigest(phase.BeforePhysicalDigest) || !validDigest(phase.AfterPhysicalDigest) {
			return fmt.Errorf("migration %s phase %s has malformed physical digest", artifact.Name, phase.ID)
		}
		if phaseIndex != 0 && phase.BeforePhysicalDigest != previousPhysicalDigest {
			return fmt.Errorf("migration %s phase %s breaks physical digest lineage", artifact.Name, phase.ID)
		}
		if phaseIndex == 0 && phase.BeforePhysicalDigest != PhysicalDigestSeed(artifact.FromDigest) {
			return fmt.Errorf("migration %s phase %s has the wrong physical contract seed", artifact.Name, phase.ID)
		}
		previousPhysicalDigest = phase.AfterPhysicalDigest
		if len(phase.Steps) == 0 {
			return fmt.Errorf("migration %s phase %s has no steps", artifact.Name, phase.ID)
		}
		if phase.Mode == PhaseBatch && len(phase.Steps) != 1 {
			return fmt.Errorf("migration %s batch phase %s must contain exactly one step", artifact.Name, phase.ID)
		}
		if phase.Mode == PhaseNoTransaction && len(phase.Steps) != 1 {
			return fmt.Errorf("migration %s no-transaction phase %s must contain exactly one step", artifact.Name, phase.ID)
		}
		for stepIndex, step := range phase.Steps {
			if !artifactID.MatchString(step.ID) {
				return fmt.Errorf("migration %s phase %s step %d has malformed id %q", artifact.Name, phase.ID, stepIndex, step.ID)
			}
			if _, duplicate := stepIDs[step.ID]; duplicate {
				return fmt.Errorf("migration %s has duplicate step id %q", artifact.Name, step.ID)
			}
			stepIDs[step.ID] = struct{}{}
			if step.ExecutorVersion != 1 {
				return fmt.Errorf("migration %s step %s has unsupported executor version %d", artifact.Name, step.ID, step.ExecutorVersion)
			}
			if strings.TrimSpace(step.Name) == "" {
				return fmt.Errorf("migration %s step %s has no name", artifact.Name, step.ID)
			}
			if err := validateStepPayload(phase.Mode, step); err != nil {
				return fmt.Errorf("migration %s step %s: %w", artifact.Name, step.ID, err)
			}
			if step.Kind == StepRetireResources {
				retirementSteps++
				retirementPhaseIndex, retirementStepIndex = phaseIndex, stepIndex
				var payload RetireResourcesPayload
				if err := json.Unmarshal(step.Payload, &payload); err != nil {
					return fmt.Errorf("migration %s step %s has malformed resource-retirement payload", artifact.Name, step.ID)
				}
				retiredResourceIDs = append(retiredResourceIDs, payload.ResourceIDs...)
				if artifact.Planner.Name != "mongodb" && (stepIndex+1 >= len(phase.Steps) || phase.Steps[stepIndex+1].Kind != StepSQL) {
					return fmt.Errorf("migration %s step %s resource retirement must immediately precede transactional physical SQL", artifact.Name, step.ID)
				}
			}
			if step.Kind == StepMongoDBDropResources {
				mongoDBDropResourceSteps++
				mongoDBDropPhaseIndex = phaseIndex
				var payload MongoDBDropResourcesPayload
				if err := json.Unmarshal(step.Payload, &payload); err != nil {
					return fmt.Errorf("migration %s step %s has malformed MongoDB resource-drop payload", artifact.Name, step.ID)
				}
				mongoDBDroppedResourceIDs = append(mongoDBDroppedResourceIDs, payload.ResourceIDs...)
			}
			if step.Kind == StepMongoDBRenameResource {
				var payload MongoDBRenameResourcePayload
				if err := json.Unmarshal(step.Payload, &payload); err != nil {
					return fmt.Errorf("migration %s step %s has malformed MongoDB resource-rename payload", artifact.Name, step.ID)
				}
				mongoDBResourceRenames = append(mongoDBResourceRenames, payload)
			}
			if step.Kind == StepPluginSQL {
				var payload PluginPayload
				if err := json.Unmarshal(step.Payload, &payload); err != nil {
					return fmt.Errorf("migration %s step %s has malformed plugin payload", artifact.Name, step.ID)
				}
				pluginSteps = append(pluginSteps, payload.Plugin)
			}
			if step.Kind == StepCanonicalizeAuthIdentities {
				canonicalAuthSteps++
				var payload CanonicalizeAuthIdentitiesPayload
				if err := json.Unmarshal(step.Payload, &payload); err != nil {
					return fmt.Errorf("migration %s step %s has malformed auth-identity canonicalization payload", artifact.Name, step.ID)
				}
				canonicalAuthResources = append(canonicalAuthResources, payload.Resources...)
			}
			if isSchemaAssertion(step.Kind) {
				assertions++
			}
			if step.Kind == StepMongoDBCreateIndex || step.Kind == StepMongoDBDropIndex ||
				step.Kind == StepMongoDBRenameResource || step.Kind == StepMongoDBDropResources ||
				step.Kind == StepMongoDBAssertSchema {
				mongoDBSteps++
			}
			lastStepKind = step.Kind
		}
		expectedPhysicalDigest, err := PhasePhysicalDigest(phase.BeforePhysicalDigest, phase.Mode, phase.Steps)
		if err != nil {
			return err
		}
		if phase.AfterPhysicalDigest != expectedPhysicalDigest {
			return fmt.Errorf("migration %s phase %s physical contract digest mismatch", artifact.Name, phase.ID)
		}
	}
	if assertions != 1 || !isSchemaAssertion(lastStepKind) {
		return fmt.Errorf("migration %s must end with exactly one schema assertion", artifact.Name)
	}
	if mongoDBSteps != 0 && lastStepKind != StepMongoDBAssertSchema {
		return fmt.Errorf("migration %s with MongoDB physical steps must end with a MongoDB schema assertion", artifact.Name)
	}
	collectionRenames, err := artifact.collectionRenameBindings()
	if err != nil {
		return fmt.Errorf("migration %s content rename does not match manifest identity: %w", artifact.Name, err)
	}
	if artifact.Planner.Name == "mongodb" {
		if !sameMongoDBResourceRenameBindings(mongoDBResourceRenames, collectionRenames) {
			return fmt.Errorf("migration %s MongoDB physical resource renames do not exactly match reviewed content renames", artifact.Name)
		}
	} else if mongoDBSteps != 0 {
		return fmt.Errorf("migration %s uses MongoDB physical steps with planner %q", artifact.Name, artifact.Planner.Name)
	}
	expectedRetiredResourceIDs := artifact.removedResourceIDs(collectionRenames)
	if err := artifact.validatePluginSteps(pluginSteps); err != nil {
		return fmt.Errorf("migration %s plugin migration does not match embedded manifests: %w", artifact.Name, err)
	}
	if retirementSteps > 1 || !sameStableIDs(retiredResourceIDs, expectedRetiredResourceIDs) {
		return fmt.Errorf("migration %s resource retirement does not exactly match removed resources: got %v, want %v", artifact.Name, retiredResourceIDs, expectedRetiredResourceIDs)
	}
	if artifact.Planner.Name == "mongodb" {
		if len(expectedRetiredResourceIDs) == 0 {
			if mongoDBDropResourceSteps != 0 {
				return fmt.Errorf("migration %s has a MongoDB resource drop without retired resources", artifact.Name)
			}
		} else if mongoDBDropResourceSteps != 1 || !sameStableIDs(mongoDBDroppedResourceIDs, expectedRetiredResourceIDs) ||
			retirementPhaseIndex < 0 || retirementStepIndex < 0 || mongoDBDropPhaseIndex <= retirementPhaseIndex {
			return fmt.Errorf("migration %s MongoDB resource drop does not exactly follow reviewed retirement: got %v, want %v", artifact.Name, mongoDBDroppedResourceIDs, expectedRetiredResourceIDs)
		}
	} else if mongoDBDropResourceSteps != 0 {
		return fmt.Errorf("migration %s uses MongoDB resource-drop steps with planner %q", artifact.Name, artifact.Planner.Name)
	}
	if len(expectedRetiredResourceIDs) != 0 {
		foundRisk := false
		for _, risk := range artifact.Risks {
			if risk.Code == "RIDU_RETIRE_RESOURCE_STATE" && risk.Level == RiskDestructive {
				foundRisk = true
				break
			}
		}
		if !foundRisk {
			return fmt.Errorf("migration %s resource retirement lacks its destructive risk", artifact.Name)
		}
	}
	if canonicalAuthSteps > 1 {
		return fmt.Errorf("migration %s has more than one auth-identity canonicalization step", artifact.Name)
	}
	if canonicalAuthSteps == 1 {
		if artifact.Before == nil {
			return fmt.Errorf("migration %s initial artifact cannot canonicalize auth identities", artifact.Name)
		}
		expected := RetainedAuthIdentityResources(*artifact.Before, artifact.After)
		if !sameAuthIdentityResources(canonicalAuthResources, expected) {
			return fmt.Errorf("migration %s auth-identity canonicalization does not exactly match retained identity fields", artifact.Name)
		}
	}
	return nil
}

func sameMongoDBResourceRenameBindings(actual []MongoDBRenameResourcePayload, expected map[schema.StableID]schema.StableID) bool {
	if len(actual) != len(expected) {
		return false
	}
	seenSources := make(map[schema.StableID]struct{}, len(actual))
	seenTargets := make(map[schema.StableID]struct{}, len(actual))
	for _, rename := range actual {
		if expected[rename.BeforeID] != rename.AfterID || rename.BeforeID == rename.AfterID {
			return false
		}
		if _, duplicate := seenSources[rename.BeforeID]; duplicate {
			return false
		}
		if _, duplicate := seenTargets[rename.AfterID]; duplicate {
			return false
		}
		seenSources[rename.BeforeID] = struct{}{}
		seenTargets[rename.AfterID] = struct{}{}
	}
	return true
}

func (artifact Artifact) validatePluginSteps(actual []PluginStep) error {
	plannerAdapter, plannerOwnsPluginSQL := pluginAdapterForPlanner(artifact.Planner.Name)
	if len(actual) == 0 {
		if !plannerOwnsPluginSQL {
			return nil
		}
		expected, err := artifact.expectedPluginSteps(plannerAdapter)
		if err != nil {
			return err
		}
		if len(expected) != 0 {
			return fmt.Errorf("got 0 plugin steps, want %d for %s", len(expected), plannerAdapter)
		}
		return nil
	}
	adapter := actual[0].Adapter
	if adapter != schema.PluginDatabaseAdapterPostgres && adapter != schema.PluginDatabaseAdapterSQLite {
		return fmt.Errorf("plugin step uses unsupported database adapter %q", adapter)
	}
	if plannerOwnsPluginSQL && adapter != plannerAdapter {
		return fmt.Errorf("planner %q requires %s plugin steps, got %s", artifact.Planner.Name, plannerAdapter, adapter)
	}
	for _, step := range actual[1:] {
		if step.Adapter != adapter {
			return fmt.Errorf("migration artifact mixes %s and %s plugin steps", adapter, step.Adapter)
		}
	}
	expected, err := artifact.expectedPluginSteps(adapter)
	if err != nil {
		return err
	}
	if len(actual) != len(expected) {
		return fmt.Errorf("got %d plugin steps, want %d for %s", len(actual), len(expected), adapter)
	}
	for index := range expected {
		if !samePluginStep(actual[index], expected[index]) {
			return fmt.Errorf("step %d is %s/%s/%d/%s, want %s/%s/%d/%s with exact manifest SQL", index, actual[index].Adapter, actual[index].Plugin, actual[index].Version, actual[index].Direction, expected[index].Adapter, expected[index].Plugin, expected[index].Version, expected[index].Direction)
		}
	}
	return nil
}

func pluginAdapterForPlanner(planner string) (schema.PluginDatabaseAdapter, bool) {
	switch planner {
	case "atlas":
		return schema.PluginDatabaseAdapterPostgres, true
	case "ridu-sqlite":
		return schema.PluginDatabaseAdapterSQLite, true
	default:
		return "", false
	}
}

func (artifact Artifact) expectedPluginSteps(adapter schema.PluginDatabaseAdapter) ([]PluginStep, error) {
	before := make(map[string]schema.Plugin)
	if artifact.Before != nil {
		for _, plugin := range artifact.Before.Plugins {
			before[plugin.Key] = plugin
		}
	}
	after := make(map[string]schema.Plugin)
	for _, plugin := range artifact.After.Plugins {
		after[plugin.Key] = plugin
	}
	keys := make([]string, 0, len(before)+len(after))
	seen := make(map[string]bool, len(before)+len(after))
	for key := range before {
		seen[key] = true
		keys = append(keys, key)
	}
	for key := range after {
		if !seen[key] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	var expected []PluginStep
	for _, key := range keys {
		previous, hadPrevious := before[key]
		next, hasNext := after[key]
		var previousMigrations, nextMigrations []schema.PluginMigration
		if hadPrevious && previous.HasDatabaseContributions() {
			contribution, supported := previous.DatabaseContribution(adapter)
			if !supported {
				return nil, fmt.Errorf("plugin %q has private database schema but does not support %s", key, adapter)
			}
			previousMigrations = contribution.Migrations
		}
		if hasNext && next.HasDatabaseContributions() {
			contribution, supported := next.DatabaseContribution(adapter)
			if !supported {
				return nil, fmt.Errorf("plugin %q has private database schema but does not support %s", key, adapter)
			}
			nextMigrations = contribution.Migrations
		}
		shared := len(previousMigrations)
		if len(nextMigrations) < shared {
			shared = len(nextMigrations)
		}
		for index := 0; index < shared; index++ {
			if !samePluginMigration(previousMigrations[index], nextMigrations[index]) {
				return nil, fmt.Errorf("plugin %s %s migration %d changed after publication", key, adapter, index+1)
			}
		}
		if hasNext && len(nextMigrations) > len(previousMigrations) {
			for index := len(previousMigrations); index < len(nextMigrations); index++ {
				migration := nextMigrations[index]
				step := PluginStep{Adapter: adapter, Plugin: key, Version: migration.Version, Direction: "up", SQL: append([]string(nil), migration.UpSQL...)}
				step.Checksum = PluginStepChecksum(step.Adapter, step.Plugin, step.Version, step.Direction, step.SQL)
				expected = append(expected, step)
			}
		}
		if hadPrevious && (!hasNext || len(nextMigrations) < len(previousMigrations)) {
			for index := len(previousMigrations) - 1; index >= len(nextMigrations); index-- {
				migration := previousMigrations[index]
				step := PluginStep{Adapter: adapter, Plugin: key, Version: migration.Version, Direction: "down", SQL: append([]string(nil), migration.DownSQL...)}
				step.Checksum = PluginStepChecksum(step.Adapter, step.Plugin, step.Version, step.Direction, step.SQL)
				expected = append(expected, step)
			}
		}
	}
	return expected, nil
}

func samePluginMigration(left, right schema.PluginMigration) bool {
	return left.Version == right.Version && left.Name == right.Name && sameStrings(left.UpSQL, right.UpSQL) && sameStrings(left.DownSQL, right.DownSQL)
}

func samePluginStep(left, right PluginStep) bool {
	return left.Adapter == right.Adapter && left.Plugin == right.Plugin && left.Version == right.Version && left.Direction == right.Direction && left.Checksum == right.Checksum && sameStrings(left.SQL, right.SQL)
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func (artifact Artifact) removedResourceIDs(collectionRenames map[schema.StableID]schema.StableID) []schema.StableID {
	if artifact.Before == nil {
		return nil
	}
	removed := make([]schema.StableID, 0)
	for _, previous := range artifact.Before.Collections {
		present := false
		for _, current := range artifact.After.Collections {
			present = present || current.ID == previous.ID
		}
		if _, renamed := collectionRenames[previous.ID]; !present && !renamed {
			removed = append(removed, previous.ID)
		}
	}
	for _, previous := range artifact.Before.Globals {
		present := false
		for _, current := range artifact.After.Globals {
			present = present || current.ID == previous.ID
		}
		if !present {
			removed = append(removed, previous.ID)
		}
	}
	sort.Slice(removed, func(left, right int) bool { return removed[left] < removed[right] })
	return removed
}

func sameStableIDs(left, right []schema.StableID) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func (artifact Artifact) collectionRenameBindings() (map[schema.StableID]schema.StableID, error) {
	bindings := make(map[schema.StableID]schema.StableID)
	targets := make(map[schema.StableID]schema.StableID)
	type fieldCollectionBinding struct {
		before schema.Collection
		after  schema.Collection
	}
	var fieldBindings []fieldCollectionBinding
	for _, phase := range artifact.Phases {
		for _, step := range phase.Steps {
			if step.Kind != StepRenameContent {
				continue
			}
			var payload RenamePayload
			if err := json.Unmarshal(step.Payload, &payload); err != nil {
				return nil, fmt.Errorf("step %s has malformed content-rename payload", step.ID)
			}
			before, beforeFound := collectionBySlug(artifact.Before, payload.Rename.CollectionBefore)
			after, afterFound := collectionBySlug(&artifact.After, payload.Rename.CollectionAfter)
			if !beforeFound || !afterFound {
				return nil, fmt.Errorf("step %s addresses absent collection %q -> %q", step.ID, payload.Rename.CollectionBefore, payload.Rename.CollectionAfter)
			}
			if payload.Rename.FieldBefore != "" || payload.Rename.FieldAfter != "" {
				fieldBindings = append(fieldBindings, fieldCollectionBinding{before: before, after: after})
				continue
			}
			if before.ID == after.ID {
				// A public slug rewrite preserves physical/resource identity while
				// migrating persisted polymorphic and plugin-owned slug references.
				// Store runners bind its exact before/after topology independently.
				continue
			}
			if resourceIDExists(artifact.After, before.ID) {
				return nil, fmt.Errorf("step %s rename source %s remains in the after manifest", step.ID, before.ID)
			}
			if resourceIDExists(*artifact.Before, after.ID) {
				return nil, fmt.Errorf("step %s rename target %s already existed in the before manifest", step.ID, after.ID)
			}
			if previousTarget, existed := collectionBySlug(artifact.Before, after.Slug); existed && previousTarget.ID != before.ID {
				return nil, fmt.Errorf("step %s rename target slug %q belonged to different before collection %s", step.ID, after.Slug, previousTarget.ID)
			}
			if existing, duplicate := bindings[before.ID]; duplicate {
				return nil, fmt.Errorf("step %s duplicates rename source %s already mapped to %s", step.ID, before.ID, existing)
			}
			if existing, duplicate := targets[after.ID]; duplicate {
				return nil, fmt.Errorf("step %s duplicates rename target %s already mapped from %s", step.ID, after.ID, existing)
			}
			bindings[before.ID] = after.ID
			targets[after.ID] = before.ID
		}
	}
	for _, field := range fieldBindings {
		if field.before.ID == field.after.ID || bindings[field.before.ID] == field.after.ID {
			continue
		}
		return nil, fmt.Errorf("field rename crosses unconfirmed collection identity %s -> %s", field.before.ID, field.after.ID)
	}
	return bindings, nil
}

func collectionBySlug(snapshot *schema.Snapshot, slug schema.CollectionSlug) (schema.Collection, bool) {
	if snapshot == nil {
		return schema.Collection{}, false
	}
	for _, collection := range snapshot.Collections {
		if collection.Slug == slug {
			return collection, true
		}
	}
	return schema.Collection{}, false
}

func resourceIDExists(snapshot schema.Snapshot, id schema.StableID) bool {
	for _, collection := range snapshot.Collections {
		if collection.ID == id {
			return true
		}
	}
	for _, global := range snapshot.Globals {
		if global.ID == id {
			return true
		}
	}
	return false
}

func validateStepPayload(mode PhaseMode, step Step) error {
	if len(step.Payload) == 0 {
		return fmt.Errorf("payload is required")
	}
	if err := rejectDuplicateJSONKeys(step.Payload); err != nil {
		return fmt.Errorf("invalid payload: %w", err)
	}
	switch step.Kind {
	case StepSQL:
		if mode != PhaseTransaction {
			return fmt.Errorf("SQL is allowed only in a transaction phase")
		}
		var payload SQLPayload
		if err := decodeStrictJSON(step.Payload, &payload); err != nil || strings.TrimSpace(payload.SQL) == "" {
			return fmt.Errorf("malformed SQL payload")
		}
	case StepRenameContent:
		if mode != PhaseTransaction {
			return fmt.Errorf("content rename is allowed only in a transaction phase")
		}
		var payload RenamePayload
		if err := decodeStrictJSON(step.Payload, &payload); err != nil || validateRename(payload.Rename) != nil {
			return fmt.Errorf("malformed content-rename payload")
		}
	case StepPluginSQL:
		if mode != PhaseTransaction {
			return fmt.Errorf("plugin SQL is allowed only in a transaction phase")
		}
		var payload PluginPayload
		if err := decodeStrictJSON(step.Payload, &payload); err != nil || validatePlugin(payload.Plugin) != nil {
			return fmt.Errorf("malformed plugin payload")
		}
	case StepBackfillReferences:
		if mode != PhaseBatch {
			return fmt.Errorf("reference backfill is allowed only in a batch phase")
		}
		var payload BackfillReferencesPayload
		if err := decodeStrictJSON(step.Payload, &payload); err != nil || payload.BatchSize == 0 || payload.BatchSize > 10_000 {
			return fmt.Errorf("malformed reference-backfill payload")
		}
	case StepRetireResources:
		if mode != PhaseTransaction {
			return fmt.Errorf("resource retirement is allowed only in a transaction phase")
		}
		var payload RetireResourcesPayload
		if err := decodeStrictJSON(step.Payload, &payload); err != nil || validateRetireResources(payload) != nil {
			return fmt.Errorf("malformed resource-retirement payload")
		}
	case StepDataTransform:
		if mode != PhaseTransaction {
			return fmt.Errorf("data transform is allowed only in a transaction phase")
		}
		var payload DataTransformPayload
		if err := decodeStrictJSON(step.Payload, &payload); err != nil || payload.Transform.Validate() != nil {
			return fmt.Errorf("malformed data-transform payload")
		}
	case StepCanonicalizeAuthIdentities:
		if mode != PhaseTransaction {
			return fmt.Errorf("auth identity canonicalization is allowed only in a transaction phase")
		}
		var payload CanonicalizeAuthIdentitiesPayload
		if err := decodeStrictJSON(step.Payload, &payload); err != nil || validateAuthIdentityResources(payload.Resources) != nil {
			return fmt.Errorf("malformed auth-identity canonicalization payload")
		}
	case StepConcurrentIndex:
		if mode != PhaseNoTransaction {
			return fmt.Errorf("concurrent index operation is allowed only in a no-transaction phase")
		}
		var payload ConcurrentIndexPayload
		if err := decodeStrictJSON(step.Payload, &payload); err != nil || validateConcurrentIndex(payload) != nil {
			return fmt.Errorf("malformed concurrent-index payload")
		}
	case StepMongoDBCreateIndex:
		if mode != PhaseNoTransaction {
			return fmt.Errorf("MongoDB index creation is allowed only in a no-transaction phase")
		}
		var payload MongoDBCreateIndexPayload
		if err := decodeStrictJSON(step.Payload, &payload); err != nil || validateMongoDBCreateIndex(payload) != nil {
			return fmt.Errorf("malformed MongoDB-create-index payload")
		}
	case StepMongoDBDropIndex:
		if mode != PhaseNoTransaction {
			return fmt.Errorf("MongoDB index removal is allowed only in a no-transaction phase")
		}
		var payload MongoDBDropIndexPayload
		if err := decodeStrictJSON(step.Payload, &payload); err != nil || validateMongoDBDropIndex(payload) != nil {
			return fmt.Errorf("malformed MongoDB-drop-index payload")
		}
	case StepMongoDBRenameResource:
		if mode != PhaseNoTransaction {
			return fmt.Errorf("MongoDB resource rename is allowed only in a no-transaction phase")
		}
		var payload MongoDBRenameResourcePayload
		if err := decodeStrictJSON(step.Payload, &payload); err != nil || validateMongoDBRenameResource(payload) != nil {
			return fmt.Errorf("malformed MongoDB-resource-rename payload")
		}
	case StepMongoDBDropResources:
		if mode != PhaseNoTransaction {
			return fmt.Errorf("MongoDB resource removal is allowed only in a no-transaction phase")
		}
		var payload MongoDBDropResourcesPayload
		if err := decodeStrictJSON(step.Payload, &payload); err != nil || validateMongoDBDropResources(payload) != nil {
			return fmt.Errorf("malformed MongoDB-resource-drop payload")
		}
	case StepMongoDBAssertSchema:
		if mode != PhaseNoTransaction {
			return fmt.Errorf("MongoDB schema assertion is allowed only in a no-transaction phase")
		}
		var payload AssertSchemaPayload
		if err := decodeStrictJSON(step.Payload, &payload); err != nil || !bytes.Equal(step.Payload, []byte("{}")) {
			return fmt.Errorf("malformed MongoDB-schema-assertion payload")
		}
	case StepAssertSchema:
		if mode != PhaseTransaction {
			return fmt.Errorf("schema assertion is allowed only in a transaction phase")
		}
		var payload AssertSchemaPayload
		if err := decodeStrictJSON(step.Payload, &payload); err != nil || !bytes.Equal(bytes.TrimSpace(step.Payload), []byte("{}")) {
			return fmt.Errorf("malformed schema-assertion payload")
		}
	default:
		return fmt.Errorf("unknown kind %q", step.Kind)
	}
	return nil
}

func validateAuthIdentityResources(resources []AuthIdentityResource) error {
	if len(resources) == 0 {
		return fmt.Errorf("auth identity resources are required")
	}
	previous := ""
	for _, resource := range resources {
		key := string(resource.CollectionID) + "\x00" + string(resource.FieldID) + "\x00" + resource.FieldName
		if !schema.IsValidStableID(string(resource.CollectionID)) || !schema.IsValidStableID(string(resource.FieldID)) || !schema.IsValidFieldName(resource.FieldName) || previous != "" && key <= previous {
			return fmt.Errorf("auth identity resources must be valid, unique, and strictly sorted")
		}
		previous = key
	}
	return nil
}

// AuthIdentityResources returns the deterministic complete identity-field
// scope for a manifest snapshot. Adapter planners use it to bind the typed
// canonicalization step to immutable schema identities.
func AuthIdentityResources(snapshot schema.Snapshot) []AuthIdentityResource {
	resources := make([]AuthIdentityResource, 0)
	for _, collection := range snapshot.Collections {
		if collection.Auth == nil {
			continue
		}
		for _, field := range collection.Fields {
			if field.Name == collection.Auth.IdentityField {
				resources = append(resources, AuthIdentityResource{CollectionID: collection.ID, FieldID: field.ID, FieldName: field.Name})
				break
			}
		}
	}
	sort.Slice(resources, func(left, right int) bool {
		leftKey := string(resources[left].CollectionID) + "\x00" + string(resources[left].FieldID) + "\x00" + resources[left].FieldName
		rightKey := string(resources[right].CollectionID) + "\x00" + string(resources[right].FieldID) + "\x00" + resources[right].FieldName
		return leftKey < rightKey
	})
	return resources
}

// RetainedAuthIdentityResources returns only identity fields whose collection,
// field identity, and authored field name are unchanged across a transition.
// A forward canonicalization must never inspect a newly introduced auth
// resource before its adapter-owned physical storage exists.
func RetainedAuthIdentityResources(before, after schema.Snapshot) []AuthIdentityResource {
	legacy := AuthIdentityResources(before)
	legacySet := make(map[AuthIdentityResource]struct{}, len(legacy))
	for _, resource := range legacy {
		legacySet[resource] = struct{}{}
	}
	retained := make([]AuthIdentityResource, 0, len(legacy))
	for _, resource := range AuthIdentityResources(after) {
		if _, exists := legacySet[resource]; exists {
			retained = append(retained, resource)
		}
	}
	return retained
}

func sameAuthIdentityResources(left, right []AuthIdentityResource) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func validateRetireResources(payload RetireResourcesPayload) error {
	if len(payload.ResourceIDs) == 0 {
		return fmt.Errorf("resource IDs are required")
	}
	validateIDs := func(ids []schema.StableID) bool {
		previous := ""
		for _, id := range ids {
			value := string(id)
			if !schema.IsValidStableID(value) || previous != "" && value <= previous {
				return false
			}
			previous = value
		}
		return true
	}
	if !validateIDs(payload.ResourceIDs) || !validateIDs(payload.PurgeVersionOwnerIDs) {
		return fmt.Errorf("resource and version-owner IDs must be unique, valid, and strictly sorted")
	}
	retired := make(map[schema.StableID]struct{}, len(payload.ResourceIDs))
	for _, id := range payload.ResourceIDs {
		retired[id] = struct{}{}
	}
	for _, id := range payload.PurgeVersionOwnerIDs {
		if _, overlap := retired[id]; overlap {
			return fmt.Errorf("a retired resource cannot also be a surviving version owner")
		}
	}
	return nil
}

func validateRename(rename Rename) error {
	if rename.CollectionBefore == "" || rename.CollectionAfter == "" {
		return fmt.Errorf("incomplete collection intent")
	}
	if (rename.FieldBefore == "") != (rename.FieldAfter == "") || rename.FieldBefore != "" && rename.FieldBefore == rename.FieldAfter {
		return fmt.Errorf("malformed field intent")
	}
	for _, pair := range rename.Fields {
		if pair.Before == "" || pair.After == "" || pair.Before == pair.After {
			return fmt.Errorf("malformed field pair")
		}
	}
	return nil
}

func validatePlugin(plugin PluginStep) error {
	if plugin.Adapter != schema.PluginDatabaseAdapterPostgres && plugin.Adapter != schema.PluginDatabaseAdapterSQLite || !schemaPluginKey(plugin.Plugin) || plugin.Version == 0 || plugin.Direction != "up" && plugin.Direction != "down" || len(plugin.SQL) == 0 {
		return fmt.Errorf("malformed plugin")
	}
	for _, statement := range plugin.SQL {
		if strings.TrimSpace(statement) == "" {
			return fmt.Errorf("empty plugin SQL")
		}
	}
	if plugin.Checksum != PluginStepChecksum(plugin.Adapter, plugin.Plugin, plugin.Version, plugin.Direction, plugin.SQL) {
		return fmt.Errorf("plugin checksum mismatch")
	}
	return nil
}

func schemaPluginKey(key string) bool {
	return schema.IsValidPluginKey(key)
}

func validateConcurrentIndex(payload ConcurrentIndexPayload) error {
	if !postgresIdentifier(payload.Name) {
		return fmt.Errorf("invalid index name")
	}
	switch payload.Action {
	case ConcurrentIndexCreate:
		if !postgresIdentifier(payload.Table) || len(payload.Parts) == 0 {
			return fmt.Errorf("incomplete create definition")
		}
		if _, supported := map[string]struct{}{"": {}, "BTREE": {}, "BRIN": {}, "HASH": {}, "GIN": {}, "GIST": {}, "SPGIST": {}}[payload.Method]; !supported {
			return fmt.Errorf("unsupported index method")
		}
		for _, part := range payload.Parts {
			if strings.TrimSpace(part) == "" || strings.ContainsRune(part, ';') {
				return fmt.Errorf("unsafe index part")
			}
		}
		if strings.ContainsRune(payload.Predicate, ';') {
			return fmt.Errorf("unsafe index predicate")
		}
	case ConcurrentIndexDrop:
		if payload.Table != "" || payload.Unique || payload.Method != "" || len(payload.Parts) != 0 || payload.Predicate != "" {
			return fmt.Errorf("drop definition has create-only fields")
		}
	default:
		return fmt.Errorf("unknown index action")
	}
	return nil
}

func validateMongoDBCreateIndex(payload MongoDBCreateIndexPayload) error {
	if !mongoDBPhysicalIdentifier(payload.Collection) || !mongoDBPhysicalIdentifier(payload.Index) || payload.Index == "_id_" {
		return fmt.Errorf("invalid MongoDB physical index identity")
	}
	return nil
}

func validateMongoDBDropIndex(payload MongoDBDropIndexPayload) error {
	if !schema.IsValidStableID(string(payload.CollectionID)) || !mongoDBPhysicalIdentifier(payload.Index) || payload.Index == "_id_" {
		return fmt.Errorf("invalid MongoDB physical index identity")
	}
	return nil
}

func validateMongoDBRenameResource(payload MongoDBRenameResourcePayload) error {
	if !schema.IsValidStableID(string(payload.BeforeID)) || !schema.IsValidStableID(string(payload.AfterID)) || payload.BeforeID == payload.AfterID {
		return fmt.Errorf("invalid MongoDB resource rename identity")
	}
	return nil
}

func validateMongoDBDropResources(payload MongoDBDropResourcesPayload) error {
	return validateRetireResources(RetireResourcesPayload{ResourceIDs: payload.ResourceIDs})
}

func isSchemaAssertion(kind StepKind) bool {
	return kind == StepAssertSchema || kind == StepMongoDBAssertSchema
}

func mongoDBPhysicalIdentifier(value string) bool {
	if value == "" || len(value) > 127 {
		return false
	}
	for index, candidate := range []byte(value) {
		if candidate >= 'a' && candidate <= 'z' || candidate == '_' || index > 0 && candidate >= '0' && candidate <= '9' {
			continue
		}
		return false
	}
	return true
}

func postgresIdentifier(value string) bool {
	if value == "" || len(value) > 63 {
		return false
	}
	for index, candidate := range []byte(value) {
		if candidate >= 'a' && candidate <= 'z' || candidate == '_' || index > 0 && candidate >= '0' && candidate <= '9' {
			continue
		}
		return false
	}
	return true
}

func validDigest(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
