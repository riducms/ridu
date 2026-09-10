package mongodb

import (
	"context"
	"fmt"
	"reflect"

	"github.com/riducms/ridu/internal/embedded"
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

type mongoDBMigrationDataTransaction struct {
	transaction                 *documentTransaction
	versionedResourceIdentities map[schema.StableID]struct{}
	resourceShapes              map[schema.StableID][]schema.Collection
	locales                     map[string]struct{}
}

func newMongoDBMigrationDataTransaction(transaction *documentTransaction, artifact ridumigration.Artifact) mongoDBMigrationDataTransaction {
	versioned := make(map[schema.StableID]struct{})
	resourceShapes := make(map[schema.StableID][]schema.Collection)
	locales := make(map[string]struct{})
	collect := func(snapshot schema.Snapshot) {
		if snapshot.Application.Localization != nil {
			for _, locale := range snapshot.Application.Localization.Locales {
				locales[string(locale.Code)] = struct{}{}
			}
		}
		resources := append(append([]schema.Collection(nil), snapshot.Collections...), snapshot.Globals...)
		for _, resource := range resources {
			if resource.Versions != nil || resource.Capabilities.Versions {
				versioned[resource.ID] = struct{}{}
			}
			known := false
			for _, shape := range resourceShapes[resource.ID] {
				if reflect.DeepEqual(shape, resource) {
					known = true
					break
				}
			}
			if !known {
				resourceShapes[resource.ID] = append(resourceShapes[resource.ID], resource)
			}
		}
	}
	if artifact.Before != nil {
		collect(*artifact.Before)
	}
	collect(artifact.After)
	return mongoDBMigrationDataTransaction{
		transaction: transaction, versionedResourceIdentities: versioned,
		resourceShapes: resourceShapes, locales: locales,
	}
}

func (transaction mongoDBMigrationDataTransaction) Create(ctx context.Context, request store.CreateRequest) (store.Document, error) {
	if err := transaction.validateMutation(request.Collection, request.Values); err != nil {
		return store.Document{}, err
	}
	if err := transaction.authorize(request.Collection, request.Locales); err != nil {
		return store.Document{}, err
	}
	return transaction.transaction.Create(ctx, request)
}

func (transaction mongoDBMigrationDataTransaction) Find(ctx context.Context, request store.Request) (store.Document, error) {
	if err := transaction.requireResource(request.Collection); err != nil {
		return store.Document{}, err
	}
	if err := transaction.authorize(request.Collection, request.Locales); err != nil {
		return store.Document{}, err
	}
	return transaction.transaction.Find(ctx, request)
}

func (transaction mongoDBMigrationDataTransaction) List(ctx context.Context, request store.Request) (store.Page, error) {
	if err := transaction.requireResource(request.Collection); err != nil {
		return store.Page{}, err
	}
	if err := transaction.authorize(request.Collection, request.Locales); err != nil {
		return store.Page{}, err
	}
	return transaction.transaction.List(ctx, request)
}

func (transaction mongoDBMigrationDataTransaction) Update(ctx context.Context, request store.UpdateRequest) (store.Document, error) {
	if err := transaction.validateMutation(request.Collection, request.Values); err != nil {
		return store.Document{}, err
	}
	if err := transaction.authorize(request.Collection, request.Locales); err != nil {
		return store.Document{}, err
	}
	return transaction.transaction.Update(ctx, request)
}

func (transaction mongoDBMigrationDataTransaction) Trash(ctx context.Context, request store.Request) (store.Document, error) {
	if err := transaction.validateMutation(request.Collection, nil); err != nil {
		return store.Document{}, err
	}
	if err := transaction.authorize(request.Collection, request.Locales); err != nil {
		return store.Document{}, err
	}
	return transaction.transaction.Trash(ctx, request)
}

func (transaction mongoDBMigrationDataTransaction) Restore(ctx context.Context, request store.Request) (store.Document, error) {
	if err := transaction.validateMutation(request.Collection, nil); err != nil {
		return store.Document{}, err
	}
	if err := transaction.authorize(request.Collection, request.Locales); err != nil {
		return store.Document{}, err
	}
	return transaction.transaction.Restore(ctx, request)
}

func (transaction mongoDBMigrationDataTransaction) Delete(ctx context.Context, request store.Request) (store.Document, error) {
	if err := transaction.validateMutation(request.Collection, nil); err != nil {
		return store.Document{}, err
	}
	if err := transaction.authorize(request.Collection, request.Locales); err != nil {
		return store.Document{}, err
	}
	return transaction.transaction.Delete(ctx, request)
}

func (transaction mongoDBMigrationDataTransaction) validateMutation(collection schema.Collection, values store.Values) error {
	if err := transaction.requireResource(collection); err != nil {
		return err
	}
	if _, versioned := transaction.versionedResourceIdentities[collection.ID]; versioned {
		return fmt.Errorf("MongoDB data transforms cannot mutate versioned resource %q; retained snapshots require typed rename executors", collection.ID)
	}
	if values != nil {
		return transaction.validateValues(collection.Fields, values, string(collection.ID), nil)
	}
	return nil
}

func (transaction mongoDBMigrationDataTransaction) requireResource(collection schema.Collection) error {
	shapes := transaction.resourceShapes[collection.ID]
	if len(shapes) == 0 {
		return fmt.Errorf("MongoDB data transform resource %q is outside the immutable artifact manifests", collection.ID)
	}
	for _, shape := range shapes {
		if reflect.DeepEqual(shape, collection) {
			return nil
		}
	}
	return fmt.Errorf("MongoDB data transform resource %q must exactly match its immutable before or after manifest shape", collection.ID)
}

func (transaction mongoDBMigrationDataTransaction) authorize(collection schema.Collection, locales []schema.LocaleCode) error {
	for _, locale := range locales {
		if _, allowed := transaction.locales[string(locale)]; !allowed {
			return fmt.Errorf("MongoDB data transform locale %q is outside the immutable manifests", locale)
		}
	}
	definitions, err := mongoContentIndexesForLocales(collection, locales)
	if err != nil {
		return err
	}
	fingerprint, err := mongoIndexFingerprint(definitions)
	if err != nil {
		return err
	}
	transaction.transaction.store.indexesMu.Lock()
	transaction.transaction.store.verifiedIndexes[collection.ID] = mongoVerifiedIndexPlan{
		fingerprint: fingerprint, locales: append([]schema.LocaleCode(nil), locales...),
	}
	transaction.transaction.store.indexesMu.Unlock()
	return nil
}

func (transaction mongoDBMigrationDataTransaction) validateValues(fields []schema.Field, values store.Values, path string, special map[string]struct{}) error {
	byName := make(map[string]schema.Field, len(fields))
	for _, field := range fields {
		if field.Category != schema.FieldCategoryPresentation {
			byName[field.Name] = field
		}
	}
	for name, value := range values {
		if _, allowed := special[name]; allowed {
			continue
		}
		field, exists := byName[name]
		if !exists {
			return fmt.Errorf("MongoDB data transform value %q is outside the immutable resource shape", path+"."+name)
		}
		if err := transaction.validateValue(field, value, path+"."+name); err != nil {
			return err
		}
	}
	return nil
}

func (transaction mongoDBMigrationDataTransaction) validateValue(field schema.Field, value store.Value, path string) error {
	if value.Kind() == store.ValueNull {
		return nil
	}
	if field.Localized {
		localized, valid := value.CopyObject()
		if !valid {
			return fmt.Errorf("MongoDB data transform localized value %q must be an object", path)
		}
		field.Localized = false
		for locale, localizedValue := range localized {
			if _, allowed := transaction.locales[locale]; !allowed {
				return fmt.Errorf("MongoDB data transform locale %q at %q is outside the immutable manifests", locale, path)
			}
			if err := transaction.validateValue(field, localizedValue, path+"."+locale); err != nil {
				return err
			}
		}
		return nil
	}
	if embedded.HasFields(field) {
		if err := embedded.ValidateValue(field, value, path, true, nil); err != nil {
			return err
		}
		_, err := embedded.Transform(field, value, path, embedded.NewBudget(), func(o embedded.Occurrence) (store.Values, error) {
			return o.Payload, transaction.validateValues(o.Fields, o.Payload, o.RuntimePath, map[string]struct{}{o.Case.Identity: {}, o.Case.Discriminator: {}})
		})
		return err
	}

	switch field.Type {
	case schema.FieldTypeGroup:
		object, valid := value.CopyObject()
		if !valid || field.Nested == nil {
			return fmt.Errorf("MongoDB data transform group value %q must match its immutable resource shape", path)
		}
		return transaction.validateValues(field.Nested.ResolvedFields(), object, path, nil)
	case schema.FieldTypeArray:
		items, valid := value.CopyList()
		if !valid || field.Nested == nil {
			return fmt.Errorf("MongoDB data transform array value %q must match its immutable resource shape", path)
		}
		special := map[string]struct{}{"_key": {}}
		for index, item := range items {
			object, valid := item.CopyObject()
			if !valid {
				return fmt.Errorf("MongoDB data transform array row %q must be an object", fmt.Sprintf("%s.%d", path, index))
			}
			if err := transaction.validateValues(field.Nested.ResolvedFields(), object, fmt.Sprintf("%s.%d", path, index), special); err != nil {
				return err
			}
		}
	case schema.FieldTypeBlocks:
		items, valid := value.CopyList()
		if !valid || field.Blocks == nil {
			return fmt.Errorf("MongoDB data transform blocks value %q must match its immutable resource shape", path)
		}
		special := map[string]struct{}{"_key": {}, "blockType": {}}
		for index, item := range items {
			itemPath := fmt.Sprintf("%s.%d", path, index)
			object, valid := item.CopyObject()
			if !valid {
				return fmt.Errorf("MongoDB data transform block %q must be an object", itemPath)
			}
			blockKey, valid := object["blockType"].StringValue()
			if !valid {
				return fmt.Errorf("MongoDB data transform block %q must name an immutable block type", itemPath)
			}
			var blockFields []schema.Field
			found := false
			for _, block := range field.Blocks.ResolvedTypes() {
				if block.Slug == blockKey {
					blockFields = block.ResolvedFields()
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("MongoDB data transform block type %q at %q is outside the immutable resource shape", blockKey, itemPath)
			}
			if err := transaction.validateValues(blockFields, object, itemPath, special); err != nil {
				return err
			}
		}
	}
	return nil
}

var _ ridumigration.DataTransaction = mongoDBMigrationDataTransaction{}

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
	if request.Action == ridumigration.ProjectVerify && mongoDBReplayRequiresMaintenance(replay) && !options.AllowMaintenance {
		return fmt.Errorf("MongoDB semantic migrations require explicit maintenance admission before opening the database")
	}
	config := Config{
		DatabaseURL: request.DatabaseURL, AllowInsecureTransport: request.AllowInsecureDatabase,
		ApplicationName: "ridu-project-migrations",
	}
	switch request.Action {
	case ridumigration.ProjectVerify:
		return verifyPreparedMongoDBArtifactReplay(ctx, config, files, replay, normalized, registry, options.AllowMaintenance)
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
