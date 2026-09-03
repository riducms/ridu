package mongodb

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/riducms/ridu/internal/migrationartifact"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
	"go.mongodb.org/mongo-driver/v2/mongo/writeconcern"
)

const mongoDBShadowDatabasePrefix = "ridu_shadow_"

type mongoDBArtifactReplayStep struct {
	phaseID                       string
	stepID                        string
	mode                          ridumigration.PhaseMode
	kind                          ridumigration.StepKind
	index                         mongoDBPlannedIndex
	dropIndex                     mongoDBPlannedIndex
	resourceRename                ridumigration.MongoDBRenameResourcePayload
	resourceRenameContentRequired bool
	resourceRenameVersionRequired bool
	rename                        ridumigration.Rename
	transform                     ridumigration.DataTransformDescriptor
	resourceIDs                   []schema.StableID
}

type mongoDBArtifactReplayPhase struct {
	id    string
	mode  ridumigration.PhaseMode
	steps []mongoDBArtifactReplayStep
}

type mongoDBArtifactReplayPlan struct {
	fileName          string
	artifact          ridumigration.Artifact
	before            *schema.Manifest
	after             schema.Manifest
	physical          mongoPhysicalIndexPlanSet
	collectionMapping map[schema.StableID]schema.StableID
	semantic          bool
	phases            []mongoDBArtifactReplayPhase
	steps             []mongoDBArtifactReplayStep
}

// verifyMongoDBArtifacts retains the original private test boundary while the
// exported verifier exercises the production runner and lifecycle.
func verifyMongoDBArtifacts(ctx context.Context, config Config, directory string) error {
	return VerifyArtifacts(ctx, config, directory)
}

