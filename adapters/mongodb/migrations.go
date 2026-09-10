package mongodb

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/riducms/ridu/internal/embedded"
	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/internal/primitivefield"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
)

const (
	mongoDBPlannerName = "mongodb"
	// mongoDBPlannerVersion is the frozen development-qualified contract. Its
	// emitted artifacts and digests must remain byte-for-byte reproducible.
	mongoDBPlannerVersion   = "1.0.0"
	mongoDBPlannerVersionV2 = "2.0.0"
)

type mongoDBPlannerContract struct {
	version  string
	semantic bool
}

func currentMongoDBPlannerContract() mongoDBPlannerContract {
	return mongoDBPlannerContract{version: mongoDBPlannerVersionV2, semantic: true}
}

func mongoDBPlannerContractFor(version string) (mongoDBPlannerContract, bool) {
	switch version {
	case mongoDBPlannerVersion:
		return mongoDBPlannerContract{version: version}, true
	case mongoDBPlannerVersionV2:
		return currentMongoDBPlannerContract(), true
	default:
		return mongoDBPlannerContract{}, false
	}
}

// CreatedArtifact is the stable filesystem identity of one newly published
// immutable MongoDB migration artifact.
type CreatedArtifact struct {
	Path     string `json:"path"`
	Name     string `json:"name"`
	Checksum string `json:"checksum"`
	Version  uint32 `json:"version"`
}

// ArtifactOptions contains only reviewed semantic intent that can be frozen
// into an immutable MongoDB artifact. Callbacks themselves are never
// serialized; only their checksum-bound descriptors cross this boundary.
type ArtifactOptions struct {
	AllowDestructive bool
	Renames          []ridumigration.Rename
	DataTransforms   []ridumigration.DataTransformDescriptor
}

// SafetyError reports a valid MongoDB transition that still requires explicit
// destructive review before publication.
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

// CreateArtifact plans and atomically publishes one offline MongoDB migration
// with the frozen planner 1.0.0 contract retained for existing direct callers.
// New application migrations use CreateArtifactWithOptions and planner 2.0.0.
// This function reads only committed artifacts and the executable manifest;
// database URLs, secrets, and live MongoDB state are outside this boundary.
func CreateArtifact(ctx context.Context, directory, name string, after schema.Manifest, now time.Time) (CreatedArtifact, error) {
	return createMongoDBArtifactWithContract(ctx, directory, name, after, now, ArtifactOptions{}, mongoDBPlannerContract{version: mongoDBPlannerVersion})
}

// CreateArtifactWithOptions publishes one immutable MongoDB planner 2.0.0
// artifact with explicit semantic intent. Planning is offline and never opens
// MongoDB.
func CreateArtifactWithOptions(ctx context.Context, directory, name string, after schema.Manifest, now time.Time, options ArtifactOptions) (CreatedArtifact, error) {
	return createMongoDBArtifactWithContract(ctx, directory, name, after, now, options, currentMongoDBPlannerContract())
}

func createMongoDBArtifactWithContract(ctx context.Context, directory, name string, after schema.Manifest, now time.Time, options ArtifactOptions, contract mongoDBPlannerContract) (CreatedArtifact, error) {
	if ctx == nil {
		return CreatedArtifact{}, fmt.Errorf("MongoDB migration context is required")
	}
	if err := ctx.Err(); err != nil {
		return CreatedArtifact{}, err
	}
	files, err := migrationartifact.ReadAll(directory)
	if err != nil {
		return CreatedArtifact{}, err
	}
	if err := validateMongoDBArtifactHistory(ctx, files); err != nil {
		return CreatedArtifact{}, err
	}
	var before *schema.Manifest
	previousPlannerVersion := ""
	if len(files) != 0 {
		head := files[len(files)-1].Artifact
		latest, err := head.AfterManifest()
		if err != nil {
			return CreatedArtifact{}, err
		}
		before = &latest
		previousPlannerVersion = head.Planner.Version
	}
	if err := validateMongoDBDataTransformIdentities(files, options.DataTransforms); err != nil {
		return CreatedArtifact{}, err
	}
	artifact, err := buildMongoDBArtifactWithOptions(ctx, name, before, after, previousPlannerVersion, contract, options)
	if err != nil {
		return CreatedArtifact{}, err
	}
	if err := requireMongoDBDestructiveApproval(artifact.Risks, options.AllowDestructive); err != nil {
		return CreatedArtifact{}, err
	}
	file, err := publishMongoDBArtifact(directory, name, artifact, files, now)
	if err != nil {
		return CreatedArtifact{}, err
	}
	return CreatedArtifact{Path: file.Path, Name: file.Name, Checksum: file.Digest, Version: file.Artifact.Version}, nil
}

