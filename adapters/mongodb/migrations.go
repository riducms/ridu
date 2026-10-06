package mongodb

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/riducms/ridu/internal/blockgraph"
	"github.com/riducms/ridu/internal/embedded"
	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/internal/primitivefield"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
)

const (
	mongoDBPlannerName = "mongodb"
	// mongoDBPlannerVersion is recorded in every artifact. Validation replans
	// each committed artifact with this planner, so its emitted artifacts and
	// digests must remain byte-for-byte reproducible. Change the version
	// whenever the planner can emit a different artifact for the same input.
	mongoDBPlannerVersion = "5.0.0"
)

// mongoDBUnsupportedPlannerVersion explains that an artifact or ledger row was
// recorded by another planner contract, which this release cannot replay.
func mongoDBUnsupportedPlannerVersion(subject, version string) error {
	return fmt.Errorf("%s uses unsupported planner version %q; this Ridu release supports only %s %q, so create a new migration history with ridu migrate create and apply it to a new database", subject, version, mongoDBPlannerName, mongoDBPlannerVersion)
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

// CreateArtifact plans and atomically publishes one immutable
// MongoDB migration artifact with explicit semantic intent. Planning is offline:
// it reads only committed artifacts and the executable manifest; database URLs,
// secrets, and live MongoDB state are outside this boundary.
func CreateArtifact(ctx context.Context, directory, name string, after schema.Manifest, now time.Time, options ArtifactOptions) (CreatedArtifact, error) {
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
	if len(files) != 0 {
		latest, err := files[len(files)-1].Artifact.AfterManifest()
		if err != nil {
			return CreatedArtifact{}, err
		}
		before = &latest
	}
	if err := validateMongoDBDataTransformIdentities(files, options.DataTransforms); err != nil {
		return CreatedArtifact{}, err
	}
	artifact, err := buildMongoDBArtifact(ctx, name, before, after, options)
	if err != nil {
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
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := validateMongoDBArtifactPlan(ctx, file.Artifact, file.Name); err != nil {
			return err
		}
	}
	return nil
}

// validateMongoDBArtifactPlan binds one artifact to the complete private plan
// the current planner generates for it. The runner calls this boundary before
// opening MongoDB or inspecting its catalog.
func validateMongoDBArtifactPlan(ctx context.Context, artifact ridumigration.Artifact, label string) error {
	if artifact.Planner.Name != mongoDBPlannerName {
		return fmt.Errorf("MongoDB migration %s uses planner %q instead of %q", label, artifact.Planner.Name, mongoDBPlannerName)
	}
	if err := artifact.Validate(); err != nil {
		return fmt.Errorf("validate MongoDB migration %s: %w", label, err)
	}
	if artifact.Planner.Version != mongoDBPlannerVersion {
		return mongoDBUnsupportedPlannerVersion("MongoDB migration "+label, artifact.Planner.Version)
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
	expected, err := buildMongoDBArtifact(ctx, artifact.Name, before, after, semanticOptions)
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
		return fmt.Errorf("MongoDB migration %s does not match planner %s %s", label, mongoDBPlannerName, artifact.Planner.Version)
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
	published   bool
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
			if plan.kind == mongoSystemVersionIndexes || plan.kind == mongoSystemPublishedIndexes {
				planned.resourceID = plan.collectionID
				planned.version = plan.kind == mongoSystemVersionIndexes
				planned.published = plan.kind == mongoSystemPublishedIndexes
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

func validateMongoDBAdditiveTransition(before, after schema.Snapshot) error {
	// Presentation never shapes stored documents, so compare storage schemas.
	before, after = mongoDBStorageSchema(before), mongoDBStorageSchema(after)
	application := after.Application
	application.AllowIDOnCreate = before.Application.AllowIDOnCreate
	application.Admin = before.Application.Admin
	if !reflect.DeepEqual(before.Application, application) {
		return fmt.Errorf("MongoDB artifact planner supports only additive transitions; application physical settings changed")
	}
	if err := validateMongoDBAdditiveResources("collection", before.Collections, after.Collections); err != nil {
		return err
	}
	if err := validateMongoDBAdditiveResources("global", before.Globals, after.Globals); err != nil {
		return err
	}
	return validateMongoDBAdditiveDefinitions(before, after)
}

// validateMongoDBAdditiveDefinitions validates each block definition placed
// before and after the transition once: ordinarily placed views with the
// ordinary rules, and views inside embedded plugin payloads with the embedded
// rules. Containers already refused deselecting a placed definition.
func validateMongoDBAdditiveDefinitions(before, after schema.Snapshot) error {
	previous, current := blockgraph.New(before), blockgraph.New(after)
	validate := func(keys map[blockgraph.Key]bool, rules mongoDBAdditiveRules) error {
		for _, key := range blockgraph.SortedKeys(keys) {
			beforeView, _ := previous.View(key)
			afterView, placed := current.View(key)
			if !placed {
				continue
			}
			if err := rules.fields(fmt.Sprintf("block type %q", key.Slug), beforeView.ResolvedFields(), afterView.ResolvedFields()); err != nil {
				return err
			}
		}
		return nil
	}
	if err := validate(blockgraph.Shared(previous, current, nil, false), mongoDBAdditiveRules{}); err != nil {
		return err
	}
	return validate(blockgraph.EmbeddedPlacements(previous), mongoDBAdditiveRules{embedded: true})
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
		comparison.Fields = previous.Fields
		comparison.Indexes = previous.Indexes
		if !reflect.DeepEqual(previous, comparison) {
			return fmt.Errorf("MongoDB artifact planner supports only additive transitions; %s %q changed outside its fields or indexes", kind, previous.ID)
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

// mongoDBAdditiveRules validates an additive field transition. embedded marks
// fields inside a plugin's embedded payload, which the required-value audit
// does not read: their requiredness stays fixed.
type mongoDBAdditiveRules struct {
	embedded bool
}

func validateMongoDBAdditiveFields(location string, before, after []schema.Field) error {
	return mongoDBAdditiveRules{}.fields(location, before, after)
}

func (rules mongoDBAdditiveRules) fields(location string, before, after []schema.Field) error {
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
		// Requiredness does not reshape stored documents. Relaxing it is safe,
		// and the migration audits stored values for a field made required.
		if !rules.embedded {
			comparison.Required = previous.Required
		}
		if previous.Nested != nil && comparison.Nested != nil {
			// Children are compared below; the group or array keeps its prior
			// children and compares only its row limits.
			nested := *previous.Nested
			nested.MinRows, nested.MaxRows = comparison.Nested.MinRows, comparison.Nested.MaxRows
			comparison.Nested = &nested
		}
		if previous.Blocks != nil && comparison.Blocks != nil {
			// Variants are compared below; the container keeps the prior
			// selection and compares only its row limits.
			blocks := *previous.Blocks
			blocks.MinRows, blocks.MaxRows = comparison.Blocks.MinRows, comparison.Blocks.MaxRows
			comparison.Blocks = &blocks
		}
		var embeddedErr error
		comparison, embeddedErr = embedded.CompareEvolution(previous, comparison, mongoDBAdditiveRules{embedded: true}.blockTypes)
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
			if err := rules.fields(fmt.Sprintf("field %q in %s", previous.ID, location), previous.Nested.ResolvedFields(), current.Nested.ResolvedFields()); err != nil {
				return err
			}
		}
		if previous.Blocks != nil {
			if err := rules.blockTypes(fmt.Sprintf("field %q in %s", previous.ID, location), previous.Blocks.Definitions(), current.Blocks.Definitions()); err != nil {
				return err
			}
		}
		delete(afterByID, previous.ID)
	}
	if !rules.embedded {
		// An added required field is audited like any field made required.
		return nil
	}
	for _, added := range after {
		if _, remains := afterByID[added.ID]; remains && added.Required && added.Category != schema.FieldCategoryPresentation {
			return fmt.Errorf("MongoDB additive migration cannot add required embedded field %q to existing %s without rewriting existing documents", added.ID, location)
		}
	}
	return nil
}

func (rules mongoDBAdditiveRules) blockTypes(location string, before, after []schema.BlockType) error {
	afterByKey := make(map[string]schema.BlockType, len(after))
	for _, block := range after {
		afterByKey[block.Slug] = block
	}
	for _, previous := range before {
		if _, exists := afterByKey[previous.Slug]; !exists {
			return fmt.Errorf("MongoDB artifact planner supports only additive transitions; block type %q in %s was removed", previous.Slug, location)
		}
		// A block's labels, summary and type name are presentation metadata, and
		// its slug is its identity. Its fields are validated once with the
		// definition, wherever it is placed.
	}
	return nil
}

// mongoDBStorageSchema is the private copy MongoDB's planners compare: the
// storage schema, without settings that never shape stored documents.
func mongoDBStorageSchema(snapshot schema.Snapshot) schema.Snapshot {
	return schema.NewManifest(snapshot).StorageSchema().Snapshot()
}