// prepareMongoDBArtifactReplay reconstructs every private index definition and
// full assertion plan before the connection boundary is entered.
func prepareMongoDBArtifactReplay(ctx context.Context, files []migrationartifact.File) ([]mongoDBArtifactReplayPlan, error) {
	if ctx == nil {
		return nil, fmt.Errorf("MongoDB migration verification context is required")
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("migration artifact history is empty; create and commit an initial MongoDB migration before verification")
	}
	if err := validateMongoDBArtifactHistory(ctx, files); err != nil {
		return nil, err
	}

	replay := make([]mongoDBArtifactReplayPlan, 0, len(files))
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		after, err := file.Artifact.AfterManifest()
		if err != nil {
			return nil, err
		}
		var before *schema.Manifest
		var beforePlans mongoPhysicalIndexPlanSet
		if file.Artifact.Before != nil {
			manifest, err := file.Artifact.BeforeManifest()
			if err != nil {
				return nil, err
			}
			before = &manifest
			beforePlans, err = mongoPhysicalIndexPlans(manifest)
			if err != nil {
				return nil, fmt.Errorf("reconstruct previous MongoDB migration indexes for %s: %w", file.Name, err)
			}
		}
		afterPlans, err := mongoPhysicalIndexPlans(after)
		if err != nil {
			return nil, fmt.Errorf("reconstruct MongoDB migration indexes for %s: %w", file.Name, err)
		}
		var additions []mongoDBPlannedIndex
		var drops []mongoDBPlannedIndex
		var renamePlan mongoDBSemanticRenamePlan
		var retired []schema.StableID
		semantic := file.Artifact.Planner.Version == mongoDBPlannerVersionV2
		if semantic {
			options, optionsErr := mongoDBArtifactSemanticOptions(file.Artifact)
			if optionsErr != nil {
				return nil, optionsErr
			}
			if before != nil {
				renamePlan, err = compileMongoDBRenamePlan(*before, after, options.Renames)
				if err != nil {
					return nil, err
				}
				_, retired, err = normalizeMongoDBSemanticBefore(before.Snapshot(), after.Snapshot(), renamePlan)
				if err != nil {
					return nil, err
				}
			}
			delta, deltaErr := mongoDBSemanticIndexDelta(beforePlans, afterPlans, renamePlan.collectionMapping, retired)
			if deltaErr != nil {
				return nil, fmt.Errorf("reconstruct MongoDB semantic index transition for %s: %w", file.Name, deltaErr)
			}
			additions, drops = delta.creates, delta.drops
		} else {
			additions, _, err = mongoDBAddedIndexPlans(beforePlans, afterPlans)
			if err != nil {
				return nil, fmt.Errorf("reconstruct MongoDB migration additions for %s: %w", file.Name, err)
			}
		}
		remaining := make(map[string]mongoDBPlannedIndex, len(additions))
		for _, addition := range additions {
			key := mongoDBReplayIndexKey(addition.collection, addition.name)
			if _, duplicate := remaining[key]; duplicate {
				return nil, fmt.Errorf("MongoDB migration %s reconstructs duplicate index identity", file.Name)
			}
			remaining[key] = addition
		}

		remainingDrops := make(map[string]mongoDBPlannedIndex, len(drops))
		for _, drop := range drops {
			key := mongoDBReplayDropIndexKey(drop.resourceID, drop.version, drop.name)
			if _, duplicate := remainingDrops[key]; duplicate {
				return nil, fmt.Errorf("MongoDB migration %s reconstructs duplicate dropped index identity", file.Name)
			}
			remainingDrops[key] = drop
		}

		plan := mongoDBArtifactReplayPlan{
			fileName: file.Name, artifact: file.Artifact, before: before, after: after, physical: afterPlans,
			collectionMapping: renamePlan.collectionMapping, semantic: semantic,
		}
		for _, phase := range file.Artifact.Phases {
			compiledPhase := mongoDBArtifactReplayPhase{id: phase.ID, mode: phase.Mode}
			for _, step := range phase.Steps {
				compiled := mongoDBArtifactReplayStep{phaseID: phase.ID, stepID: step.ID, mode: phase.Mode, kind: step.Kind}
				switch step.Kind {
				case ridumigration.StepMongoDBCreateIndex:
					var payload ridumigration.MongoDBCreateIndexPayload
					if err := json.Unmarshal(step.Payload, &payload); err != nil {
						return nil, fmt.Errorf("decode MongoDB migration step %s/%s in %s: %w", phase.ID, step.ID, file.Name, err)
					}
					key := mongoDBReplayIndexKey(payload.Collection, payload.Index)
					addition, exists := remaining[key]
					if !exists {
						return nil, fmt.Errorf("MongoDB migration step %s/%s in %s does not resolve to one planned addition", phase.ID, step.ID, file.Name)
					}
					compiled.index = addition
					delete(remaining, key)
				case ridumigration.StepMongoDBDropIndex:
					var payload ridumigration.MongoDBDropIndexPayload
					if err := json.Unmarshal(step.Payload, &payload); err != nil {
						return nil, fmt.Errorf("decode MongoDB migration index drop %s/%s in %s: %w", phase.ID, step.ID, file.Name, err)
					}
					key := mongoDBReplayDropIndexKey(payload.CollectionID, payload.Version, payload.Index)
					drop, exists := remainingDrops[key]
					if !exists {
						return nil, fmt.Errorf("MongoDB migration step %s/%s in %s does not resolve to one planned index drop", phase.ID, step.ID, file.Name)
					}
					compiled.dropIndex = drop
					delete(remainingDrops, key)
				case ridumigration.StepMongoDBRenameResource:
					if err := json.Unmarshal(step.Payload, &compiled.resourceRename); err != nil {
						return nil, fmt.Errorf("decode MongoDB migration resource rename %s/%s in %s: %w", phase.ID, step.ID, file.Name, err)
					}
					if renamePlan.collectionMapping[compiled.resourceRename.BeforeID] != compiled.resourceRename.AfterID || compiled.resourceRename.BeforeID == compiled.resourceRename.AfterID {
						return nil, fmt.Errorf("MongoDB migration step %s/%s in %s does not resolve to one planned resource rename", phase.ID, step.ID, file.Name)
					}
					compiled.resourceRenameContentRequired, compiled.resourceRenameVersionRequired = mongoDBRenameSourceRequirements(beforePlans, compiled.resourceRename.BeforeID)
				case ridumigration.StepRenameContent:
					var payload ridumigration.RenamePayload
					if err := json.Unmarshal(step.Payload, &payload); err != nil {
						return nil, fmt.Errorf("decode MongoDB migration content rename %s/%s in %s: %w", phase.ID, step.ID, file.Name, err)
					}
					compiled.rename = payload.Rename
				case ridumigration.StepDataTransform:
					var payload ridumigration.DataTransformPayload
					if err := json.Unmarshal(step.Payload, &payload); err != nil {
						return nil, fmt.Errorf("decode MongoDB migration transform %s/%s in %s: %w", phase.ID, step.ID, file.Name, err)
					}
					compiled.transform = payload.Transform
				case ridumigration.StepRetireResources:
					var payload ridumigration.RetireResourcesPayload
					if err := json.Unmarshal(step.Payload, &payload); err != nil {
						return nil, fmt.Errorf("decode MongoDB migration retirement %s/%s in %s: %w", phase.ID, step.ID, file.Name, err)
					}
					compiled.resourceIDs = append([]schema.StableID(nil), payload.ResourceIDs...)
				case ridumigration.StepMongoDBDropResources:
					var payload ridumigration.MongoDBDropResourcesPayload
					if err := json.Unmarshal(step.Payload, &payload); err != nil {
						return nil, fmt.Errorf("decode MongoDB migration resource drop %s/%s in %s: %w", phase.ID, step.ID, file.Name, err)
					}
					compiled.resourceIDs = append([]schema.StableID(nil), payload.ResourceIDs...)
				case ridumigration.StepMongoDBAssertSchema:
				default:
					return nil, fmt.Errorf("MongoDB migration step %s/%s in %s has unsupported kind %q", phase.ID, step.ID, file.Name, step.Kind)
				}
				plan.steps = append(plan.steps, compiled)
				compiledPhase.steps = append(compiledPhase.steps, compiled)
			}
			plan.phases = append(plan.phases, compiledPhase)
		}
		if len(remaining) != 0 || len(remainingDrops) != 0 {
			return nil, fmt.Errorf("MongoDB migration %s omits a reconstructed index transition", file.Name)
		}
		replay = append(replay, plan)
	}
	return replay, nil
}