func publishMongoDBArtifact(
	directory string,
	name string,
	artifact ridumigration.Artifact,
	validatedHistory []migrationartifact.File,
	now time.Time,
) (migrationartifact.File, error) {
	if len(validatedHistory) != 0 {
		artifact.PreviousArtifactDigest = validatedHistory[len(validatedHistory)-1].Digest
	}
	return migrationartifact.Create(directory, name, artifact, now)
}

func validateMongoDBArtifactHistory(ctx context.Context, files []migrationartifact.File) error {
	if err := validateMongoDBDataTransformIdentities(files, nil); err != nil {
		return err
	}
	previousPlannerVersion := ""
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := validateMongoDBArtifactPlan(ctx, file.Artifact, file.Name, previousPlannerVersion); err != nil {
			return err
		}
		previousPlannerVersion = file.Artifact.Planner.Version
	}
	return nil
}

// validateMongoDBArtifactPlan binds one artifact to the complete private plan
// generated by its immutable planner version. A future runner must call this
// boundary before opening MongoDB or inspecting its catalog.
func validateMongoDBArtifactPlan(
	ctx context.Context,
	artifact ridumigration.Artifact,
	label string,
	previousPlannerVersion string,
) error {
	if artifact.Planner.Name != mongoDBPlannerName {
		return fmt.Errorf("MongoDB migration %s uses planner %q instead of %q", label, artifact.Planner.Name, mongoDBPlannerName)
	}
	if err := artifact.Validate(); err != nil {
		return fmt.Errorf("validate MongoDB migration %s: %w", label, err)
	}
	contract, supported := mongoDBPlannerContractFor(artifact.Planner.Version)
	if !supported {
		return fmt.Errorf("MongoDB migration %s uses unsupported planner version %q", label, artifact.Planner.Version)
	}
	after, err := artifact.AfterManifest()
	if err != nil {
		return err
	}
	var before *schema.Manifest
	if artifact.Before != nil {
		manifest, err := artifact.BeforeManifest()
		if err != nil {
			return err
		}
		before = &manifest
	}
	semanticOptions, err := mongoDBArtifactSemanticOptions(artifact)
	if err != nil {
		return fmt.Errorf("inspect MongoDB migration %s semantic intent: %w", label, err)
	}
	semanticOptions.AllowDestructive = true
	expected, err := buildMongoDBArtifactWithOptions(ctx, artifact.Name, before, after, previousPlannerVersion, contract, semanticOptions)
	if err != nil {
		return fmt.Errorf("validate MongoDB migration %s against planner: %w", label, err)
	}
	expected.PreviousArtifactDigest = artifact.PreviousArtifactDigest
	expectedDigest, err := expected.Digest()
	if err != nil {
		return err
	}
	artifactDigest, err := artifact.Digest()
	if err != nil {
		return err
	}
	if expectedDigest != artifactDigest {
		return fmt.Errorf("MongoDB migration %s does not match planner %s %s", label, mongoDBPlannerName, contract.version)
	}
	return nil
}

func buildMongoDBArtifact(
	ctx context.Context,
	name string,
	before *schema.Manifest,
	after schema.Manifest,
	previousPlannerVersion string,
	contract mongoDBPlannerContract,
) (ridumigration.Artifact, error) {
	return buildMongoDBArtifactWithOptions(ctx, name, before, after, previousPlannerVersion, contract, ArtifactOptions{})
}

