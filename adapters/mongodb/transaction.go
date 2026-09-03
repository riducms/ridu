package mongodb

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readconcern"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
	"go.mongodb.org/mongo-driver/v2/mongo/writeconcern"
)

const (
	transientTransactionErrorLabel      = "TransientTransactionError"
	unknownTransactionCommitResultLabel = "UnknownTransactionCommitResult"
	commitRetryTimeout                  = 120 * time.Second
)

type transactionState uint8

const (
	transactionOpen transactionState = iota
	transactionCommitted
	transactionRolledBack
	transactionCommitUnknown
)

type documentTransaction struct {
	store                 *Store
	session               *mongo.Session
	readOnly              bool
	authBootstrapPrepared bool
	uploadLockReleases    []*mongoUploadLockRelease
	uploadLockedKeys      map[string]struct{}
	uploadLockCleanupErr  error

	mu    sync.Mutex
	state transactionState
}

type mongoConfirmedTransactionConflict struct{}

func (mongoConfirmedTransactionConflict) Error() string {
	return store.ErrConflict.Error()
}

func (mongoConfirmedTransactionConflict) Unwrap() error {
	return store.ErrConflict
}

func isMongoConfirmedTransactionConflict(err error) bool {
	var conflict mongoConfirmedTransactionConflict
	return errors.As(err, &conflict)
}

// Begin opens a majority-committed, snapshot-isolated write transaction.
func (backend *Store) Begin(ctx context.Context) (store.Transaction, error) {
	return backend.begin(ctx, false)
}

// BeginSnapshot opens the same stable snapshot in read-only adapter mode.
func (backend *Store) BeginSnapshot(ctx context.Context) (store.Transaction, error) {
	return backend.begin(ctx, true)
}

