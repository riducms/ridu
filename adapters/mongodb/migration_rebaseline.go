package mongodb

import (
	"context"
	"errors"
	"fmt"

	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/migration"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// ReplaceBaseline atomically replaces a verified development artifact/step
// ledger. Content, snapshots, namespaces, and indexes are never changed.
// Replacement artifacts from the first unrecorded semantic step onward stay
// pending for ApplyArtifacts.
func (backend *Store) ReplaceBaseline(ctx context.Context, directory string, options migration.BaselineReplacementOptions) ([]string, error) {
	files, previous, err := migrationartifact.ReplacementHistories(directory, options)
	if err != nil {
		return nil, err
	}
	replay, err := prepareMongoDBArtifactReplay(ctx, files)
	if err != nil {
		return nil, err
	}
	oldReplay, err := prepareMongoDBArtifactReplay(ctx, previous)
	if err != nil {
		return nil, err
	}
	normalized, err := normalizeMongoMigrationRunnerOptions(RunnerOptions{})
	if err != nil {
		return nil, err
	}
	var recorded []string
	changed := false
	end := 0
	err = backend.withMongoMigrationLease(ctx, normalized, func(runContext context.Context, lease *mongoMigrationLease) error {
		state, err := backend.readMongoMigrationLedgerState(runContext)
		if err != nil {
			return err
		}
		if err := validateMongoMigrationLedgerAgainstFiles(previous, state); err != nil {
			return migrationartifact.PreviousHistoryError(err)
		}
		applied := len(state.artifacts)
		end, err = migrationartifact.CheckReplacement(files, previous, applied, mongoDBBlockingStep)
		if err != nil {
			return err
		}
		oldManifest := oldReplay[applied-1].after
		after := replay[end-1].after
		// Index sync for ridu dev leaves a removed resource's namespaces and
		// shared state behind; only a migration's retirement removes them.
		if err := migrationartifact.RequireRetainedResources(oldManifest.Snapshot(), after.Snapshot()); err != nil {
			return err
		}
		for _, step := range state.steps {
			if step.State != mongoMigrationStepComplete {
				return migrationartifact.ErrReplacementPartlyApplied
			}
			found := false
			for _, file := range previous[:applied] {
				found = found || file.Name == step.ArtifactName
			}
			if !found {
				return migrationartifact.ErrReplacementPartlyApplied
			}
		}
		backend.clearVerifiedIndexes()
		physical := replay[end-1].physical
		if err := backend.verifyIndexPlansWithoutAuthorization(runContext, physical.collections, physical.system); err != nil {
			return migrationartifact.ReplacementSchemaDriftError(err)
		}
		// The schema record is written only after ridu dev scanned and verified a
		// change, or by a migration, so it vouches for stored values that
		// matching indexes cannot.
		if err := backend.requireMongoDevelopmentManifest(runContext, after); err != nil {
			return err
		}
		if migrationartifact.SameRecordedHistory(files[:end], previous, applied) {
			return nil
		}
		if !options.AllowProduction {
			oldPhysical := oldReplay[applied-1].physical
			err := backend.verifyIndexPlansWithoutAuthorization(runContext, oldPhysical.collections, oldPhysical.system)
			if err == nil {
				return migration.ErrReplacementNeedsConfirmation
			}
			// Timeouts and cancellation keep their context errors. Other server
			// and network failures reach here as untyped errors, indistinguishable
			// from index drift until the verifier reports drift with a type; the
			// replacement transaction below still fails on a lost connection.
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return fmt.Errorf("check whether the database is at its recorded migration head: %w", err)
			}
		}
		if err := runContext.Err(); err != nil {
			return err
		}
		if err := lease.transaction(runContext, func(sessionContext context.Context) error {
			confirmed, err := backend.readMongoMigrationLedgerState(sessionContext)
			if err != nil {
				return err
			}
			if !mongoMigrationReadyStatesEqual(state, confirmed) {
				return fmt.Errorf("migration history changed while replacing the baseline; retry with the original history")
			}
			if _, err := backend.mongoMigrationCollection(mongoMigrationStepCollectionName).DeleteMany(sessionContext, bson.D{}); err != nil {
				return translateMongoError(sessionContext, err)
			}
			if _, err := backend.mongoMigrationCollection(mongoMigrationArtifactCollectionName).DeleteMany(sessionContext, bson.D{}); err != nil {
				return translateMongoError(sessionContext, err)
			}
			now := backend.now().UTC()
			for index, file := range files[:end] {
				for _, phase := range file.Artifact.Phases {
					for _, step := range phase.Steps {
						document, err := encodeMongoMigrationStepLedger(mongoMigrationStepLedgerRow{
							ID: mongoMigrationStepLedgerID(file.Name, phase.ID, step.ID), ArtifactName: file.Name, ArtifactDigest: file.Digest,
							PhaseID: phase.ID, StepID: step.ID, PhaseMode: phase.Mode, StepKind: step.Kind,
							State: mongoMigrationStepComplete, Attempts: 1, Owner: lease.owner, Fence: lease.fence, UpdatedAt: now, CompletedAt: &now,
						})
						if err != nil {
							return err
						}
						if _, err := backend.mongoMigrationCollection(mongoMigrationStepCollectionName).InsertOne(sessionContext, document); err != nil {
							return translateMongoError(sessionContext, err)
						}
					}
				}
				document, err := encodeMongoMigrationArtifactLedger(mongoMigrationArtifactLedgerRow{
					Position: index + 1, Name: file.Name, Digest: file.Digest, PreviousArtifactDigest: file.Artifact.PreviousArtifactDigest,
					FromDigest: file.Artifact.FromDigest, ToDigest: file.Artifact.ToDigest, PlannerName: file.Artifact.Planner.Name,
					PlannerVersion: file.Artifact.Planner.Version, StepCount: countMongoMigrationArtifactSteps(file.Artifact), AppliedAt: now,
				})
				if err != nil {
					return err
				}
				if _, err := backend.mongoMigrationCollection(mongoMigrationArtifactCollectionName).InsertOne(sessionContext, document); err != nil {
					return translateMongoError(sessionContext, err)
				}
			}
			if err := backend.writeMongoDevelopmentManifest(sessionContext, after); err != nil {
				return err
			}
			return lease.refreshUnlocked(sessionContext, backend.mongoMigrationCollection(mongoMigrationLeaseCollectionName))
		}); err != nil {
			return err
		}
		changed = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	// Publish a successful result only after the replacement transaction commits.
	if changed {
		for _, file := range files[:end] {
			recorded = append(recorded, file.Name)
		}
	}
	backend.clearVerifiedIndexes()
	physical := replay[end-1].physical
	if err := backend.verifyIndexPlans(ctx, physical.collections, physical.system); err != nil {
		return nil, fmt.Errorf("verify replacement baseline indexes: %w", err)
	}
	return recorded, nil
}

// mongoDBBlockingStep names the first step only the migration runner performs.
func mongoDBBlockingStep(file migrationartifact.File) string {
	for _, phase := range file.Artifact.Phases {
		for _, step := range phase.Steps {
			if mongoDBRunnerOnlyStep(step.Kind) {
				return string(step.Kind)
			}
		}
	}
	return ""
}