func buildMongoDBArtifactWithOptions(
	ctx context.Context,
	name string,
	before *schema.Manifest,
	after schema.Manifest,
	previousPlannerVersion string,
	contract mongoDBPlannerContract,
	semanticOptions ArtifactOptions,
) (ridumigration.Artifact, error) {
	if ctx == nil {
		return ridumigration.Artifact{}, fmt.Errorf("MongoDB migration context is required")
	}
	if err := ctx.Err(); err != nil {
		return ridumigration.Artifact{}, err
	}
	if contract.version == "" {
		return ridumigration.Artifact{}, fmt.Errorf("MongoDB planner contract is incomplete")
	}
	if !contract.semantic && (len(semanticOptions.Renames) != 0 || len(semanticOptions.DataTransforms) != 0) {
		return ridumigration.Artifact{}, fmt.Errorf("MongoDB planner %s does not support semantic migration intent", contract.version)
	}
	if err := requireMongoDBMigrationPlugins(after); err != nil {
		return ridumigration.Artifact{}, err
	}
	if before == nil {
		if previousPlannerVersion != "" {
			return ridumigration.Artifact{}, fmt.Errorf("initial MongoDB artifact cannot have previous planner version %q", previousPlannerVersion)
		}
	} else {
		if err := requireMongoDBMigrationPlugins(*before); err != nil {
			return ridumigration.Artifact{}, err
		}
		if previousPlannerVersion == "" {
			return ridumigration.Artifact{}, fmt.Errorf("previous MongoDB planner version is required for a non-initial artifact")
		}
		if previousPlannerVersion != contract.version && !(previousPlannerVersion == mongoDBPlannerVersion && contract.version == mongoDBPlannerVersionV2) {
			return ridumigration.Artifact{}, fmt.Errorf("MongoDB planner version %q does not match current version %q", previousPlannerVersion, contract.version)
		}
		fromDigest, err := ridumigration.DigestManifest(*before)
		if err != nil {
			return ridumigration.Artifact{}, err
		}
		toDigest, err := ridumigration.DigestManifest(after)
		if err != nil {
			return ridumigration.Artifact{}, err
		}
		if fromDigest == toDigest && len(semanticOptions.DataTransforms) == 0 {
			return ridumigration.Artifact{}, fmt.Errorf("schema is current; no MongoDB migration steps were planned")
		}
		if contract.semantic {
			return buildMongoDBSemanticArtifact(ctx, name, before, after, previousPlannerVersion, contract, semanticOptions)
		}
		if err := validateMongoDBAdditiveTransition(before.Snapshot(), after.Snapshot()); err != nil {
			return ridumigration.Artifact{}, err
		}
	}
	if contract.semantic {
		return buildMongoDBSemanticArtifact(ctx, name, before, after, previousPlannerVersion, contract, semanticOptions)
	}

	var beforePlans mongoPhysicalIndexPlanSet
	var err error
	if before != nil {
		beforePlans, err = mongoPhysicalIndexPlans(*before)
		if err != nil {
			return ridumigration.Artifact{}, fmt.Errorf("plan previous MongoDB physical indexes: %w", err)
		}
	}
	afterPlans, err := mongoPhysicalIndexPlans(after)
	if err != nil {
		return ridumigration.Artifact{}, fmt.Errorf("plan MongoDB physical indexes: %w", err)
	}
	if err := validateMongoDBMigrationPlanUnion(beforePlans, afterPlans); err != nil {
		return ridumigration.Artifact{}, err
	}
	createdIndexes, risks, err := mongoDBAddedIndexPlans(beforePlans, afterPlans)
	if err != nil {
		return ridumigration.Artifact{}, err
	}

	artifact, err := ridumigration.NewArtifact(name, ridumigration.Planner{Name: mongoDBPlannerName, Version: contract.version}, before, after)
	if err != nil {
		return ridumigration.Artifact{}, err
	}
	artifact.Risks = risks
	artifact.Phases, err = mongoDBArtifactPhases(artifact.FromDigest, createdIndexes)
	if err != nil {
		return ridumigration.Artifact{}, err
	}
	if err := artifact.Validate(); err != nil {
		return ridumigration.Artifact{}, err
	}
	return artifact, nil
}

func requireMongoDBMigrationPlugins(manifest schema.Manifest) error {
	for _, plugin := range manifest.Snapshot().Plugins {
		if plugin.HasDatabaseContributions() {
			return fmt.Errorf("plugin %q has private database schema; MongoDB migrations do not support plugin database contributions", plugin.Key)
		}
	}
	return nil
}

