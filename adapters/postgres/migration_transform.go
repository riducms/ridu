package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/riducms/ridu/internal/datatransform"
	"github.com/riducms/ridu/internal/migrationartifact"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func validatePostgresDataTransformDescriptors(transforms []ridumigration.DataTransformDescriptor) error {
	seen := make(map[string]struct{}, len(transforms))
	for _, transform := range transforms {
		if err := transform.Validate(); err != nil {
			return err
		}
		if _, duplicate := seen[transform.Name]; duplicate {
			return fmt.Errorf("PostgreSQL data transform %q is bound more than once", transform.Name)
		}
		seen[transform.Name] = struct{}{}
	}
	return nil
}

func postgresArtifactDataTransformDescriptors(artifact ridumigration.Artifact) ([]ridumigration.DataTransformDescriptor, error) {
	var descriptors []ridumigration.DataTransformDescriptor
	for _, phase := range artifact.Phases {
		for _, step := range phase.Steps {
			if step.Kind != ridumigration.StepDataTransform {
				continue
			}
			var payload ridumigration.DataTransformPayload
			if err := json.Unmarshal(step.Payload, &payload); err != nil {
				return nil, fmt.Errorf("decode data transform step %s: %w", step.ID, err)
			}
			descriptors = append(descriptors, payload.Transform)
		}
	}
	return descriptors, nil
}

func validatePostgresDataTransformIdentities(files []migrationartifact.File, additional []ridumigration.DataTransformDescriptor) error {
	checksums := make(map[string]string)
	validate := func(descriptors []ridumigration.DataTransformDescriptor) error {
		for _, descriptor := range descriptors {
			if err := descriptor.Validate(); err != nil {
				return err
			}
			if previous, exists := checksums[descriptor.Name]; exists && previous != descriptor.Checksum {
				return fmt.Errorf("PostgreSQL data transform %q changed checksum after immutable history; use a new transform name", descriptor.Name)
			}
			checksums[descriptor.Name] = descriptor.Checksum
		}
		return nil
	}
	for _, file := range files {
		descriptors, err := postgresArtifactDataTransformDescriptors(file.Artifact)
		if err != nil {
			return fmt.Errorf("inspect PostgreSQL migration %s data transforms: %w", file.Name, err)
		}
		if err := validate(descriptors); err != nil {
			return err
		}
	}
	return validate(additional)
}

type postgresDataTransformRegistry map[string]ridumigration.DataTransform

func newPostgresDataTransformRegistry(transforms []ridumigration.DataTransform) (postgresDataTransformRegistry, error) {
	registry := make(postgresDataTransformRegistry, len(transforms))
	for _, transform := range transforms {
		if err := transform.Validate(); err != nil {
			return nil, err
		}
		if _, duplicate := registry[transform.Name]; duplicate {
			return nil, fmt.Errorf("PostgreSQL data transform %q is registered more than once", transform.Name)
		}
		registry[transform.Name] = transform
	}
	return registry, nil
}

func requirePostgresDataTransformRegistry(files []migrationartifact.File, registry postgresDataTransformRegistry) error {
	if err := validatePostgresDataTransformIdentities(files, nil); err != nil {
		return err
	}
	for _, file := range files {
		descriptors, err := postgresArtifactDataTransformDescriptors(file.Artifact)
		if err != nil {
			return fmt.Errorf("inspect PostgreSQL migration %s data transforms: %w", file.Name, err)
		}
		for _, descriptor := range descriptors {
			registered, exists := registry[descriptor.Name]
			if !exists {
				return fmt.Errorf("PostgreSQL migration %s requires unregistered data transform %q", file.Name, descriptor.Name)
			}
			if registered.Checksum != descriptor.Checksum {
				return fmt.Errorf("PostgreSQL migration %s data transform %q checksum differs from the immutable artifact", file.Name, descriptor.Name)
			}
		}
	}
	return nil
}

// postgresMigrationDataTransaction is the document transaction a compiled
// data transform runs in. Operations are serialized, and none may run after
// the callback returns, because the native connection then returns to the
// migration runner. datatransform.Transaction admits every request first.
type postgresMigrationDataTransaction struct {
	operations  sync.Mutex
	closed      atomic.Bool
	transaction *documentTransaction
	native      *postgresMigrationPGXTransaction
}

func newPostgresMigrationDataTransaction(native *pgx.Conn) *postgresMigrationDataTransaction {
	transaction := &postgresMigrationDataTransaction{}
	transaction.native = &postgresMigrationPGXTransaction{connection: native}
	transaction.transaction = &documentTransaction{
		transaction: transaction.native,
		tableExists: make(map[string]bool),
	}
	return transaction
}

var errPostgresMigrationDataTransactionClosed = errors.New("PostgreSQL data transaction is no longer active")