func (backend *Store) begin(ctx context.Context, readOnly bool) (*documentTransaction, error) {
	if ctx == nil {
		return nil, fmt.Errorf("MongoDB transaction context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if backend == nil || backend.client == nil || backend.database == nil {
		return nil, fmt.Errorf("MongoDB database is unavailable")
	}
	backend.closeMu.Lock()
	closed := backend.closed
	backend.closeMu.Unlock()
	if closed {
		return nil, fmt.Errorf("MongoDB database is closed")
	}
	session, err := backend.client.StartSession()
	if err != nil {
		return nil, translateMongoError(ctx, err)
	}
	transactionOptions := options.Transaction().
		SetReadConcern(readconcern.Snapshot()).
		SetReadPreference(readpref.Primary()).
		SetWriteConcern(writeconcern.Majority())
	if err := session.StartTransaction(transactionOptions); err != nil {
		session.EndSession(ctx)
		return nil, translateMongoError(ctx, err)
	}
	return &documentTransaction{store: backend, session: session, readOnly: readOnly}, nil
}

func (transaction *documentTransaction) enter(ctx context.Context, writable bool) (context.Context, func(), error) {
	if ctx == nil {
		return nil, nil, fmt.Errorf("MongoDB transaction context is required")
	}
	transaction.mu.Lock()
	leave := transaction.mu.Unlock
	if err := ctx.Err(); err != nil {
		leave()
		return nil, nil, err
	}
	if transaction.session == nil || transaction.state != transactionOpen {
		state := transaction.state
		leave()
		return nil, nil, fmt.Errorf("MongoDB transaction is not open (state %s)", state)
	}
	if writable && transaction.readOnly {
		leave()
		return nil, nil, fmt.Errorf("MongoDB snapshot transaction is read-only")
	}
	return mongo.NewSessionContext(ctx, transaction.session), leave, nil
}

func (state transactionState) String() string {
	switch state {
	case transactionOpen:
		return "open"
	case transactionCommitted:
		return "committed"
	case transactionRolledBack:
		return "rolled back"
	case transactionCommitUnknown:
		return "commit outcome unknown"
	default:
		return "invalid"
	}
}

func (transaction *documentTransaction) Commit(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("MongoDB transaction context is required")
	}
	transaction.mu.Lock()
	defer transaction.mu.Unlock()
	if transaction.session == nil || transaction.state != transactionOpen {
		return fmt.Errorf("MongoDB transaction cannot commit from state %s", transaction.state)
	}
	if err := ctx.Err(); err != nil {
		cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), defaultCloseTimeout)
		defer cancel()
		_ = transaction.session.AbortTransaction(mongo.NewSessionContext(cleanupContext, transaction.session))
		transaction.state = transactionRolledBack
		transaction.session.EndSession(cleanupContext)
		transaction.session = nil
		return errors.Join(err, transaction.releaseUploadObjectLocks())
	}
	// Once a commit is in flight, caller cancellation must not manufacture an
	// ambiguous outcome. Mirror the driver's bounded commit-loop policy without
	// using WithTransaction, which could replay application hooks and writes.
	commitContext, cancelCommit := context.WithTimeout(context.WithoutCancel(ctx), commitRetryTimeout)
	defer cancelCommit()
	backoff := 10 * time.Millisecond
	for {
		err := transaction.session.CommitTransaction(mongo.NewSessionContext(commitContext, transaction.session))
		if err == nil {
			transaction.state = transactionCommitted
			transaction.session.EndSession(commitContext)
			transaction.session = nil
			// A confirmed content commit must remain a successful Commit result.
			// Exact upload-lock cleanup retries while the Store remains open; only
			// concurrent Store shutdown can stop it and leave private fail-closed
			// state for quiesced operator reconciliation.
			transaction.uploadLockCleanupErr = transaction.releaseUploadObjectLocks()
			return nil
		}
		if commitContext.Err() != nil {
			transaction.finishUnknownCommit(ctx)
			return fmt.Errorf("MongoDB commit outcome is unknown after the bounded retry window")
		}
		if !hasMongoErrorLabel(err, unknownTransactionCommitResultLabel) || isMaxTimeMSExpiredError(err) {
			if hasMongoErrorLabel(err, unknownTransactionCommitResultLabel) {
				transaction.finishUnknownCommit(ctx)
				return fmt.Errorf("MongoDB commit outcome is unknown after the server commit time limit")
			}
			transaction.state = transactionRolledBack
			cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), defaultCloseTimeout)
			transaction.session.EndSession(cleanupContext)
			cancel()
			transaction.session = nil
			translated := translateMongoError(commitContext, err)
			if transaction.authBootstrapPrepared && isMongoConfirmedTransactionConflict(translated) {
				return errors.Join(store.ErrAuthInitialized, transaction.releaseUploadObjectLocks())
			}
			return errors.Join(translated, transaction.releaseUploadObjectLocks())
		}
		timer := time.NewTimer(backoff)
		select {
		case <-commitContext.Done():
			if !timer.Stop() {
				<-timer.C
			}
			transaction.finishUnknownCommit(ctx)
			return fmt.Errorf("MongoDB commit outcome is unknown after the bounded retry window")
		case <-timer.C:
		}
		if backoff < 250*time.Millisecond {
			backoff *= 2
			if backoff > 250*time.Millisecond {
				backoff = 250 * time.Millisecond
			}
		}
	}
}

func (transaction *documentTransaction) finishUnknownCommit(ctx context.Context) {
	transaction.state = transactionCommitUnknown
	cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), defaultCloseTimeout)
	defer cancel()
	// The adapter admits replica sets only. Ending the session releases its
	// pinned resources; the caller still receives an explicitly unknown outcome
	// and must reconcile by document identity instead of retrying hooks blindly.
	// Separately committed upload-object locks remain fail-closed until that
	// reconciliation is complete and an operator removes their exact rows.
	transaction.session.EndSession(cleanupContext)
	transaction.session = nil
	transaction.abandonUploadObjectLocks()
}