func validateMongoDBMigrationPlanUnion(before, after mongoPhysicalIndexPlanSet) error {
	collections := append(append([]mongoCollectionIndexPlan(nil), before.collections...), after.collections...)
	system := append(append([]mongoSystemIndexPlan(nil), before.system...), after.system...)
	if err := validateMongoPhysicalIndexPlans(collections, system); err != nil {
		return fmt.Errorf("validate MongoDB migration physical-name union: %w", err)
	}
	return nil
}

type mongoDBPlannedIndex struct {
	collection  string
	name        string
	description string
	definition  mongoIndexDefinition
	resourceID  schema.StableID
	version     bool
}

func mongoDBAddedIndexPlans(before, after mongoPhysicalIndexPlanSet) ([]mongoDBPlannedIndex, []ridumigration.Risk, error) {
	previous := mongoDBFlattenedIndexPlans(before)
	next := mongoDBFlattenedIndexPlans(after)
	previousByKey := make(map[string]mongoDBPlannedIndex, len(previous))
	for _, planned := range previous {
		previousByKey[planned.collection+"\x00"+planned.name] = planned
	}
	nextByKey := make(map[string]mongoDBPlannedIndex, len(next))
	for _, planned := range next {
		nextByKey[planned.collection+"\x00"+planned.name] = planned
	}
	for _, planned := range previous {
		key := planned.collection + "\x00" + planned.name
		current, exists := nextByKey[key]
		if !exists {
			return nil, nil, fmt.Errorf("MongoDB artifact planner supports only additive physical transitions; index %q for %s was removed", planned.name, planned.description)
		}
		previousFingerprint, err := mongoIndexFingerprint([]mongoIndexDefinition{planned.definition})
		if err != nil {
			return nil, nil, fmt.Errorf("fingerprint previous MongoDB index %q for %s: %w", planned.name, planned.description, err)
		}
		currentFingerprint, err := mongoIndexFingerprint([]mongoIndexDefinition{current.definition})
		if err != nil {
			return nil, nil, fmt.Errorf("fingerprint current MongoDB index %q for %s: %w", current.name, current.description, err)
		}
		if previousFingerprint != currentFingerprint {
			return nil, nil, fmt.Errorf("MongoDB artifact planner supports only additive physical transitions; index %q for %s changed", planned.name, planned.description)
		}
	}
	var additions []mongoDBPlannedIndex
	var uniqueRisks []ridumigration.Risk
	for _, planned := range next {
		if _, existed := previousByKey[planned.collection+"\x00"+planned.name]; existed {
			continue
		}
		additions = append(additions, planned)
		if planned.definition.unique {
			uniqueRisks = append(uniqueRisks, ridumigration.Risk{
				Code: "RIDU_BUILD_UNIQUE_INDEX", Level: ridumigration.RiskWarning,
				Message: fmt.Sprintf("build unique MongoDB index %s for %s; the migration fails if existing indexed values conflict", planned.name, planned.description),
			})
		}
	}
	sort.Slice(uniqueRisks, func(left, right int) bool {
		return uniqueRisks[left].Message < uniqueRisks[right].Message
	})
	var risks []ridumigration.Risk
	if len(additions) != 0 {
		risks = append(risks, ridumigration.Risk{
			Code: "RIDU_MONGODB_INDEX_BUILD_NON_TRANSACTIONAL", Level: ridumigration.RiskWarning,
			Message: "MongoDB index builds run as resumable no-transaction phases; an interrupted artifact may leave reviewed indexes in place before the artifact completes",
		})
		risks = append(risks, uniqueRisks...)
	}
	return additions, risks, nil
}

func mongoDBFlattenedIndexPlans(plans mongoPhysicalIndexPlanSet) []mongoDBPlannedIndex {
	flattened := make([]mongoDBPlannedIndex, 0)
	for _, plan := range plans.collections {
		description := fmt.Sprintf("content resource %q", plan.collection.ID)
		for _, definition := range plan.definitions {
			flattened = append(flattened, mongoDBPlannedIndex{
				collection: plan.physicalName, name: definition.name, description: description, definition: definition,
				resourceID: plan.collection.ID,
			})
		}
	}
	for _, plan := range plans.system {
		for _, definition := range plan.definitions {
			planned := mongoDBPlannedIndex{
				collection: plan.physicalName, name: definition.name, description: plan.description, definition: definition,
			}
			if plan.kind == mongoSystemVersionIndexes {
				planned.resourceID, planned.version = plan.collectionID, true
			}
			flattened = append(flattened, planned)
		}
	}
	sort.Slice(flattened, func(left, right int) bool {
		if flattened[left].collection != flattened[right].collection {
			return flattened[left].collection < flattened[right].collection
		}
		return flattened[left].name < flattened[right].name
	})
	return flattened
}

