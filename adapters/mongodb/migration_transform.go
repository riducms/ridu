package mongodb

import (
	"context"
	"fmt"

	"github.com/riducms/ridu/internal/datatransform"
	"github.com/riducms/ridu/internal/migrationartifact"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

type mongoDBDataTransformRegistry map[string]ridumigration.DataTransform

func newMongoDBDataTransformRegistry(transforms []ridumigration.DataTransform) (mongoDBDataTransformRegistry, error) {
	registry := make(mongoDBDataTransformRegistry, len(transforms))
	for _, transform := range transforms {
		if err := transform.Validate(); err != nil {
			return nil, err
		}
		if _, duplicate := registry[transform.Name]; duplicate {
			return nil, fmt.Errorf("MongoDB data transform %q is registered more than once", transform.Name)
		}
		registry[transform.Name] = transform
	}
	return registry, nil
}

func requireMongoDBDataTransformRegistry(files []migrationartifact.File, registry mongoDBDataTransformRegistry) error {
	for _, file := range files {
		options, err := mongoDBArtifactSemanticOptions(file.Artifact)
		if err != nil {
			return fmt.Errorf("inspect MongoDB migration %s data transforms: %w", file.Name, err)
		}
		for _, descriptor := range options.DataTransforms {
			registered, exists := registry[descriptor.Name]
			if !exists {
				return fmt.Errorf("MongoDB migration %s requires unregistered data transform %q", file.Name, descriptor.Name)
			}
			if registered.Checksum != descriptor.Checksum {
				return fmt.Errorf("MongoDB migration %s data transform %q checksum differs from the immutable artifact", file.Name, descriptor.Name)
			}
		}
	}
	return nil
}

// newMongoDBMigrationDataTransaction gives a compiled data transform the
// shared admission and locked-update surface. Admission also records the
// resource's private index plan for the requested locales, which the document
// operations require and the migration phase created.
func newMongoDBMigrationDataTransaction(transaction *documentTransaction, artifact ridumigration.Artifact) ridumigration.DataTransaction {
	return datatransform.New(mongoDBMigrationDocuments{transaction}, artifact, datatransform.Options{
		Engine: "MongoDB",
		Admit: func(collection schema.Collection, locales []schema.LocaleCode) error {
			definitions, err := mongoContentIndexesForLocales(collection, locales)
			if err != nil {
				return err
			}
			fingerprint, err := mongoIndexFingerprint(definitions)
			if err != nil {
				return err
			}
			transaction.store.indexesMu.Lock()
			transaction.store.verifiedIndexes[collection.ID] = mongoVerifiedIndexPlan{
				fingerprint: fingerprint, locales: append([]schema.LocaleCode(nil), locales...),
			}
			transaction.store.indexesMu.Unlock()
			return nil
		},
	})
}

// mongoDBMigrationDocuments is the migration transaction's document surface.
// Serving-index verification gates DeleteDocumentState in application
// traffic, but a migration phase owns the physical state it writes, so a
// transform delete removes the document's state directly. Transforms cannot
// mutate versioned resources, so there are no versions to remove, and the
// delete already removed the document's own reference rows.
type mongoDBMigrationDocuments struct {
	*documentTransaction
}

func (documents mongoDBMigrationDocuments) DeleteDocumentState(ctx context.Context, reference store.DocumentReference) error {
	sessionContext, leave, err := documents.enter(ctx, true)
	if err != nil {
		return err
	}
	defer leave()
	if err := store.ValidateDocumentID(reference.DocumentID); err != nil {
		return err
	}
	return documents.deleteDocumentState(ctx, sessionContext, reference, false, false)
}

var _ datatransform.Documents = mongoDBMigrationDocuments{}

type mongoDBProjectMigrationDriver struct {
	transforms []ridumigration.DataTransform
}

// ProjectMigrations binds checksum-protected callbacks into the compiled
// project while MongoDB retains ownership of transactions, checkpoints, and
// the credential-bearing connection boundary.
func ProjectMigrations(transforms ...ridumigration.DataTransform) ridumigration.ProjectDriver {
	return &mongoDBProjectMigrationDriver{transforms: append([]ridumigration.DataTransform(nil), transforms...)}
}

func (driver *mongoDBProjectMigrationDriver) Validate() error {
	_, err := newMongoDBDataTransformRegistry(driver.transforms)
	return err
}

func (driver *mongoDBProjectMigrationDriver) DataTransforms() []ridumigration.DataTransformDescriptor {
	descriptors := make([]ridumigration.DataTransformDescriptor, 0, len(driver.transforms))
	for _, transform := range driver.transforms {
		descriptors = append(descriptors, transform.DataTransformDescriptor)
	}
	return descriptors
}

func (driver *mongoDBProjectMigrationDriver) RunProjectMigration(ctx context.Context, request ridumigration.ProjectRequest, executableManifest schema.Manifest) error {
	if err := request.Validate(); err != nil {
		return err
	}
	if request.LockTimeout != 0 || request.StatementTimeout != 0 || request.BatchTimeout != 0 ||
		request.IdleTransactionTimeout != 0 || request.StopAfterPhase != "" || request.StopAfterStep != "" {
		return fmt.Errorf("MongoDB project migrations do not accept PostgreSQL-only timeout or stop-boundary options")
	}
	if request.DatabaseURL == "" {
		return fmt.Errorf("MongoDB project migration requires a private database URL")
	}
	registry, err := newMongoDBDataTransformRegistry(driver.transforms)
	if err != nil {
		return err
	}
	files, err := migrationartifact.RequireCurrentHistory(request.Directory, executableManifest)
	if err != nil {
		return err
	}
	return driver.runProjectMigrationFiles(ctx, request, files, registry)
}

func (driver *mongoDBProjectMigrationDriver) runProjectMigrationFiles(
	ctx context.Context,
	request ridumigration.ProjectRequest,
	files []migrationartifact.File,
	registry mongoDBDataTransformRegistry,
) error {
	replay, err := prepareMongoDBArtifactReplay(ctx, files)
	if err != nil {
		return err
	}
	if err := requireMongoDBDataTransformRegistry(files, registry); err != nil {
		return err
	}
	options := RunnerOptions{
		AllowMaintenance: request.AllowMaintenance,
		AllowUnbounded:   request.AllowUnbounded,
		LeaseWait:        request.LockWait,
		OperationTimeout: request.OperationTimeout,
	}
	normalized, err := normalizeMongoMigrationRunnerOptions(options)
	if err != nil {
		return err
	}
	config := Config{
		DatabaseURL: request.DatabaseURL, AllowInsecureTransport: request.AllowInsecureDatabase,
		ApplicationName: "ridu-project-migrations",
	}
	switch request.Action {
	case ridumigration.ProjectVerify:
		// Verification replays into a dropped shadow database with no traffic.
		return verifyPreparedMongoDBArtifactReplay(ctx, config, files, replay, normalized, registry, true)
	case ridumigration.ProjectApply:
		backend, err := OpenWithConfig(ctx, config)
		if err != nil {
			return err
		}
		defer backend.Close()
		return backend.applyPreparedMongoDBArtifactReplay(ctx, files, replay, normalized, registry, options.AllowMaintenance)
	default:
		return fmt.Errorf("MongoDB project migrations support only up and verify; action %q is unsupported", request.Action)
	}
}

var _ ridumigration.ProjectDriver = (*mongoDBProjectMigrationDriver)(nil)