func (transaction *postgresMigrationDataTransaction) beginOperation() error {
	if transaction.closed.Load() {
		return errPostgresMigrationDataTransactionClosed
	}
	transaction.operations.Lock()
	if transaction.closed.Load() {
		transaction.operations.Unlock()
		return errPostgresMigrationDataTransactionClosed
	}
	return nil
}

func (transaction *postgresMigrationDataTransaction) endOperation() {
	transaction.operations.Unlock()
}

func (transaction *postgresMigrationDataTransaction) invalidate() {
	transaction.closed.Store(true)
	transaction.operations.Lock()
	transaction.native.connection = nil
	transaction.operations.Unlock()
}

// serialized runs one document operation unless the transaction has closed.
func serialized[T any](transaction *postgresMigrationDataTransaction, operation func() (T, error)) (T, error) {
	if err := transaction.beginOperation(); err != nil {
		var zero T
		return zero, err
	}
	defer transaction.endOperation()
	return operation()
}

func (transaction *postgresMigrationDataTransaction) Create(ctx context.Context, request store.CreateRequest) (store.Document, error) {
	return serialized(transaction, func() (store.Document, error) { return transaction.transaction.Create(ctx, request) })
}

func (transaction *postgresMigrationDataTransaction) Find(ctx context.Context, request store.Request) (store.Document, error) {
	return serialized(transaction, func() (store.Document, error) { return transaction.transaction.Find(ctx, request) })
}

func (transaction *postgresMigrationDataTransaction) List(ctx context.Context, request store.Request) (store.Page, error) {
	return serialized(transaction, func() (store.Page, error) { return transaction.transaction.List(ctx, request) })
}

func (transaction *postgresMigrationDataTransaction) Update(ctx context.Context, request store.UpdateRequest) (store.Document, error) {
	return serialized(transaction, func() (store.Document, error) { return transaction.transaction.Update(ctx, request) })
}

func (transaction *postgresMigrationDataTransaction) Trash(ctx context.Context, request store.Request) (store.Document, error) {
	return serialized(transaction, func() (store.Document, error) { return transaction.transaction.Trash(ctx, request) })
}

func (transaction *postgresMigrationDataTransaction) Restore(ctx context.Context, request store.Request) (store.Document, error) {
	return serialized(transaction, func() (store.Document, error) { return transaction.transaction.Restore(ctx, request) })
}

func (transaction *postgresMigrationDataTransaction) Delete(ctx context.Context, request store.Request) (store.Document, error) {
	return serialized(transaction, func() (store.Document, error) { return transaction.transaction.Delete(ctx, request) })
}

func (transaction *postgresMigrationDataTransaction) DeleteDocumentState(ctx context.Context, reference store.DocumentReference) error {
	_, err := serialized(transaction, func() (struct{}, error) {
		return struct{}{}, transaction.transaction.DeleteDocumentState(ctx, reference)
	})
	return err
}

var _ datatransform.Documents = (*postgresMigrationDataTransaction)(nil)

func executePostgresDataTransform(ctx context.Context, connection *sql.Conn, artifact ridumigration.Artifact, transform ridumigration.DataTransform) error {
	// Raw holds database/sql's driver-connection lock until the callback and
	// invalidation finish. Transaction cancellation takes the same lock before
	// rolling back, so it cannot race the native pgx operations below.
	var callbackErr error
	rawErr := connection.Raw(func(driverConnection any) error {
		postgresConnection, ok := driverConnection.(*stdlib.Conn)
		if !ok {
			return fmt.Errorf("PostgreSQL migration connection uses unexpected driver %T", driverConnection)
		}
		native := postgresConnection.Conn()
		if native == nil {
			return fmt.Errorf("PostgreSQL migration connection is unavailable")
		}
		if native.IsClosed() || native.PgConn().TxStatus() != 'T' {
			return fmt.Errorf("compiled PostgreSQL data transaction is no longer active")
		}
		transaction := newPostgresMigrationDataTransaction(native)
		defer transaction.invalidate()
		callbackErr = transform.Up(ctx, datatransform.New(transaction, artifact, datatransform.Options{Engine: "PostgreSQL"}))
		return nil
	})
	if rawErr != nil {
		return rawErr
	}
	return callbackErr
}

type postgresMigrationPGXTransaction struct {
	connection *pgx.Conn
}

func (transaction postgresMigrationPGXTransaction) Begin(context.Context) (pgx.Tx, error) {
	return nil, fmt.Errorf("nested transactions are unavailable to PostgreSQL data transforms")
}

func (transaction postgresMigrationPGXTransaction) Commit(context.Context) error {
	return fmt.Errorf("commit is unavailable to PostgreSQL data transforms")
}

func (transaction postgresMigrationPGXTransaction) Rollback(context.Context) error {
	return fmt.Errorf("rollback is unavailable to PostgreSQL data transforms")
}

func (transaction postgresMigrationPGXTransaction) CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSource pgx.CopyFromSource) (int64, error) {
	return transaction.connection.CopyFrom(ctx, tableName, columnNames, rowSource)
}