func mongoDBArtifactPhases(fromDigest string, indexes []mongoDBPlannedIndex) ([]ridumigration.Phase, error) {
	physical := ridumigration.PhysicalDigestSeed(fromDigest)
	phases := make([]ridumigration.Phase, 0, len(indexes)+1)
	stepNumber := 0
	appendPhase := func(kind ridumigration.StepKind, name string, payload any) error {
		encoded, err := ridumigration.MarshalStepPayload(payload)
		if err != nil {
			return err
		}
		stepNumber++
		step := ridumigration.Step{
			ID: fmt.Sprintf("step-%04d", stepNumber), Kind: kind, ExecutorVersion: 1, Name: name, Payload: encoded,
		}
		after, err := ridumigration.PhasePhysicalDigest(physical, ridumigration.PhaseNoTransaction, []ridumigration.Step{step})
		if err != nil {
			return err
		}
		phases = append(phases, ridumigration.Phase{
			ID: fmt.Sprintf("phase-%03d", len(phases)+1), Mode: ridumigration.PhaseNoTransaction,
			PhysicalContractVersion: ridumigration.PhysicalContractVersion,
			BeforePhysicalDigest:    physical, AfterPhysicalDigest: after, Steps: []ridumigration.Step{step},
		})
		physical = after
		return nil
	}
	for _, planned := range indexes {
		if err := appendPhase(
			ridumigration.StepMongoDBCreateIndex,
			fmt.Sprintf("create MongoDB index %s for %s", planned.name, planned.description),
			ridumigration.MongoDBCreateIndexPayload{Collection: planned.collection, Index: planned.name},
		); err != nil {
			return nil, err
		}
	}
	if err := appendPhase(ridumigration.StepMongoDBAssertSchema, "verify resulting MongoDB schema", ridumigration.AssertSchemaPayload{}); err != nil {
		return nil, err
	}
	return phases, nil
}

func validateMongoDBAdditiveTransition(before, after schema.Snapshot) error {
	application := after.Application
	application.Name = before.Application.Name
	application.NameTranslations = before.Application.NameTranslations
	application.AllowIDOnCreate = before.Application.AllowIDOnCreate
	application.Admin = before.Application.Admin
	application.AdminLocalization = before.Application.AdminLocalization
	application.Endpoints = before.Application.Endpoints
	if !reflect.DeepEqual(before.Application, application) {
		return fmt.Errorf("MongoDB artifact planner supports only additive transitions; application physical settings changed")
	}
	if err := validateMongoDBAdditiveResources("collection", before.Collections, after.Collections); err != nil {
		return err
	}
	return validateMongoDBAdditiveResources("global", before.Globals, after.Globals)
}

func validateMongoDBAdditiveResources(kind string, before, after []schema.Collection) error {
	afterByID := make(map[schema.StableID]schema.Collection, len(after))
	for _, resource := range after {
		afterByID[resource.ID] = resource
	}
	for _, previous := range before {
		current, exists := afterByID[previous.ID]
		if !exists {
			return fmt.Errorf("MongoDB artifact planner supports only additive transitions; %s %q was removed", kind, previous.ID)
		}
		comparison := current
		comparison.Labels = previous.Labels
		comparison.Admin = previous.Admin
		comparison.Endpoints = previous.Endpoints
		comparison.Fields = previous.Fields
		comparison.Indexes = previous.Indexes
		if !reflect.DeepEqual(previous, comparison) {
			return fmt.Errorf("MongoDB artifact planner supports only additive transitions; %s %q changed outside presentation, fields, or indexes", kind, previous.ID)
		}
		if !mongoDBIndexesContain(current.Indexes, previous.Indexes) {
			return fmt.Errorf("MongoDB artifact planner supports only additive transitions; %s %q changed or removed an existing index", kind, previous.ID)
		}
		if err := validateMongoDBAdditiveFields(fmt.Sprintf("%s %q", kind, previous.ID), previous.Fields, current.Fields); err != nil {
			return err
		}
	}
	return nil
}