func (transaction *documentTransaction) Rollback(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("MongoDB transaction context is required")
	}
	transaction.mu.Lock()
	defer transaction.mu.Unlock()
	if transaction.session == nil || transaction.state != transactionOpen {
		return fmt.Errorf("MongoDB transaction cannot roll back from state %s", transaction.state)
	}
	cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), defaultCloseTimeout)
	defer cancel()
	err := transaction.session.AbortTransaction(mongo.NewSessionContext(cleanupContext, transaction.session))
	transaction.state = transactionRolledBack
	transaction.session.EndSession(cleanupContext)
	transaction.session = nil
	return errors.Join(translateMongoError(cleanupContext, err), transaction.releaseUploadObjectLocks())
}

func hasMongoErrorLabel(err error, label string) bool {
	var labeled mongo.LabeledError
	return errors.As(err, &labeled) && labeled.HasErrorLabel(label)
}

func isMaxTimeMSExpiredError(err error) bool {
	var commandError mongo.CommandError
	return errors.As(err, &commandError) && commandError.IsMaxTimeMSExpiredError()
}

func translateMongoError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if ctx != nil {
		if contextError := ctx.Err(); contextError != nil {
			return contextError
		}
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) || mongo.IsTimeout(err) {
		return context.DeadlineExceeded
	}
	if errors.Is(err, mongo.ErrNoDocuments) {
		return store.ErrNotFound
	}
	if mongo.IsDuplicateKeyError(err) || hasMongoErrorLabel(err, transientTransactionErrorLabel) {
		return mongoConfirmedTransactionConflict{}
	}
	var serverError mongo.ServerError
	if errors.As(err, &serverError) {
		for _, code := range serverError.ErrorCodes() {
			switch code {
			case 112, 244, 251:
				return mongoConfirmedTransactionConflict{}
			case 10334:
				return fmt.Errorf("MongoDB document exceeds the supported BSON size")
			}
		}
		codes := serverError.ErrorCodes()
		if len(codes) != 0 {
			return fmt.Errorf("MongoDB operation failed with server code %d", codes[0])
		}
		return fmt.Errorf("MongoDB server operation failed")
	}
	return fmt.Errorf("MongoDB operation failed")
}

func validateCollectionEnvelope(collection schema.Collection) error {
	if !schema.IsValidStableID(string(collection.ID)) {
		return fmt.Errorf("MongoDB collection requires a valid stable ID")
	}
	if collection.Capabilities.Versions != (collection.Versions != nil) {
		return fmt.Errorf("MongoDB collection %q has inconsistent version capability metadata", collection.ID)
	}
	if collection.Capabilities.Locking != (collection.DocumentLock != nil) {
		return fmt.Errorf("MongoDB collection %q has inconsistent document-lock capability metadata", collection.ID)
	}
	if collection.Capabilities.Auth != (collection.Auth != nil) {
		return fmt.Errorf("MongoDB collection %q has inconsistent auth capability metadata", collection.ID)
	}
	if collection.Auth != nil {
		if _, err := validateMongoAuthCollectionMetadata(collection); err != nil {
			return err
		}
	}
	if err := validateMongoUploadCollectionMetadata(collection); err != nil {
		return err
	}
	if err := validateMongoFieldEnvelope(collection.Fields, nil); err != nil {
		return err
	}
	_, err := mongoDeclaredIndexesForLocales(collection, []schema.LocaleCode{"ridu-validation"})
	return err
}

func validateMongoFieldEnvelope(fields []schema.Field, ancestors []string) error {
	return validateMongoFieldEnvelopeAt(fields, ancestors, false)
}