func (transaction postgresMigrationPGXTransaction) SendBatch(ctx context.Context, batch *pgx.Batch) pgx.BatchResults {
	return transaction.connection.SendBatch(ctx, batch)
}

func (transaction postgresMigrationPGXTransaction) LargeObjects() pgx.LargeObjects {
	return pgx.LargeObjects{}
}

func (transaction postgresMigrationPGXTransaction) Prepare(ctx context.Context, name, statement string) (*pgconn.StatementDescription, error) {
	return transaction.connection.Prepare(ctx, name, statement)
}

func (transaction postgresMigrationPGXTransaction) Exec(ctx context.Context, statement string, arguments ...any) (pgconn.CommandTag, error) {
	return transaction.connection.Exec(ctx, statement, arguments...)
}

func (transaction postgresMigrationPGXTransaction) Query(ctx context.Context, statement string, arguments ...any) (pgx.Rows, error) {
	return transaction.connection.Query(ctx, statement, arguments...)
}

func (transaction postgresMigrationPGXTransaction) QueryRow(ctx context.Context, statement string, arguments ...any) pgx.Row {
	return transaction.connection.QueryRow(ctx, statement, arguments...)
}

func (transaction postgresMigrationPGXTransaction) Conn() *pgx.Conn {
	return transaction.connection
}

var _ pgx.Tx = (*postgresMigrationPGXTransaction)(nil)

type postgresProjectMigrationDriver struct {
	transforms []ridumigration.DataTransform
}

// ProjectMigrations binds checksum-protected callbacks into the compiled
// project while PostgreSQL retains ownership of every migration transaction
// and the credential-bearing connection boundary.
func ProjectMigrations(transforms ...ridumigration.DataTransform) ridumigration.ProjectDriver {
	return &postgresProjectMigrationDriver{transforms: append([]ridumigration.DataTransform(nil), transforms...)}
}

func (driver *postgresProjectMigrationDriver) Validate() error {
	_, err := newPostgresDataTransformRegistry(driver.transforms)
	return err
}

func (driver *postgresProjectMigrationDriver) DataTransforms() []ridumigration.DataTransformDescriptor {
	descriptors := make([]ridumigration.DataTransformDescriptor, 0, len(driver.transforms))
	for _, transform := range driver.transforms {
		descriptors = append(descriptors, transform.DataTransformDescriptor)
	}
	return descriptors
}

func (driver *postgresProjectMigrationDriver) RunProjectMigration(ctx context.Context, request ridumigration.ProjectRequest, executableManifest schema.Manifest) error {
	if err := request.Validate(); err != nil {
		return err
	}
	if request.DatabaseURL == "" {
		return fmt.Errorf("PostgreSQL project migration requires a private database URL")
	}
	registry, err := newPostgresDataTransformRegistry(driver.transforms)
	if err != nil {
		return err
	}
	files, err := migrationartifact.RequireCurrentHistoryForPlanner(request.Directory, executableManifest, atlasPlannerName)
	if err != nil {
		return err
	}
	return driver.runProjectMigrationFiles(ctx, request, files, registry)
}

func (driver *postgresProjectMigrationDriver) runProjectMigrationFiles(ctx context.Context, request ridumigration.ProjectRequest, files []migrationartifact.File, registry postgresDataTransformRegistry) error {
	if err := requirePostgresDataTransformRegistry(files, registry); err != nil {
		return err
	}
	options := RunnerOptions{
		AllowInsecureDatabase:    request.AllowInsecureDatabase,
		AllowMaintenance:         request.AllowMaintenance,
		AllowUnbounded:           request.AllowUnbounded,
		AdvisoryLockWait:         request.LockWait,
		LockTimeout:              request.LockTimeout,
		StatementTimeout:         request.StatementTimeout,
		BatchTimeout:             request.BatchTimeout,
		ConcurrentIndexTimeout:   request.OperationTimeout,
		IdleInTransactionTimeout: request.IdleTransactionTimeout,
		StopAfterPhase:           request.StopAfterPhase,
		StopAfterStep:            request.StopAfterStep,
	}
	options, err := normalizeRunnerOptions(options)
	if err != nil {
		return err
	}
	switch request.Action {
	case ridumigration.ProjectVerify:
		return verifyPostgresArtifactFiles(ctx, request.DatabaseURL, files, options, registry)
	case ridumigration.ProjectApply:
		backend, err := OpenWithConfig(ctx, PoolConfig{
			DatabaseURL: request.DatabaseURL, AllowInsecureTransport: request.AllowInsecureDatabase,
			ApplicationName: "ridu-project-migrations",
		})
		if err != nil {
			return err
		}
		defer backend.Close()
		return backend.applyArtifactFilesWithRegistry(ctx, files, options, registry)
	default:
		return fmt.Errorf("PostgreSQL project migrations support only up and verify; action %q is unsupported", request.Action)
	}
}

var _ ridumigration.ProjectDriver = (*postgresProjectMigrationDriver)(nil)
