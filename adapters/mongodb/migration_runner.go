package mongodb

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/riducms/ridu/internal/migrationartifact"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// ApplyArtifacts applies the complete immutable MongoDB migration history.
func (backend *Store) ApplyArtifacts(ctx context.Context, directory string, transforms ...ridumigration.DataTransform) error {
	return backend.ApplyArtifactsWithOptions(ctx, directory, RunnerOptions{}, transforms...)
}

// ApplyArtifactsWithOptions applies planner-validated physical and semantic
// MongoDB steps, including registered transforms, and durably records each
// resumable boundary.
func (backend *Store) ApplyArtifactsWithOptions(ctx context.Context, directory string, options RunnerOptions, transforms ...ridumigration.DataTransform) error {
	if ctx == nil {
		return fmt.Errorf("MongoDB migration context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	normalized, err := normalizeMongoMigrationRunnerOptions(options)
	if err != nil {
		return err
	}
	files, err := migrationartifact.ReadAll(directory)
	if err != nil {
		return err
	}
	replay, err := prepareMongoDBArtifactReplay(ctx, files)
	if err != nil {
		return err
	}
	registry, err := newMongoDBDataTransformRegistry(transforms)
	if err != nil {
		return err
	}
	if err := requireMongoDBDataTransformRegistry(files, registry); err != nil {
		return err
	}
	return backend.applyPreparedMongoDBArtifactReplay(ctx, files, replay, normalized, registry, options.AllowMaintenance)
}

func (backend *Store) applyPreparedMongoDBArtifactReplay(
	ctx context.Context,
	files []migrationartifact.File,
	replay []mongoDBArtifactReplayPlan,
	normalized normalizedMongoMigrationRunnerOptions,
	registry mongoDBDataTransformRegistry,
	allowMaintenance bool,
) error {
	if err := backend.prepareIndexOperation(ctx); err != nil {
		return err
	}
	operationContext := ctx
	cancelOperation := func() {}
	if normalized.operationTimeout > 0 {
		operationContext, cancelOperation = context.WithTimeout(ctx, normalized.operationTimeout)
	}
	defer cancelOperation()

	if err := backend.lockMongoMigrationLifecycle(operationContext); err != nil {
		return err
	}
	defer backend.indexLifecycleMu.Unlock()
	backend.clearVerifiedIndexes()
	retainVerifiedIndexes := false
	defer func() {
		if !retainVerifiedIndexes {
			backend.clearVerifiedIndexes()
		}
	}()
	lease, err := backend.acquireMongoMigrationLease(operationContext, normalized.leaseWait, normalized.leaseDuration)
	if err != nil {
		return err
	}
	defer lease.release()
	runContext, cancelRun := context.WithCancel(operationContext)
	heartbeat := make(chan error, 1)
	go lease.heartbeat(runContext, cancelRun, heartbeat)

	applyErr := backend.applyMongoMigrationReplay(runContext, lease, files, replay, registry, allowMaintenance)
	cancelRun()
	heartbeatErr := <-heartbeat
	if applyErr != nil || heartbeatErr != nil {
		return errors.Join(applyErr, heartbeatErr)
	}
	retainVerifiedIndexes = true
	return nil
}

func (backend *Store) lockMongoMigrationLifecycle(ctx context.Context) error {
	for {
		if backend.indexLifecycleMu.TryLock() {
			return nil
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func mongoDBReplayRequiresMaintenance(replay []mongoDBArtifactReplayPlan) bool {
	for _, plan := range replay {
		for _, step := range plan.steps {
			switch step.kind {
			case ridumigration.StepMongoDBCreateIndex, ridumigration.StepMongoDBAssertSchema:
			default:
				return true
			}
		}
	}
	return false
}

func (lease *mongoMigrationLease) heartbeat(ctx context.Context, cancel context.CancelFunc, result chan<- error) {
	interval := lease.duration / 3
	if interval < 25*time.Millisecond {
		interval = 25 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			result <- nil
			return
		case <-ticker.C:
			err := lease.heartbeatRefresh(ctx, interval)
			if err != nil {
				cancel()
				result <- err
				return
			}
		}
	}
}

func (lease *mongoMigrationLease) heartbeatRefresh(ctx context.Context, timeout time.Duration) error {
	return lease.heartbeatRefreshWith(ctx, timeout, func(refreshContext context.Context) error {
		return lease.refreshUnlocked(refreshContext, lease.backend.mongoMigrationCollection(mongoMigrationLeaseCollectionName))
	})
}

func (lease *mongoMigrationLease) heartbeatRefreshWith(ctx context.Context, timeout time.Duration, refresh func(context.Context) error) error {
	if ctx.Err() != nil {
		return nil
	}
	lease.mu.Lock()
	defer lease.mu.Unlock()
	if ctx.Err() != nil {
		return nil
	}
	refreshContext, refreshCancel := context.WithTimeout(ctx, timeout)
	err := refresh(refreshContext)
	refreshCancel()
	if err != nil && ctx.Err() != nil {
		return nil
	}
	return err
}

func (backend *Store) applyMongoMigrationReplay(
	ctx context.Context,
	lease *mongoMigrationLease,
	files []migrationartifact.File,
	replay []mongoDBArtifactReplayPlan,
	registry mongoDBDataTransformRegistry,
	allowMaintenance bool,
) error {
	state, err := backend.readMongoMigrationLedgerState(ctx)
	if err != nil {
		return err
	}
	if err := validateMongoMigrationLedgerAgainstFiles(files, state); err != nil {
		return err
	}
	if err := backend.verifyMongoMigrationPhysicalProgress(ctx, replay, state); err != nil {
		return err
	}
	if mongoDBReplayRequiresMaintenance(replay[len(state.artifacts):]) && !allowMaintenance {
		return fmt.Errorf("pending MongoDB semantic migrations require explicit maintenance admission before any migration phase runs")
	}
	for artifactIndex := len(state.artifacts); artifactIndex < len(files); artifactIndex++ {
		file, plan := files[artifactIndex], replay[artifactIndex]
		for _, phase := range plan.phases {
			if phase.mode == ridumigration.PhaseTransaction {
				if err := backend.applyMongoMigrationTransactionPhase(ctx, lease, file, plan, phase, registry); err != nil {
					return err
				}
				continue
			}
			for _, step := range phase.steps {
				completed, err := backend.startMongoMigrationStep(ctx, lease, file, step)
				if err != nil {
					return err
				}
				if completed {
					continue
				}
				switch step.kind {
				case ridumigration.StepMongoDBCreateIndex:
					if err := backend.applyMongoMigrationIndex(ctx, step.index); err != nil {
						return fmt.Errorf("apply MongoDB migration %s step %s/%s: %w", file.Name, step.phaseID, step.stepID, err)
					}
				case ridumigration.StepMongoDBDropIndex:
					if err := backend.dropMongoMigrationIndex(ctx, step.dropIndex); err != nil {
						return fmt.Errorf("apply MongoDB migration %s step %s/%s: %w", file.Name, step.phaseID, step.stepID, err)
					}
				case ridumigration.StepMongoDBRenameResource:
					if err := backend.renameMongoMigrationResource(ctx, plan, step); err != nil {
						return fmt.Errorf("apply MongoDB migration %s step %s/%s: %w", file.Name, step.phaseID, step.stepID, err)
					}
				case ridumigration.StepMongoDBDropResources:
					if err := backend.dropMongoMigrationResources(ctx, plan, step.resourceIDs); err != nil {
						return fmt.Errorf("apply MongoDB migration %s step %s/%s: %w", file.Name, step.phaseID, step.stepID, err)
					}
				case ridumigration.StepMongoDBAssertSchema:
					backend.clearVerifiedIndexes()
					if err := backend.verifyIndexPlansWithoutAuthorization(ctx, plan.physical.collections, plan.physical.system); err != nil {
						return fmt.Errorf("apply MongoDB migration %s step %s/%s: %w", file.Name, step.phaseID, step.stepID, err)
					}
				default:
					return fmt.Errorf("apply MongoDB migration %s: unsupported step kind %q", file.Name, step.kind)
				}
				if err := backend.completeMongoMigrationStep(ctx, lease, file, step); err != nil {
					return err
				}
			}
		}
		if err := backend.completeMongoMigrationArtifact(ctx, lease, artifactIndex, file); err != nil {
			return err
		}
	}
	backend.clearVerifiedIndexes()
	last := replay[len(replay)-1].physical
	return backend.verifyIndexPlans(ctx, last.collections, last.system)
}

func (backend *Store) applyMongoMigrationIndex(ctx context.Context, planned mongoDBPlannedIndex) error {
	actual, err := backend.readNamedCollectionIndexes(ctx, planned.collection, planned.description)
	if err != nil {
		return err
	}
	if err := compareMongoNamedIndexSets(planned.description, []mongoIndexDefinition{planned.definition}, filterMongoActualIndex(actual, planned.name), true); err != nil {
		return err
	}
	if _, exists := actual[planned.name]; exists {
		return nil
	}
	return backend.createMongoIndex(ctx, planned.collection, planned.description, planned.definition)
}

func filterMongoActualIndex(actual map[string]mongoActualIndex, name string) map[string]mongoActualIndex {
	filtered := make(map[string]mongoActualIndex, 2)
	if native, exists := actual["_id_"]; exists {
		filtered["_id_"] = native
	}
	if found, exists := actual[name]; exists {
		filtered[name] = found
	}
	return filtered
}

func mongoMigrationStepMatches(row mongoMigrationStepLedgerRow, file migrationartifact.File, step mongoDBArtifactReplayStep) bool {
	return row.ID == mongoMigrationStepLedgerID(file.Name, step.phaseID, step.stepID) &&
		row.ArtifactName == file.Name && row.ArtifactDigest == file.Digest && row.PhaseID == step.phaseID &&
		row.StepID == step.stepID && row.PhaseMode == step.mode && row.StepKind == step.kind
}

func (backend *Store) startMongoMigrationStep(
	ctx context.Context,
	lease *mongoMigrationLease,
	file migrationartifact.File,
	step mongoDBArtifactReplayStep,
) (bool, error) {
	completed := false
	err := lease.transaction(ctx, func(sessionContext context.Context) error {
		collection := backend.mongoMigrationCollection(mongoMigrationStepCollectionName)
		id := mongoMigrationStepLedgerID(file.Name, step.phaseID, step.stepID)
		raw, findErr := collection.FindOne(sessionContext, bson.D{{Key: "_id", Value: id}}).Raw()
		attempts := 1
		if findErr == nil {
			row, decodeErr := decodeMongoMigrationStepLedger(raw)
			if decodeErr != nil {
				return decodeErr
			}
			if !mongoMigrationStepMatches(row, file, step) {
				return fmt.Errorf("MongoDB migration step ledger identity differs for %s/%s/%s", file.Name, step.phaseID, step.stepID)
			}
			if row.State == mongoMigrationStepComplete {
				completed = true
				return nil
			}
			attempts = row.Attempts + 1
		} else if !errors.Is(findErr, mongo.ErrNoDocuments) {
			return fmt.Errorf("read MongoDB migration step: %w", translateMongoError(sessionContext, findErr))
		}
		row := mongoMigrationStepLedgerRow{
			ID: id, ArtifactName: file.Name, ArtifactDigest: file.Digest,
			PhaseID: step.phaseID, StepID: step.stepID, PhaseMode: step.mode, StepKind: step.kind,
			State: mongoMigrationStepRunning, Attempts: attempts, Owner: lease.owner, Fence: lease.fence,
			UpdatedAt: backend.now().UTC(),
		}
		document, err := encodeMongoMigrationStepLedger(row)
		if err != nil {
			return err
		}
		if findErr == nil {
			_, err = collection.ReplaceOne(sessionContext, bson.D{{Key: "_id", Value: id}}, document)
		} else {
			_, err = collection.InsertOne(sessionContext, document)
		}
		if err != nil {
			return fmt.Errorf("record running MongoDB migration step: %w", translateMongoError(sessionContext, err))
		}
		return nil
	})
	return completed, err
}

func (backend *Store) completeMongoMigrationStep(
	ctx context.Context,
	lease *mongoMigrationLease,
	file migrationartifact.File,
	step mongoDBArtifactReplayStep,
) error {
	return lease.transaction(ctx, func(sessionContext context.Context) error {
		collection := backend.mongoMigrationCollection(mongoMigrationStepCollectionName)
		id := mongoMigrationStepLedgerID(file.Name, step.phaseID, step.stepID)
		raw, err := collection.FindOne(sessionContext, bson.D{{Key: "_id", Value: id}}).Raw()
		if err != nil {
			return fmt.Errorf("read running MongoDB migration step: %w", translateMongoError(sessionContext, err))
		}
		row, err := decodeMongoMigrationStepLedger(raw)
		if err != nil {
			return err
		}
		if !mongoMigrationStepMatches(row, file, step) || row.State != mongoMigrationStepRunning || row.Owner != lease.owner || row.Fence != lease.fence {
			return fmt.Errorf("complete MongoDB migration step %s/%s/%s: stale owner or changed identity", file.Name, step.phaseID, step.stepID)
		}
		now := backend.now().UTC()
		row.State, row.UpdatedAt, row.CompletedAt = mongoMigrationStepComplete, now, &now
		document, err := encodeMongoMigrationStepLedger(row)
		if err != nil {
			return err
		}
		result, err := collection.ReplaceOne(sessionContext, bson.D{
			{Key: "_id", Value: id}, {Key: "state", Value: mongoMigrationStepRunning},
			{Key: "owner", Value: lease.owner}, {Key: "fence", Value: lease.fence},
		}, document)
		if err != nil {
			return fmt.Errorf("complete MongoDB migration step: %w", translateMongoError(sessionContext, err))
		}
		if result.MatchedCount != 1 {
			return errMongoMigrationLeaseLost
		}
		return nil
	})
}

func (backend *Store) completeMongoMigrationArtifact(
	ctx context.Context,
	lease *mongoMigrationLease,
	position int,
	file migrationartifact.File,
) error {
	return lease.transaction(ctx, func(sessionContext context.Context) error {
		for _, phase := range file.Artifact.Phases {
			for _, step := range phase.Steps {
				id := mongoMigrationStepLedgerID(file.Name, phase.ID, step.ID)
				raw, err := backend.mongoMigrationCollection(mongoMigrationStepCollectionName).FindOne(sessionContext, bson.D{{Key: "_id", Value: id}}).Raw()
				if err != nil {
					return fmt.Errorf("confirm MongoDB migration step %s/%s/%s: %w", file.Name, phase.ID, step.ID, translateMongoError(sessionContext, err))
				}
				row, err := decodeMongoMigrationStepLedger(raw)
				if err != nil || row.ID != id || row.ArtifactName != file.Name || row.ArtifactDigest != file.Digest ||
					row.PhaseID != phase.ID || row.StepID != step.ID || row.PhaseMode != phase.Mode || row.StepKind != step.Kind ||
					row.State != mongoMigrationStepComplete {
					if err != nil {
						return err
					}
					return fmt.Errorf("MongoDB migration %s cannot complete before every exact step completes", file.Name)
				}
			}
		}
		row := mongoMigrationArtifactLedgerRow{
			Position: position + 1, Name: file.Name, Digest: file.Digest,
			PreviousArtifactDigest: file.Artifact.PreviousArtifactDigest,
			FromDigest:             file.Artifact.FromDigest, ToDigest: file.Artifact.ToDigest,
			PlannerName: file.Artifact.Planner.Name, PlannerVersion: file.Artifact.Planner.Version,
			StepCount: countMongoMigrationArtifactSteps(file.Artifact), AppliedAt: backend.now().UTC(),
		}
		document, err := encodeMongoMigrationArtifactLedger(row)
		if err != nil {
			return err
		}
		_, err = backend.mongoMigrationCollection(mongoMigrationArtifactCollectionName).InsertOne(sessionContext, document)
		if err != nil {
			return fmt.Errorf("complete MongoDB migration %s: %w", file.Name, translateMongoError(sessionContext, err))
		}
		return nil
	})
}

// ArtifactStatus validates immutable history and reports durable progress.
func (backend *Store) ArtifactStatus(ctx context.Context, directory string) ([]MigrationStatus, error) {
	if ctx == nil {
		return nil, fmt.Errorf("MongoDB migration context is required")
	}
	files, err := migrationartifact.ReadAll(directory)
	if err != nil {
		return nil, err
	}
	replay, err := prepareMongoDBArtifactReplay(ctx, files)
	if err != nil {
		return nil, err
	}
	return backend.artifactStatusForReplay(ctx, files, replay)
}

// ArtifactPlan returns the same strict state with phase and step detail for
// deterministic CLI rendering.
func (backend *Store) ArtifactPlan(ctx context.Context, directory string) ([]MigrationStatus, error) {
	return backend.ArtifactStatus(ctx, directory)
}

// InspectArtifacts binds status and plan inspection to the executable manifest,
// validates and precompiles that exact in-memory history before connecting, and
// uses the retained files for every database-backed check.
func InspectArtifacts(ctx context.Context, config Config, directory string, executableManifest schema.Manifest) ([]MigrationStatus, error) {
	if ctx == nil {
		return nil, fmt.Errorf("MongoDB migration context is required")
	}
	files, err := migrationartifact.RequireCurrentHistory(directory, executableManifest)
	if err != nil {
		return nil, err
	}
	replay, err := prepareMongoDBArtifactReplay(ctx, files)
	if err != nil {
		return nil, err
	}
	backend, err := OpenWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("open MongoDB: %w", err)
	}
	defer backend.Close()
	return backend.artifactStatusForReplay(ctx, files, replay)
}

func (backend *Store) artifactStatusForReplay(ctx context.Context, files []migrationartifact.File, replay []mongoDBArtifactReplayPlan) ([]MigrationStatus, error) {
	if err := backend.prepareIndexOperation(ctx); err != nil {
		return nil, err
	}
	if err := backend.verifyMongoMigrationLedgerPhysicalContract(ctx); err != nil {
		return nil, err
	}
	state, err := backend.readMongoMigrationLedgerState(ctx)
	if err != nil {
		return nil, err
	}
	if err := validateMongoMigrationLedgerAgainstFiles(files, state); err != nil {
		return nil, err
	}
	if err := backend.verifyMongoMigrationPhysicalProgress(ctx, replay, state); err != nil {
		return nil, err
	}
	return mongoMigrationStatuses(files, state), nil
}

func (backend *Store) verifyMongoMigrationPhysicalProgress(ctx context.Context, replay []mongoDBArtifactReplayPlan, state mongoMigrationLedgerState) error {
	if len(state.artifacts) == len(replay) {
		last := replay[len(replay)-1].physical
		return backend.verifyIndexPlansWithoutAuthorization(ctx, last.collections, last.system)
	}
	current := len(state.artifacts)
	hasCurrentSteps := false
	for _, row := range state.steps {
		if row.ArtifactName == replay[current].fileName {
			hasCurrentSteps = true
			break
		}
	}
	if !hasCurrentSteps {
		if current == 0 {
			return backend.requireMongoMigrationNamespacesAbsent(ctx, replay[current].physical)
		}
		previous := replay[current-1].physical
		return backend.verifyIndexPlansWithoutAuthorization(ctx, previous.collections, previous.system)
	}
	if replay[current].semantic {
		// Every v2 physical executor is exact and idempotent. A crash can occur
		// after a no-transaction catalog mutation but before its running ledger
		// row becomes complete, so neither the full source nor target catalog is
		// necessarily true here. The executor revalidates its one closed identity
		// on resume and the artifact's final assertion proves the complete target.
		return nil
	}
	requiredIndexes := mongoMigrationRequiredIndexes(replay, current, state)
	return backend.verifyMongoMigrationTargetSubset(ctx, replay[current].physical, requiredIndexes)
}

func mongoMigrationRequiredIndexes(replay []mongoDBArtifactReplayPlan, current int, state mongoMigrationLedgerState) []mongoDBPlannedIndex {
	requiredIndexes := mongoDBFlattenedIndexPlans(currentPhysicalPlan(replay, current))
	for _, step := range replay[current].steps {
		if step.kind != ridumigration.StepMongoDBCreateIndex {
			continue
		}
		row := state.stepByID[mongoMigrationStepLedgerID(replay[current].fileName, step.phaseID, step.stepID)]
		if row.State == mongoMigrationStepComplete {
			requiredIndexes = append(requiredIndexes, step.index)
		}
	}
	return requiredIndexes
}

func currentPhysicalPlan(replay []mongoDBArtifactReplayPlan, current int) mongoPhysicalIndexPlanSet {
	if current == 0 {
		return mongoPhysicalIndexPlanSet{}
	}
	return replay[current-1].physical
}

func (backend *Store) requireMongoMigrationNamespacesAbsent(ctx context.Context, plan mongoPhysicalIndexPlanSet) error {
	for _, planned := range mongoDBFlattenedIndexPlans(plan) {
		names, err := backend.database.ListCollectionNames(ctx, bson.D{{Key: "name", Value: planned.collection}})
		if err != nil {
			return fmt.Errorf("inspect initial MongoDB migration namespace: %w", translateMongoError(ctx, err))
		}
		if len(names) != 0 {
			return fmt.Errorf("initial MongoDB migration requires an empty managed namespace; %s already exists", planned.description)
		}
	}
	return nil
}

func (backend *Store) verifyMongoMigrationTargetSubset(ctx context.Context, target mongoPhysicalIndexPlanSet, required []mongoDBPlannedIndex) error {
	actualByCollection := make(map[string]map[string]mongoActualIndex)
	for _, planned := range mongoDBFlattenedIndexPlans(target) {
		if _, exists := actualByCollection[planned.collection]; exists {
			continue
		}
		actual, err := backend.readNamedCollectionIndexes(ctx, planned.collection, planned.description)
		if err != nil {
			return err
		}
		actualByCollection[planned.collection] = actual
	}
	for _, collection := range target.collections {
		actual := actualByCollection[collection.physicalName]
		if err := compareMongoIndexSets(collection.collection, collection.definitions, actual, true); err != nil {
			return err
		}
	}
	for _, collection := range target.system {
		actual := actualByCollection[collection.physicalName]
		if err := compareMongoNamedIndexSets(collection.description, collection.definitions, actual, true); err != nil {
			return err
		}
	}
	for _, planned := range required {
		if _, exists := actualByCollection[planned.collection][planned.name]; !exists {
			return fmt.Errorf("MongoDB migration source drift for %s: required index %q is missing", planned.description, planned.name)
		}
	}
	return nil
}

// VerifyArtifacts replays immutable history in an isolated shadow database.
func VerifyArtifacts(ctx context.Context, config Config, directory string, transforms ...ridumigration.DataTransform) error {
	return VerifyArtifactsWithOptions(ctx, config, directory, RunnerOptions{}, transforms...)
}

// VerifyArtifactsWithOptions exercises the production runner, ledger, status,
// and readiness checks against an isolated database which is always dropped.
func VerifyArtifactsWithOptions(ctx context.Context, config Config, directory string, options RunnerOptions, transforms ...ridumigration.DataTransform) error {
	if ctx == nil {
		return fmt.Errorf("MongoDB migration verification context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	normalized, err := normalizeMongoMigrationRunnerOptions(options)
	if err != nil {
		return err
	}
	files, err := migrationartifact.ReadAll(directory)
	if err != nil {
		return err
	}
	replay, err := prepareMongoDBArtifactReplay(ctx, files)
	if err != nil {
		return err
	}
	registry, err := newMongoDBDataTransformRegistry(transforms)
	if err != nil {
		return err
	}
	if err := requireMongoDBDataTransformRegistry(files, registry); err != nil {
		return err
	}
	if mongoDBReplayRequiresMaintenance(replay) && !options.AllowMaintenance {
		return fmt.Errorf("MongoDB semantic migrations require explicit maintenance admission before verification")
	}
	return verifyPreparedMongoDBArtifactReplay(ctx, config, files, replay, normalized, registry, options.AllowMaintenance)
}

func verifyPreparedMongoDBArtifactReplay(
	ctx context.Context,
	config Config,
	files []migrationartifact.File,
	replay []mongoDBArtifactReplayPlan,
	normalized normalizedMongoMigrationRunnerOptions,
	registry mongoDBDataTransformRegistry,
	allowMaintenance bool,
) error {
	return withMongoDBShadowDatabase(ctx, config, func(shadow *Store) error {
		if err := shadow.applyPreparedMongoDBArtifactReplay(ctx, files, replay, normalized, registry, allowMaintenance); err != nil {
			return err
		}
		statuses, err := shadow.artifactStatusForReplay(ctx, files, replay)
		if err != nil {
			return err
		}
		for _, status := range statuses {
			if !status.Applied {
				return fmt.Errorf("shadow MongoDB migration %s is incomplete", status.Name)
			}
		}
		manifest, err := files[len(files)-1].Artifact.AfterManifest()
		if err != nil {
			return err
		}
		return shadow.Ready(ctx, manifest)
	})
}

// Ready preserves manifest-only readiness compatibility for callers that do
// not yet provide an executable migration-history digest.
func (backend *Store) Ready(ctx context.Context, manifest schema.Manifest) error {
	return backend.ready(ctx, manifest, "")
}

// ReadyWithMigrationHistory proves the executable manifest and exact ordered
// artifact history are the complete ledger head, then non-mutatingly verifies
// the physical index contract.
func (backend *Store) ReadyWithMigrationHistory(ctx context.Context, manifest schema.Manifest, expectedHistoryDigest string) error {
	if ctx == nil {
		return fmt.Errorf("MongoDB readiness context is required")
	}
	if !validMongoMigrationDigest(expectedHistoryDigest, false) {
		return fmt.Errorf("MongoDB readiness requires a valid executable migration history digest")
	}
	return backend.ready(ctx, manifest, expectedHistoryDigest)
}

func (backend *Store) ready(ctx context.Context, manifest schema.Manifest, expectedHistoryDigest string) error {
	if ctx == nil {
		return fmt.Errorf("MongoDB readiness context is required")
	}
	if err := backend.Ping(ctx); err != nil {
		return err
	}
	digest, err := ridumigration.DigestManifest(manifest)
	if err != nil {
		return err
	}
	state, err := backend.readMongoMigrationLedgerState(ctx)
	if err != nil {
		return err
	}
	if err := validateMongoMigrationReadyStateWithHistory(state, digest, expectedHistoryDigest); err != nil {
		return err
	}
	if err := backend.VerifyIndexes(ctx, manifest); err != nil {
		return err
	}
	retainVerifiedIndexes := false
	defer func() {
		if !retainVerifiedIndexes {
			backend.clearVerifiedIndexes()
		}
	}()
	confirmed, err := backend.readMongoMigrationLedgerState(ctx)
	if err != nil {
		return err
	}
	if err := validateMongoMigrationReadyStateWithHistory(confirmed, digest, expectedHistoryDigest); err != nil {
		return err
	}
	if !mongoMigrationReadyStatesEqual(state, confirmed) {
		return fmt.Errorf("MongoDB migration ledger changed during readiness verification")
	}
	retainVerifiedIndexes = true
	return nil
}

func mongoMigrationReadyStatesEqual(left, right mongoMigrationLedgerState) bool {
	return reflect.DeepEqual(left.artifacts, right.artifacts) && reflect.DeepEqual(left.steps, right.steps)
}

func validateMongoMigrationReadyState(state mongoMigrationLedgerState, manifestDigest string) error {
	if len(state.artifacts) == 0 {
		return fmt.Errorf("MongoDB migration ledger is empty")
	}
	byDigest := make(map[string]mongoMigrationArtifactLedgerRow, len(state.artifacts))
	for _, artifact := range state.artifacts {
		byDigest[artifact.Digest] = artifact
	}
	counts := make(map[string]int, len(state.artifacts))
	for _, step := range state.steps {
		artifact, exists := byDigest[step.ArtifactDigest]
		if !exists || step.ArtifactName != artifact.Name || step.State != mongoMigrationStepComplete {
			return fmt.Errorf("MongoDB migration ledger has incomplete or unbound step work")
		}
		counts[step.ArtifactDigest]++
	}
	for _, artifact := range state.artifacts {
		if counts[artifact.Digest] != artifact.StepCount {
			return fmt.Errorf("MongoDB migration %s has incomplete durable step state", artifact.Name)
		}
	}
	head := state.artifacts[len(state.artifacts)-1]
	if head.ToDigest != manifestDigest {
		return fmt.Errorf("MongoDB migration ledger head does not match the executable manifest")
	}
	return nil
}

func validateMongoMigrationReadyStateWithHistory(state mongoMigrationLedgerState, manifestDigest, expectedHistoryDigest string) error {
	if err := validateMongoMigrationReadyState(state, manifestDigest); err != nil {
		return err
	}
	if expectedHistoryDigest == "" {
		return nil
	}
	identities := make([]ridumigration.ArtifactIdentity, len(state.artifacts))
	for index, artifact := range state.artifacts {
		identities[index] = ridumigration.ArtifactIdentity{Name: artifact.Name, Digest: artifact.Digest}
	}
	actualHistoryDigest, err := ridumigration.DigestArtifactHistory(identities)
	if err != nil {
		return fmt.Errorf("digest MongoDB migration ledger history: %w", err)
	}
	if actualHistoryDigest != expectedHistoryDigest {
		return fmt.Errorf("MongoDB migration ledger history does not match the executable migration history")
	}
	return nil
}