func validateMongoFieldEnvelopeAt(fields []schema.Field, ancestors []string, insideRepeated bool) error {
	if len(ancestors) >= query.MaxPathSegments {
		return fmt.Errorf("MongoDB field nesting exceeds %d path segments", query.MaxPathSegments)
	}
	for _, field := range fields {
		if field.Category == schema.FieldCategoryPresentation {
			continue
		}
		segments := append(append([]string(nil), ancestors...), field.Name)
		path := strings.Join(segments, ".")
		repeatedSelect := field.Type == schema.FieldTypeSelect && field.Select != nil && field.Select.HasMany
		repeatedContainer := field.Type == schema.FieldTypeArray || field.Type == schema.FieldTypeBlocks
		if insideRepeated {
			if field.Index || field.Unique {
				return fmt.Errorf("MongoDB adapter does not support indexes on field %q inside a repeated field", path)
			}
		}
		if field.Unique && len(ancestors) != 0 {
			return fmt.Errorf("MongoDB adapter cannot enforce unique nested field %q; use a declared compound index from the collection root", path)
		}
		if repeatedContainer || repeatedSelect {
			if field.Index || field.Unique {
				return fmt.Errorf("MongoDB adapter does not support indexed or unique repeated field %q", path)
			}
		}
		if field.Name == "" || field.Path.String() != path || len(field.Path.Segments()) != len(segments) {
			return fmt.Errorf("MongoDB field %q does not have its canonical resolved path %q", field.Name, path)
		}
		if field.Type == schema.FieldTypeGroup {
			if field.Category != schema.FieldCategoryNested || field.Nested == nil {
				return fmt.Errorf("MongoDB group field %q does not have a nested field contract", path)
			}
			if err := validateMongoFieldEnvelopeAt(field.Nested.Fields, segments, insideRepeated); err != nil {
				return err
			}
			continue
		}
		if field.Type == schema.FieldTypeArray {
			if field.Category != schema.FieldCategoryNested || field.Nested == nil {
				return fmt.Errorf("MongoDB array field %q does not have a nested field contract", path)
			}
			if err := validateMongoFieldEnvelopeAt(field.Nested.Fields, segments, true); err != nil {
				return err
			}
			continue
		}
		if field.Type == schema.FieldTypeBlocks {
			if field.Category != schema.FieldCategoryNested || field.Blocks == nil || len(field.Blocks.Types) == 0 {
				return fmt.Errorf("MongoDB blocks field %q does not have a block contract", path)
			}
			for _, block := range field.Blocks.Types {
				for _, blockField := range block.Fields {
					if blockField.Name == "blockType" {
						return fmt.Errorf("MongoDB block %q in field %q declares reserved discriminator field \"blockType\"", block.Key, path)
					}
				}
				if err := validateMongoFieldEnvelopeAt(block.Fields, append(append([]string(nil), segments...), block.Key), true); err != nil {
					return err
				}
			}
			continue
		}
		if field.Type == schema.FieldTypeRelationship {
			if err := validateMongoRelationshipEnvelope(field, path); err != nil {
				return err
			}
			continue
		}
		if field.Type == schema.FieldTypeUpload {
			if err := validateMongoUploadEnvelope(field, path); err != nil {
				return err
			}
			continue
		}
		if len(ancestors) == 0 && mongoUploadSizesField(field) {
			continue
		}
		if field.Category != schema.FieldCategoryScalar &&
			!(field.Type == schema.FieldTypePlugin && field.Category == schema.FieldCategoryPlugin) &&
			!mongoFrameworkUploadMetadataField(field) {
			return fmt.Errorf("MongoDB adapter currently supports scalar and plugin fields, references, framework upload metadata, and nested containers; field %q is unsupported", path)
		}
		switch field.Type {
		case schema.FieldTypeText, schema.FieldTypeCode, schema.FieldTypeTextarea, schema.FieldTypeEmail,
			schema.FieldTypeDate, schema.FieldTypeNumber, schema.FieldTypeCheckbox, schema.FieldTypeRadio,
			schema.FieldTypeJSON, schema.FieldTypePoint:
		case schema.FieldTypeSelect:
			if field.Select == nil {
				return fmt.Errorf("MongoDB select field %q does not have a select contract", path)
			}
		case schema.FieldTypePlugin:
			if field.Category != schema.FieldCategoryPlugin || field.Plugin == nil || strings.TrimSpace(field.Plugin.Key) == "" {
				return fmt.Errorf("MongoDB plugin field %q does not have a plugin contract", path)
			}
		default:
			return fmt.Errorf("MongoDB adapter currently does not support field %q of type %q", path, field.Type)
		}
	}
	return nil
}