func mongoDBIndexesContain(current, previous []schema.CollectionIndex) bool {
	for _, wanted := range previous {
		found := false
		for _, candidate := range current {
			if reflect.DeepEqual(wanted, candidate) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func validateMongoDBAdditiveFields(location string, before, after []schema.Field) error {
	afterByID := make(map[schema.StableID]schema.Field, len(after))
	for _, field := range after {
		afterByID[field.ID] = field
	}
	for _, previous := range before {
		current, exists := afterByID[previous.ID]
		if !exists {
			return fmt.Errorf("MongoDB artifact planner supports only additive transitions; field %q in %s was removed", previous.ID, location)
		}
		comparison := current
		comparison.Admin = previous.Admin
		if !previous.Index && comparison.Index {
			comparison.Index = false
		}
		if !previous.Unique && comparison.Unique {
			comparison.Unique = false
		}
		if previous.Nested != nil && comparison.Nested != nil {
			nested := *comparison.Nested
			nested.Fields = previous.Nested.ResolvedFields()
			comparison.Nested = &nested
		}
		if previous.Blocks != nil && comparison.Blocks != nil {
			blocks := *comparison.Blocks
			blocks.Types = previous.Blocks.ResolvedTypes()
			comparison.Blocks = &blocks
		}
		var embeddedErr error
		comparison, embeddedErr = embedded.CompareEvolution(previous, comparison, validateMongoDBAdditiveBlockTypes)
		if embeddedErr != nil {
			return embeddedErr
		}

		if previous.Type != comparison.Type && (primitivefield.IsList(previous) || primitivefield.IsList(comparison)) {
			return fmt.Errorf("field %q changes value shape from %q to %q; add a new field and migrate existing values explicitly, or register a supported compiled data transform; automatic list conversion is not available", previous.Path.String(), previous.Type, comparison.Type)
		}
		if !reflect.DeepEqual(previous, comparison) {
			return fmt.Errorf("MongoDB artifact planner supports only additive transitions; field %q in %s changed", previous.ID, location)
		}
		if previous.Nested != nil {
			if err := validateMongoDBAdditiveFields(fmt.Sprintf("field %q in %s", previous.ID, location), previous.Nested.ResolvedFields(), current.Nested.ResolvedFields()); err != nil {
				return err
			}
		}
		if previous.Blocks != nil {
			if err := validateMongoDBAdditiveBlockTypes(fmt.Sprintf("field %q in %s", previous.ID, location), previous.Blocks.ResolvedTypes(), current.Blocks.ResolvedTypes()); err != nil {
				return err
			}
		}
		delete(afterByID, previous.ID)
	}
	for _, added := range after {
		if _, remains := afterByID[added.ID]; remains && added.Required && added.Category != schema.FieldCategoryPresentation {
			return fmt.Errorf("MongoDB additive migration cannot add required field %q to existing %s without rewriting existing documents", added.ID, location)
		}
	}
	return nil
}

func validateMongoDBAdditiveBlockTypes(location string, before, after []schema.BlockType) error {
	afterByKey := make(map[string]schema.BlockType, len(after))
	for _, block := range after {
		afterByKey[block.Slug] = block
	}
	for _, previous := range before {
		current, exists := afterByKey[previous.Slug]
		if !exists {
			return fmt.Errorf("MongoDB artifact planner supports only additive transitions; block type %q in %s was removed", previous.Slug, location)
		}
		comparison := current
		comparison.Fields = previous.ResolvedFields()
		// Block summaries are presentation metadata, not a stored-data transition.
		comparison.Admin = previous.Admin
		comparison.TypeName = previous.TypeName
		if !reflect.DeepEqual(previous, comparison) {
			return fmt.Errorf("MongoDB artifact planner supports only additive transitions; block type %q in %s changed", previous.Slug, location)
		}
		if err := validateMongoDBAdditiveFields(fmt.Sprintf("block type %q in %s", previous.Slug, location), previous.ResolvedFields(), current.ResolvedFields()); err != nil {
			return err
		}
	}
	return nil
}