func mongoDBRenameSourceRequirements(plans mongoPhysicalIndexPlanSet, resourceID schema.StableID) (content, versions bool) {
	for _, plan := range plans.collections {
		if plan.collection.ID == resourceID && len(plan.definitions) != 0 {
			content = true
		}
	}
	for _, plan := range plans.system {
		if plan.kind == mongoSystemVersionIndexes && plan.collectionID == resourceID && len(plan.definitions) != 0 {
			versions = true
		}
	}
	return content, versions
}

func mongoDBReplayDropIndexKey(resourceID schema.StableID, version bool, index string) string {
	return fmt.Sprintf("%s\x00%t\x00%s", resourceID, version, index)
}

func replayMongoDBArtifacts(ctx context.Context, shadow *Store, artifacts []mongoDBArtifactReplayPlan) error {
	if err := shadow.prepareIndexOperation(ctx); err != nil {
		return err
	}
	for _, artifact := range artifacts {
		for _, step := range artifact.steps {
			if err := ctx.Err(); err != nil {
				return err
			}
			switch step.kind {
			case ridumigration.StepMongoDBCreateIndex:
				if err := shadow.createMongoIndex(ctx, step.index.collection, step.index.description, step.index.definition); err != nil {
					return fmt.Errorf("replay MongoDB migration %s step %s/%s: %w", artifact.fileName, step.phaseID, step.stepID, err)
				}
			case ridumigration.StepMongoDBAssertSchema:
				shadow.clearVerifiedIndexes()
				if err := shadow.verifyIndexPlansWithoutAuthorization(ctx, artifact.physical.collections, artifact.physical.system); err != nil {
					return fmt.Errorf("replay MongoDB migration %s step %s/%s: %w", artifact.fileName, step.phaseID, step.stepID, err)
				}
			default:
				return fmt.Errorf("replay MongoDB migration %s step %s/%s: unsupported kind %q", artifact.fileName, step.phaseID, step.stepID, step.kind)
			}
		}
	}
	return nil
}

func mongoDBReplayIndexKey(collection, index string) string {
	return collection + "\x00" + index
}

func withMongoDBShadowDatabase(ctx context.Context, config Config, action func(*Store) error) (resultError error) {
	if ctx == nil {
		return fmt.Errorf("MongoDB migration verification context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if action == nil {
		return fmt.Errorf("MongoDB shadow replay action is required")
	}
	shadowName, err := newMongoDBShadowDatabaseName()
	if err != nil {
		return err
	}
	connectionConfig := config
	connectionConfig.ApplicationName = "ridu-migration-shadow"
	owner, err := OpenWithConfig(ctx, connectionConfig)
	if err != nil {
		return err
	}
	owned := false
	shadowDatabase := owner.client.Database(
		shadowName,
		options.Database().
			SetReadPreference(readpref.Primary()).
			SetWriteConcern(writeconcern.Majority()),
	)
	shadow := &Store{
		client: owner.client, database: shadowDatabase, now: time.Now,
		verifiedIndexes:        make(map[schema.StableID]mongoVerifiedIndexPlan),
		verifiedVersionIndexes: make(map[schema.StableID]bool),
	}
	defer func() {
		if owned {
			if shadow.database.Name() != shadowName || !strings.HasPrefix(shadowName, mongoDBShadowDatabasePrefix) {
				resultError = errors.Join(resultError, fmt.Errorf("refuse to drop an unowned MongoDB shadow database"))
			} else {
				cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), defaultCloseTimeout)
				dropError := shadow.database.Drop(cleanupContext)
				translatedDropError := translateMongoError(cleanupContext, dropError)
				cancel()
				if dropError != nil {
					resultError = errors.Join(resultError, fmt.Errorf("drop isolated MongoDB migration database %s: %w", shadowName, translatedDropError))
				}
			}
		}
		if closeError := owner.Close(); closeError != nil {
			resultError = errors.Join(resultError, fmt.Errorf("close MongoDB migration verifier: %w", closeError))
		}
	}()

	if shadowName == owner.database.Name() {
		return fmt.Errorf("generated MongoDB shadow database identity matches the configured database; retry verification")
	}
	collections, err := shadow.database.ListCollectionNames(ctx, bson.D{})
	if err != nil {
		return fmt.Errorf("inspect MongoDB shadow database identity: %w", translateMongoError(ctx, err))
	}
	if len(collections) != 0 {
		return fmt.Errorf("generated MongoDB shadow database identity is already in use; retry verification")
	}
	owned = true
	return action(shadow)
}

func newMongoDBShadowDatabaseName() (string, error) {
	var identifier [16]byte
	if _, err := rand.Read(identifier[:]); err != nil {
		return "", fmt.Errorf("create MongoDB shadow database identity: %w", err)
	}
	return mongoDBShadowDatabasePrefix + hex.EncodeToString(identifier[:]), nil
}