func validateRequestEnvelope(request store.Request) error {
	if err := validateCollectionEnvelope(request.Collection); err != nil {
		return err
	}
	if request.IndexWindow != nil {
		return fmt.Errorf("MongoDB adapter does not yet support list index windows")
	}
	if err := validateMongoPopulationEnvelope(request); err != nil {
		return err
	}
	if _, err := requestProjection(request); err != nil {
		return err
	}
	if request.Lock != store.LockNone {
		return fmt.Errorf("MongoDB adapter does not yet support document lock mode %q", request.Lock)
	}
	if request.ExpectedRevision < 0 {
		return fmt.Errorf("MongoDB expected revision cannot be negative")
	}
	if request.ExpectedRevision != 0 && request.Collection.Versions == nil {
		return fmt.Errorf("MongoDB expected revisions require a versioned collection")
	}
	if !request.Collection.Capabilities.Trash && request.Deletion != store.DeletionActive {
		return fmt.Errorf("MongoDB deletion mode %q requires a trash-enabled collection", request.Deletion)
	}
	switch request.Deletion {
	case store.DeletionActive, store.DeletionTrash, store.DeletionAll:
	default:
		return fmt.Errorf("unsupported MongoDB deletion mode %q", request.Deletion)
	}
	return nil
}

func validateValues(collection schema.Collection, values store.Values) error {
	return validateMongoFieldValues(collection.ID, collection.Fields, values, nil, false, false, nil, nil)
}

func validateCompleteValues(collection schema.Collection, values store.Values) error {
	return validateMongoFieldValues(collection.ID, collection.Fields, values, nil, true, false, nil, nil)
}

func validatePatchValues(collection schema.Collection, values store.Values) error {
	return validateMongoFieldValues(collection.ID, collection.Fields, values, nil, false, true, nil, nil)
}

func validateCompleteValuesForLocales(collection schema.Collection, values store.Values, locales []schema.LocaleCode) error {
	configured, err := mongoConfiguredLocales(locales)
	if err != nil {
		return err
	}
	return validateMongoFieldValues(collection.ID, collection.Fields, values, nil, true, false, configured, nil)
}

func validatePatchValuesForLocales(collection schema.Collection, values store.Values, locales []schema.LocaleCode) error {
	configured, err := mongoConfiguredLocales(locales)
	if err != nil {
		return err
	}
	return validateMongoFieldValues(collection.ID, collection.Fields, values, nil, false, true, configured, nil)
}

func mongoConfiguredLocales(locales []schema.LocaleCode) (map[string]struct{}, error) {
	configured := make(map[string]struct{}, len(locales))
	for index, locale := range locales {
		code := string(locale)
		if !schema.IsValidLocaleCode(code) {
			return nil, fmt.Errorf("MongoDB locale %d %q is invalid", index, code)
		}
		if _, duplicate := configured[code]; duplicate {
			return nil, fmt.Errorf("MongoDB locale %q is duplicated", code)
		}
		configured[code] = struct{}{}
	}
	return configured, nil
}

func validateMongoFieldValues(
	collectionID schema.StableID,
	fields []schema.Field,
	values store.Values,
	ancestors []string,
	requireMissing bool,
	patchRoot bool,
	configuredLocales map[string]struct{},
	allowedSpecialKeys map[string]struct{},
) error {
	stored := make(map[string]schema.Field, len(fields))
	for _, field := range fields {
		if field.Category != schema.FieldCategoryPresentation {
			stored[field.Name] = field
		}
	}
	for name := range values {
		if _, allowed := allowedSpecialKeys[name]; allowed {
			continue
		}
		if _, exists := stored[name]; !exists {
			path := strings.Join(append(append([]string(nil), ancestors...), name), ".")
			return fmt.Errorf("MongoDB value %q is not a stored field in collection %q", path, collectionID)
		}
	}

	for _, field := range fields {
		if field.Category == schema.FieldCategoryPresentation {
			continue
		}
		pathSegments := append(append([]string(nil), ancestors...), field.Name)
		path := strings.Join(pathSegments, ".")
		value, exists := values[field.Name]
		if !exists {
			if requireMissing && field.Required {
				return fmt.Errorf("MongoDB document is missing required field %q", path)
			}
			continue
		}
		if field.Localized {
			localized, valid := value.ObjectValue()
			if !valid {
				return fmt.Errorf("MongoDB localized field %q requires canonical locale-keyed storage", path)
			}
			locales := make([]string, 0, len(localized))
			for locale := range localized {
				locales = append(locales, locale)
			}
			sort.Strings(locales)
			for _, locale := range locales {
				if !schema.IsValidLocaleCode(locale) {
					return fmt.Errorf("MongoDB localized field %q contains invalid locale %q", path, locale)
				}
				if configuredLocales != nil {
					if _, configured := configuredLocales[locale]; !configured {
						return fmt.Errorf("MongoDB localized field %q contains unconfigured locale %q", path, locale)
					}
				}
				localizedValue := localized[locale]
				if localizedValue.Kind() == store.ValueNull {
					continue
				}
				unlocalized := field
				unlocalized.Localized = false
				if err := validateMongoFieldValue(
					collectionID,
					unlocalized,
					localizedValue,
					append(append([]string(nil), pathSegments...), locale),
					requireMissing,
					patchRoot,
					configuredLocales,
				); err != nil {
					return err
				}
			}
			if _, err := encodeValue(value); err != nil {
				return fmt.Errorf("MongoDB value %q: %w", path, err)
			}
			continue
		}
		if err := validateMongoFieldValue(collectionID, field, value, pathSegments, requireMissing, patchRoot, configuredLocales); err != nil {
			return err
		}
	}
	return nil
}

func validateMongoFieldValue(
	collectionID schema.StableID,
	field schema.Field,
	value store.Value,
	pathSegments []string,
	requireMissing bool,
	patchRoot bool,
	configuredLocales map[string]struct{},
) error {
	path := strings.Join(pathSegments, ".")
	if value.Kind() == store.ValueNull {
		if field.Required {
			if patchRoot {
				return fmt.Errorf("MongoDB patch cannot clear required field %q", path)
			}
			return fmt.Errorf("MongoDB document is missing required field %q", path)
		}
		return nil
	}

	if field.Type == schema.FieldTypeGroup {
		object, valid := value.ObjectValue()
		if !valid {
			return fmt.Errorf("MongoDB value %q does not match field type %q", path, field.Type)
		}
		if field.Nested == nil {
			return fmt.Errorf("MongoDB group field %q does not have a nested field contract", path)
		}
		// Complete documents require complete supplied groups. Direct store
		// patches remain partial at every supported group depth so Update can
		// atomically merge them with the stored canonical object.
		nestedRequireMissing := requireMissing
		if patchRoot {
			nestedRequireMissing = false
		}
		return validateMongoFieldValues(collectionID, field.Nested.Fields, object, pathSegments, nestedRequireMissing, patchRoot, configuredLocales, nil)
	}
	if field.Type == schema.FieldTypeSelect && field.Select != nil && field.Select.HasMany {
		return validateMongoHasManySelectValue(field, value, path)
	}
	if field.Type == schema.FieldTypeArray {
		return validateMongoArrayValue(collectionID, field, value, pathSegments, configuredLocales)
	}
	if field.Type == schema.FieldTypeBlocks {
		return validateMongoBlocksValue(collectionID, field, value, pathSegments, configuredLocales)
	}
	if field.Type == schema.FieldTypeRelationship {
		return validateMongoRelationshipValue(field, value, path)
	}
	if field.Type == schema.FieldTypeUpload {
		return validateMongoUploadValue(field, value, path)
	}
	if mongoUploadSizesField(field) {
		return validateMongoUploadSizes(value, path)
	}
	return validateMongoScalarFieldValue(field, value, path)
}

func validateMongoHasManySelectValue(field schema.Field, value store.Value, path string) error {
	items, valid := value.Values()
	if !valid || field.Select == nil || !field.Select.HasMany {
		return fmt.Errorf("MongoDB value %q does not match field type %q", path, field.Type)
	}
	if field.Required && len(items) == 0 {
		return fmt.Errorf("MongoDB document is missing required field %q", path)
	}
	allowed := make(map[string]struct{}, len(field.Select.Choices))
	for _, choice := range field.Select.Choices {
		allowed[choice.Value] = struct{}{}
	}
	seen := make(map[string]struct{}, len(items))
	for index, item := range items {
		text, valid := item.StringValue()
		if !valid {
			return fmt.Errorf("MongoDB value %q.%d does not match field type %q", path, index, field.Type)
		}
		if _, duplicate := seen[text]; duplicate {
			return fmt.Errorf("MongoDB value %q.%d duplicates select choice %q", path, index, text)
		}
		if _, exists := allowed[text]; !exists {
			return fmt.Errorf("MongoDB value %q.%d contains unknown select choice %q", path, index, text)
		}
		seen[text] = struct{}{}
	}
	if _, err := encodeValue(value); err != nil {
		return fmt.Errorf("MongoDB value %q: %w", path, err)
	}
	return nil
}

func validateMongoArrayValue(
	collectionID schema.StableID,
	field schema.Field,
	value store.Value,
	pathSegments []string,
	configuredLocales map[string]struct{},
) error {
	path := strings.Join(pathSegments, ".")
	items, valid := value.Values()
	if !valid || field.Nested == nil {
		return fmt.Errorf("MongoDB value %q does not match field type %q", path, field.Type)
	}
	if field.Required && len(items) == 0 {
		return fmt.Errorf("MongoDB document is missing required field %q", path)
	}
	if len(items) < field.Nested.MinRows {
		return fmt.Errorf("MongoDB value %q must contain at least %d rows", path, field.Nested.MinRows)
	}
	if field.Nested.MaxRows > 0 && len(items) > field.Nested.MaxRows {
		return fmt.Errorf("MongoDB value %q must contain at most %d rows", path, field.Nested.MaxRows)
	}
	seenKeys := make(map[string]int, len(items))
	for index, item := range items {
		itemPath := fmt.Sprintf("%s.%d", path, index)
		object, valid := item.ObjectValue()
		if !valid {
			return fmt.Errorf("MongoDB array row %q must be an object", itemPath)
		}
		if err := validateMongoRowKey(object, itemPath, index, seenKeys); err != nil {
			return err
		}
		if err := validateMongoFieldValues(
			collectionID, field.Nested.Fields, object,
			append(append([]string(nil), pathSegments...), fmt.Sprintf("%d", index)),
			true, false, configuredLocales, map[string]struct{}{"_key": {}},
		); err != nil {
			return err
		}
	}
	if _, err := encodeValue(value); err != nil {
		return fmt.Errorf("MongoDB value %q: %w", path, err)
	}
	return nil
}

func validateMongoBlocksValue(
	collectionID schema.StableID,
	field schema.Field,
	value store.Value,
	pathSegments []string,
	configuredLocales map[string]struct{},
) error {
	path := strings.Join(pathSegments, ".")
	items, valid := value.Values()
	if !valid || field.Blocks == nil {
		return fmt.Errorf("MongoDB value %q does not match field type %q", path, field.Type)
	}
	if field.Required && len(items) == 0 {
		return fmt.Errorf("MongoDB document is missing required field %q", path)
	}
	seenKeys := make(map[string]int, len(items))
	for index, item := range items {
		itemPath := fmt.Sprintf("%s.%d", path, index)
		object, valid := item.ObjectValue()
		if !valid {
			return fmt.Errorf("MongoDB block %q must be an object", itemPath)
		}
		blockType, valid := object["blockType"].StringValue()
		block, exists := mongoBlockType(field, blockType)
		if !valid || !exists {
			return fmt.Errorf("MongoDB block %q has an invalid blockType", itemPath)
		}
		if err := validateMongoRowKey(object, itemPath, index, seenKeys); err != nil {
			return err
		}
		if err := validateMongoFieldValues(
			collectionID, block.Fields, object,
			append(append([]string(nil), pathSegments...), fmt.Sprintf("%d", index)),
			true, false, configuredLocales,
			map[string]struct{}{"_key": {}, "blockType": {}},
		); err != nil {
			return err
		}
	}
	if _, err := encodeValue(value); err != nil {
		return fmt.Errorf("MongoDB value %q: %w", path, err)
	}
	return nil
}

func validateMongoRowKey(values store.Values, path string, index int, seen map[string]int) error {
	value, exists := values["_key"]
	if !exists {
		return nil
	}
	key, valid := value.StringValue()
	if !valid || strings.TrimSpace(key) == "" {
		return fmt.Errorf("MongoDB row key %q._key must be a non-empty string", path)
	}
	if first, duplicate := seen[key]; duplicate {
		return fmt.Errorf("MongoDB row key %q._key duplicates row %d", path, first)
	}
	seen[key] = index
	return nil
}

func mongoBlockType(field schema.Field, key string) (schema.BlockType, bool) {
	if field.Blocks == nil {
		return schema.BlockType{}, false
	}
	for _, block := range field.Blocks.Types {
		if block.Key == key {
			return block, true
		}
	}
	return schema.BlockType{}, false
}

func validateMongoScalarFieldValue(field schema.Field, value store.Value, path string) error {
	if field.Type == schema.FieldTypeJSON || field.Type == schema.FieldTypePlugin {
		if _, err := encodeValue(value); err != nil {
			return fmt.Errorf("MongoDB value %q does not contain persistable JSON data: %w", path, err)
		}
		return nil
	}
	valid := false
	text := ""
	switch field.Type {
	case schema.FieldTypeText, schema.FieldTypeCode, schema.FieldTypeTextarea, schema.FieldTypeEmail,
		schema.FieldTypeDate, schema.FieldTypeRadio, schema.FieldTypeSelect:
		text, valid = value.StringValue()
	case schema.FieldTypeNumber:
		_, valid = value.NumberValue()
	case schema.FieldTypeCheckbox:
		_, valid = value.BooleanValue()
	case schema.FieldTypePoint:
		coordinates, point := value.Values()
		if point && len(coordinates) == 2 {
			longitude, longitudeValid := coordinates[0].NumberValue()
			latitude, latitudeValid := coordinates[1].NumberValue()
			valid = longitudeValid && latitudeValid &&
				!math.IsNaN(longitude) && !math.IsInf(longitude, 0) && longitude >= -180 && longitude <= 180 &&
				!math.IsNaN(latitude) && !math.IsInf(latitude, 0) && latitude >= -90 && latitude <= 90
		}
	}
	if !valid {
		return fmt.Errorf("MongoDB value %q does not match field type %q", path, field.Type)
	}
	if (field.Type == schema.FieldTypeSelect || field.Type == schema.FieldTypeRadio) && field.Select != nil {
		allowed := text == "" && !field.Required
		for _, choice := range field.Select.Choices {
			allowed = allowed || choice.Value == text
		}
		if !allowed {
			return fmt.Errorf("MongoDB value %q contains unknown select choice %q", path, text)
		}
	}
	if _, err := encodeValue(value); err != nil {
		return fmt.Errorf("MongoDB value %q: %w", path, err)
	}
	return nil
}

var (
	_ store.Store         = (*Store)(nil)
	_ store.SnapshotStore = (*Store)(nil)
	_ store.Transaction   = (*documentTransaction)(nil)
)
