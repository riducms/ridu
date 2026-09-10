package mongodb

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const mongoIndexNameHashBytes = 8

const (
	mongoContentLifecycleIndexName         = "z_content_lifecycle"
	mongoContentPublicationIndexName       = "z_content_publication"
	mongoReferenceOwnerIndexName           = "z_reference_owner"
	mongoReferenceTargetIndexName          = "z_reference_target"
	mongoVersionOwnerIndexName             = "z_version_owner_revision"
	mongoPreferenceOwnerIndexName          = "z_preference_owner_key"
	mongoDocumentLockTargetIndexName       = "z_document_lock_target"
	mongoDocumentLockOwnerIndexName        = "z_document_lock_owner"
	mongoTaskDueIndexName                  = "z_task_due"
	mongoTaskLeaseIndexName                = "z_task_lease"
	mongoTaskTargetIndexName               = "z_task_target"
	mongoTaskRequesterIndexName            = "z_task_requester"
	mongoTaskRetentionIndexName            = "z_task_retention"
	mongoTaskConcurrencyIdentityIndexName  = "z_task_concurrency_identity"
	mongoTaskConcurrencyTargetIndexName    = "z_task_concurrency_target"
	mongoTaskConcurrencyRequesterIndexName = "z_task_concurrency_requester"
	mongoAuthCredentialOwnerIndexName      = "z_auth_credential_owner"
	mongoAuthSessionPublicIDIndexName      = "z_auth_session_public_id"
	mongoAuthSessionOwnerIndexName         = "z_auth_session_owner"
	mongoAuthSessionExpiryIndexName        = "z_auth_session_expiry"
	mongoAuthTokenHashIndexName            = "z_auth_token_hash"
	mongoAuthTokenOwnerPurposeIndexName    = "z_auth_token_owner_purpose"
	mongoAuthAPIKeyTokenIndexName          = "z_auth_api_key_token"
	mongoAuthAPIKeyOwnerIndexName          = "z_auth_api_key_owner"
	mongoAuthAPIKeyExpiryIndexName         = "z_auth_api_key_expiry"
	mongoAuthRateLimitExpiryIndexName      = "z_auth_rate_limit_expiry"
)

// mongoIndexDefinition is the complete semantic shape of one adapter-owned
// content index. Index names use stable manifest identities while key paths
// use the current authored names, so a field rename is detected as physical
// drift instead of silently creating a second contract.
type mongoIndexDefinition struct {
	name          string
	identity      string
	keys          bson.D
	unique        bool
	partialFilter bson.D
}

type mongoCollectionIndexPlan struct {
	collection   schema.Collection
	physicalName string
	definitions  []mongoIndexDefinition
	fingerprint  string
	locales      []schema.LocaleCode
}

type mongoVerifiedIndexPlan struct {
	fingerprint string
	locales     []schema.LocaleCode
}

type mongoSystemIndexKind uint8

const (
	mongoSystemReferenceIndexes mongoSystemIndexKind = iota + 1
	mongoSystemVersionIndexes
	mongoSystemPreferenceIndexes
	mongoSystemDocumentLockIndexes
	mongoSystemTaskIndexes
	mongoSystemTaskConcurrencyIndexes
	mongoSystemAuthCredentialIndexes
	mongoSystemAuthSessionIndexes
	mongoSystemAuthTokenIndexes
	mongoSystemAuthAPIKeyIndexes
	mongoSystemAuthRateLimitIndexes
	mongoSystemAuthBootstrapIndexes
	mongoSystemUploadLockIndexes
)

type mongoSystemIndexPlan struct {
	kind         mongoSystemIndexKind
	collectionID schema.StableID
	description  string
	physicalName string
	definitions  []mongoIndexDefinition
}

// SyncIndexes is the explicit development/bootstrap entry point for the
// bounded MongoDB index slice. It creates missing declared indexes, never
// drops or rewrites an existing index, and does not install migration state.
// MongoDB index builds are not transactional, so the operation is deliberately
// resumable and verifies the complete physical shape before authorizing writes.
func (backend *Store) SyncIndexes(ctx context.Context, manifest schema.Manifest) error {
	if backend != nil {
		backend.indexLifecycleMu.Lock()
		defer backend.indexLifecycleMu.Unlock()
	}
	planSet, err := mongoPhysicalIndexPlans(manifest)
	if err != nil {
		if backend != nil {
			backend.clearVerifiedIndexes()
		}
		return err
	}
	plans, systemPlans := planSet.collections, planSet.system
	if err := backend.prepareIndexOperation(ctx); err != nil {
		return err
	}
	backend.clearVerifiedIndexes()

	// Refuse drift in every collection before starting any additive work. A
	// unique build can still fail on existing duplicate data after an earlier
	// collection succeeded; rerunning this method safely resumes that work.
	for _, plan := range plans {
		actual, err := backend.readCollectionIndexes(ctx, plan)
		if err != nil {
			return err
		}
		if err := compareMongoIndexSets(plan.collection, plan.definitions, actual, true); err != nil {
			return err
		}
	}
	for _, plan := range systemPlans {
		actual, err := backend.readNamedCollectionIndexes(ctx, plan.physicalName, plan.description)
		if err != nil {
			return err
		}
		if err := compareMongoNamedIndexSets(plan.description, plan.definitions, actual, true); err != nil {
			return err
		}
	}

	for _, plan := range plans {
		actual, err := backend.readCollectionIndexes(ctx, plan)
		if err != nil {
			return err
		}
		for _, definition := range plan.definitions {
			if _, exists := actual[definition.name]; exists {
				continue
			}
			if err := backend.createMongoIndex(ctx, plan.physicalName, fmt.Sprintf("collection %q", plan.collection.ID), definition); err != nil {
				return err
			}
		}
	}
	for _, plan := range systemPlans {
		actual, err := backend.readNamedCollectionIndexes(ctx, plan.physicalName, plan.description)
		if err != nil {
			return err
		}
		for _, definition := range plan.definitions {
			if _, exists := actual[definition.name]; exists {
				continue
			}
			if err := backend.createMongoIndex(ctx, plan.physicalName, plan.description, definition); err != nil {
				return err
			}
		}
	}

	return backend.verifyIndexPlans(ctx, plans, systemPlans)
}

// VerifyIndexes is a non-mutating physical check for the complete current
// manifest plan. A successful verification authorizes this Store instance to
// serve every resource in that plan; Open itself remains free of schema
// mutations.
func (backend *Store) VerifyIndexes(ctx context.Context, manifest schema.Manifest) error {
	if ctx == nil {
		return fmt.Errorf("MongoDB index context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if backend != nil {
		if err := backend.lockMongoMigrationLifecycle(ctx); err != nil {
			return err
		}
		defer backend.indexLifecycleMu.Unlock()
	}
	planSet, err := mongoPhysicalIndexPlans(manifest)
	if err != nil {
		if backend != nil {
			backend.clearVerifiedIndexes()
		}
		return err
	}
	plans, systemPlans := planSet.collections, planSet.system
	if err := backend.prepareIndexOperation(ctx); err != nil {
		return err
	}
	backend.clearVerifiedIndexes()
	if err := backend.verifyMongoMigrationLedgerPhysicalContract(ctx); err != nil {
		return err
	}
	return backend.verifyIndexPlans(ctx, plans, systemPlans)
}

func (backend *Store) verifyIndexPlans(ctx context.Context, plans []mongoCollectionIndexPlan, systemPlans []mongoSystemIndexPlan) error {
	return backend.verifyIndexPlansWithAuthorization(ctx, plans, systemPlans, true)
}

func (backend *Store) verifyIndexPlansWithoutAuthorization(ctx context.Context, plans []mongoCollectionIndexPlan, systemPlans []mongoSystemIndexPlan) error {
	return backend.verifyIndexPlansWithAuthorization(ctx, plans, systemPlans, false)
}

func (backend *Store) verifyIndexPlansWithAuthorization(ctx context.Context, plans []mongoCollectionIndexPlan, systemPlans []mongoSystemIndexPlan, authorize bool) error {
	verified := make(map[schema.StableID]mongoVerifiedIndexPlan, len(plans))
	verifiedVersions := make(map[schema.StableID]bool)
	verifiedReferences := false
	verifiedPreferences := false
	verifiedDocumentLocks := false
	verifiedTaskDocuments := false
	verifiedTaskConcurrency := false
	verifiedAuthCredentials := false
	verifiedAuthSessions := false
	verifiedAuthTokens := false
	verifiedAuthAPIKeys := false
	verifiedAuthRateLimits := false
	verifiedAuthBootstrap := false
	verifiedUploadLocks := false
	for _, plan := range plans {
		actual, err := backend.readCollectionIndexes(ctx, plan)
		if err != nil {
			return err
		}
		if err := compareMongoIndexSets(plan.collection, plan.definitions, actual, false); err != nil {
			return err
		}
		verified[plan.collection.ID] = mongoVerifiedIndexPlan{
			fingerprint: plan.fingerprint,
			locales:     append([]schema.LocaleCode(nil), plan.locales...),
		}
	}
	for _, plan := range systemPlans {
		actual, err := backend.readNamedCollectionIndexes(ctx, plan.physicalName, plan.description)
		if err != nil {
			return err
		}
		if err := compareMongoNamedIndexSets(plan.description, plan.definitions, actual, false); err != nil {
			return err
		}
		switch plan.kind {
		case mongoSystemReferenceIndexes:
			verifiedReferences = true
		case mongoSystemVersionIndexes:
			verifiedVersions[plan.collectionID] = true
		case mongoSystemPreferenceIndexes:
			verifiedPreferences = true
		case mongoSystemDocumentLockIndexes:
			verifiedDocumentLocks = true
		case mongoSystemTaskIndexes:
			verifiedTaskDocuments = true
		case mongoSystemTaskConcurrencyIndexes:
			verifiedTaskConcurrency = true
		case mongoSystemAuthCredentialIndexes:
			verifiedAuthCredentials = true
		case mongoSystemAuthSessionIndexes:
			verifiedAuthSessions = true
		case mongoSystemAuthTokenIndexes:
			verifiedAuthTokens = true
		case mongoSystemAuthAPIKeyIndexes:
			verifiedAuthAPIKeys = true
		case mongoSystemAuthRateLimitIndexes:
			verifiedAuthRateLimits = true
		case mongoSystemAuthBootstrapIndexes:
			verifiedAuthBootstrap = true
		case mongoSystemUploadLockIndexes:
			verifiedUploadLocks = true
		default:
			return fmt.Errorf("unknown MongoDB system index plan")
		}
	}
	if !authorize {
		// Retired derived namespaces are useful migration/status diagnostics, but
		// they do not make current application traffic unsafe. Ordinary serving
		// verification therefore does not require their absence.
		if err := backend.verifyUnversionedVersionNamespacesAbsent(ctx, plans); err != nil {
			return err
		}
		if err := backend.verifyUnclaimedReferenceNamespaceAbsent(ctx, systemPlans); err != nil {
			return err
		}
		return nil
	}
	backend.indexesMu.Lock()
	backend.verifiedIndexes = verified
	backend.verifiedVersionIndexes = verifiedVersions
	backend.verifiedReferenceIndexes = verifiedReferences
	backend.verifiedPreferenceIndexes = verifiedPreferences
	backend.verifiedDocumentLockIndexes = verifiedDocumentLocks
	backend.verifiedTaskIndexes = verifiedTaskDocuments && verifiedTaskConcurrency
	backend.verifiedAuthIndexes = verifiedAuthCredentials && verifiedAuthSessions && verifiedAuthTokens &&
		verifiedAuthAPIKeys && verifiedAuthRateLimits && verifiedAuthBootstrap
	backend.verifiedUploadLockIndexes = verifiedUploadLocks
	backend.indexesMu.Unlock()
	return nil
}

func (backend *Store) createMongoIndex(ctx context.Context, physicalName, description string, definition mongoIndexDefinition) error {
	indexOptions := options.Index().SetName(definition.name)
	if definition.unique {
		indexOptions.SetUnique(true)
	}
	if len(definition.partialFilter) != 0 {
		indexOptions.SetPartialFilterExpression(definition.partialFilter)
	}
	_, err := backend.database.Collection(physicalName).Indexes().CreateOne(
		ctx,
		mongo.IndexModel{Keys: definition.keys, Options: indexOptions},
	)
	if err == nil {
		return nil
	}
	translated := translateMongoError(ctx, err)
	if definition.unique && errors.Is(translated, store.ErrConflict) {
		return fmt.Errorf("build MongoDB unique indexes for %s: existing documents violate the required uniqueness contract: %w", description, store.ErrConflict)
	}
	return fmt.Errorf("create MongoDB index for %s: %w", description, translated)
}

func (backend *Store) prepareIndexOperation(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("MongoDB index context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if backend == nil || backend.database == nil || backend.client == nil {
		return fmt.Errorf("MongoDB database is unavailable")
	}
	backend.closeMu.Lock()
	closed := backend.closed
	backend.closeMu.Unlock()
	if closed {
		return fmt.Errorf("MongoDB database is closed")
	}
	return nil
}

func (backend *Store) verifyUnversionedVersionNamespacesAbsent(
	ctx context.Context,
	plans []mongoCollectionIndexPlan,
) error {
	for _, plan := range plans {
		if plan.collection.Versions != nil {
			continue
		}
		kind := "collection"
		if plan.collection.Capabilities.Global {
			kind = "global"
		}
		physicalName := physicalVersionCollectionName(plan.collection.ID)
		names, err := backend.database.ListCollectionNames(ctx, bson.D{{Key: "name", Value: physicalName}})
		if err != nil {
			return fmt.Errorf(
				"inspect MongoDB version namespace for %s %q: %w",
				kind, plan.collection.ID, translateMongoError(ctx, err),
			)
		}
		if len(names) != 0 {
			return fmt.Errorf(
				"MongoDB physical namespace drift for %s %q: version state exists while versions are disabled",
				kind, plan.collection.ID,
			)
		}
	}
	return nil
}

func (backend *Store) verifyUnclaimedReferenceNamespaceAbsent(
	ctx context.Context,
	systemPlans []mongoSystemIndexPlan,
) error {
	for _, plan := range systemPlans {
		if plan.kind == mongoSystemReferenceIndexes {
			return nil
		}
	}
	names, err := backend.database.ListCollectionNames(
		ctx,
		bson.D{{Key: "name", Value: mongoReferenceCollectionName}},
	)
	if err != nil {
		return fmt.Errorf("inspect MongoDB reference namespace: %w", translateMongoError(ctx, err))
	}
	if len(names) != 0 {
		return fmt.Errorf(
			"MongoDB physical namespace drift for reference state: namespace exists while no relationship or upload field requires it",
		)
	}
	return nil
}

func (backend *Store) clearVerifiedIndexes() {
	backend.indexesMu.Lock()
	backend.verifiedIndexes = make(map[schema.StableID]mongoVerifiedIndexPlan)
	backend.verifiedVersionIndexes = make(map[schema.StableID]bool)
	backend.verifiedReferenceIndexes = false
	backend.verifiedPreferenceIndexes = false
	backend.verifiedDocumentLockIndexes = false
	backend.verifiedTaskIndexes = false
	backend.verifiedAuthIndexes = false
	backend.verifiedUploadLockIndexes = false
	backend.indexesMu.Unlock()
}

func (backend *Store) requireVerifiedIndexes(collection schema.Collection) error {
	_, err := backend.verifiedIndexPlan(collection)
	return err
}

func (backend *Store) requireVerifiedIndexesForLocales(collection schema.Collection, locales []schema.LocaleCode) error {
	verified, err := backend.verifiedIndexPlan(collection)
	if err != nil {
		return err
	}
	if !mongoLocaleListsEqual(locales, verified.locales) {
		return fmt.Errorf("MongoDB locale configuration for collection %q does not match the verified manifest; call VerifyIndexes or SyncIndexes before serving this manifest", collection.ID)
	}
	return nil
}

func (backend *Store) verifiedIndexPlan(collection schema.Collection) (mongoVerifiedIndexPlan, error) {
	backend.indexesMu.RLock()
	verified, exists := backend.verifiedIndexes[collection.ID]
	backend.indexesMu.RUnlock()
	if !exists {
		return mongoVerifiedIndexPlan{}, fmt.Errorf("MongoDB indexes for collection %q are not verified; call VerifyIndexes or SyncIndexes before serving this manifest", collection.ID)
	}
	definitions, err := mongoContentIndexesForLocales(collection, verified.locales)
	if err != nil {
		return mongoVerifiedIndexPlan{}, err
	}
	fingerprint, err := mongoIndexFingerprint(definitions)
	if err != nil {
		return mongoVerifiedIndexPlan{}, err
	}
	if verified.fingerprint != fingerprint {
		return mongoVerifiedIndexPlan{}, fmt.Errorf("MongoDB indexes for collection %q are not verified; call VerifyIndexes or SyncIndexes before serving this manifest", collection.ID)
	}
	return verified, nil
}

func mongoLocaleListsEqual(left, right []schema.LocaleCode) bool {
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

func (backend *Store) requireVerifiedResourceID(id schema.StableID) error {
	_, err := backend.requireVerifiedResourcePlan(id)
	return err
}

func (backend *Store) requireVerifiedResourcePlan(id schema.StableID) (bool, error) {
	backend.indexesMu.RLock()
	_, verified := backend.verifiedIndexes[id]
	versioned := backend.verifiedVersionIndexes[id]
	backend.indexesMu.RUnlock()
	if !verified {
		return false, fmt.Errorf("MongoDB physical plan for resource %q is not verified; call VerifyIndexes or SyncIndexes before serving this manifest", id)
	}
	return versioned, nil
}

func (backend *Store) requireVerifiedDocumentStatePlan(id schema.StableID) (bool, bool, error) {
	backend.indexesMu.RLock()
	_, verified := backend.verifiedIndexes[id]
	versioned := backend.verifiedVersionIndexes[id]
	references := backend.verifiedReferenceIndexes
	backend.indexesMu.RUnlock()
	if !verified {
		return false, false, fmt.Errorf("MongoDB physical plan for resource %q is not verified; call VerifyIndexes or SyncIndexes before serving this manifest", id)
	}
	return versioned, references, nil
}

func (backend *Store) requireVerifiedVersionIndexes(collection schema.Collection) error {
	if collection.Versions == nil {
		return nil
	}
	backend.indexesMu.RLock()
	verified := backend.verifiedVersionIndexes[collection.ID]
	backend.indexesMu.RUnlock()
	if !verified {
		return fmt.Errorf("MongoDB version indexes for collection %q are not verified; call VerifyIndexes or SyncIndexes before serving this manifest", collection.ID)
	}
	return nil
}

func (backend *Store) requireVerifiedReferenceIndexes(collection schema.Collection) error {
	if !mongoCollectionHasRelationships(collection) {
		return nil
	}
	backend.indexesMu.RLock()
	verified := backend.verifiedReferenceIndexes
	backend.indexesMu.RUnlock()
	if !verified {
		return fmt.Errorf("MongoDB reference indexes are not verified; call VerifyIndexes or SyncIndexes before serving this manifest")
	}
	return nil
}

func (backend *Store) requireVerifiedPreferenceIndexes() error {
	backend.indexesMu.RLock()
	verified := backend.verifiedPreferenceIndexes
	backend.indexesMu.RUnlock()
	if !verified {
		return fmt.Errorf("MongoDB preference indexes are not verified; call VerifyIndexes or SyncIndexes before serving this manifest")
	}
	return nil
}

func (backend *Store) requireVerifiedDocumentLockIndexes() error {
	backend.indexesMu.RLock()
	verified := backend.verifiedDocumentLockIndexes
	backend.indexesMu.RUnlock()
	if !verified {
		return fmt.Errorf("MongoDB document-lock indexes are not verified; call VerifyIndexes or SyncIndexes before serving this manifest")
	}
	return nil
}

func (backend *Store) requireVerifiedTaskIndexes() error {
	backend.indexesMu.RLock()
	verified := backend.verifiedTaskIndexes
	backend.indexesMu.RUnlock()
	if !verified {
		return fmt.Errorf("MongoDB task indexes are not verified; call VerifyIndexes or SyncIndexes before serving this manifest")
	}
	return nil
}

func (backend *Store) requireVerifiedAuthIndexes() error {
	backend.indexesMu.RLock()
	verified := backend.verifiedAuthIndexes
	backend.indexesMu.RUnlock()
	if !verified {
		return fmt.Errorf("MongoDB authentication indexes are not verified; call VerifyIndexes or SyncIndexes before serving this manifest")
	}
	return nil
}

func (backend *Store) requireVerifiedUploadLockIndexes() error {
	backend.indexesMu.RLock()
	verified := backend.verifiedUploadLockIndexes
	backend.indexesMu.RUnlock()
	if !verified {
		return fmt.Errorf("MongoDB upload-lock indexes are not verified; call VerifyIndexes or SyncIndexes before serving this manifest")
	}
	return nil
}

func mongoIndexPlans(manifest schema.Manifest) ([]mongoCollectionIndexPlan, error) {
	snapshot := manifest.Snapshot()
	var locales []schema.LocaleCode
	if snapshot.Application.Localization != nil {
		locales = snapshot.Application.Localization.LocaleCodes()
		if _, err := mongoConfiguredLocales(locales); err != nil {
			return nil, fmt.Errorf("plan MongoDB indexes: %w", err)
		}
	}
	type resourceCandidate struct {
		kind       string
		collection schema.Collection
	}
	resources := make([]resourceCandidate, 0, len(snapshot.Collections)+len(snapshot.Globals))
	for _, collection := range snapshot.Collections {
		resources = append(resources, resourceCandidate{kind: "collection", collection: collection})
	}
	for _, global := range snapshot.Globals {
		resources = append(resources, resourceCandidate{kind: "global", collection: global})
	}
	sort.Slice(resources, func(left, right int) bool {
		if resources[left].collection.ID != resources[right].collection.ID {
			return resources[left].collection.ID < resources[right].collection.ID
		}
		if resources[left].kind != resources[right].kind {
			return resources[left].kind < resources[right].kind
		}
		return resources[left].collection.Slug < resources[right].collection.Slug
	})
	for index := 1; index < len(resources); index++ {
		previous, current := resources[index-1], resources[index]
		if previous.collection.ID == current.collection.ID {
			return nil, fmt.Errorf(
				"MongoDB %s %q and %s %q share stable resource ID %q",
				previous.kind, previous.collection.Slug, current.kind, current.collection.Slug, current.collection.ID,
			)
		}
	}
	plans := make([]mongoCollectionIndexPlan, len(resources))
	for index, candidate := range resources {
		collection := candidate.collection
		if err := validateCollectionEnvelope(collection); err != nil {
			return nil, fmt.Errorf("plan MongoDB indexes for collection %q: %w", collection.ID, err)
		}
		definitions, err := mongoContentIndexesForLocales(collection, locales)
		if err != nil {
			return nil, fmt.Errorf("plan MongoDB indexes for collection %q: %w", collection.ID, err)
		}
		fingerprint, err := mongoIndexFingerprint(definitions)
		if err != nil {
			return nil, fmt.Errorf("fingerprint MongoDB indexes for collection %q: %w", collection.ID, err)
		}
		plans[index] = mongoCollectionIndexPlan{
			collection: collection, physicalName: physicalCollectionName(collection.ID),
			definitions: definitions, fingerprint: fingerprint,
			locales: append([]schema.LocaleCode(nil), locales...),
		}
	}
	return plans, nil
}

func mongoSystemIndexPlans(collectionPlans []mongoCollectionIndexPlan) []mongoSystemIndexPlan {
	plans := []mongoSystemIndexPlan{
		{
			kind: mongoSystemUploadLockIndexes, description: "upload-object lock state",
			physicalName: mongoUploadLockCollectionName,
			definitions:  []mongoIndexDefinition{},
		},
		{
			kind: mongoSystemAuthCredentialIndexes, description: "authentication credential state",
			physicalName: mongoAuthCredentialCollectionName,
			definitions: []mongoIndexDefinition{{
				name:   mongoAuthCredentialOwnerIndexName,
				keys:   bson.D{{Key: "collection", Value: mongoAscendingDirection}, {Key: "user", Value: mongoAscendingDirection}},
				unique: true,
			}},
		},
		{
			kind: mongoSystemAuthSessionIndexes, description: "authentication session state",
			physicalName: mongoAuthSessionCollectionName,
			definitions: []mongoIndexDefinition{
				{name: mongoAuthSessionPublicIDIndexName, keys: bson.D{{Key: "id", Value: mongoAscendingDirection}}, unique: true},
				{name: mongoAuthSessionOwnerIndexName, keys: bson.D{{Key: "collection", Value: mongoAscendingDirection}, {Key: "user", Value: mongoAscendingDirection}, {Key: "lastSeenAt", Value: int32(-1)}, {Key: "createdAt", Value: int32(-1)}, {Key: "_id", Value: mongoAscendingDirection}}},
				{name: mongoAuthSessionExpiryIndexName, keys: bson.D{{Key: "expiresAt", Value: mongoAscendingDirection}, {Key: "_id", Value: mongoAscendingDirection}}},
			},
		},
		{
			kind: mongoSystemAuthTokenIndexes, description: "authentication recovery-token state",
			physicalName: mongoAuthTokenCollectionName,
			definitions: []mongoIndexDefinition{
				{name: mongoAuthTokenHashIndexName, keys: bson.D{{Key: "tokenHash", Value: mongoAscendingDirection}}, unique: true},
				{name: mongoAuthTokenOwnerPurposeIndexName, keys: bson.D{{Key: "collection", Value: mongoAscendingDirection}, {Key: "user", Value: mongoAscendingDirection}, {Key: "purpose", Value: mongoAscendingDirection}}, unique: true},
			},
		},
		{
			kind: mongoSystemAuthAPIKeyIndexes, description: "authentication API-key state",
			physicalName: mongoAuthAPIKeyCollectionName,
			definitions: []mongoIndexDefinition{
				{name: mongoAuthAPIKeyTokenIndexName, keys: bson.D{{Key: "tokenHash", Value: mongoAscendingDirection}}, unique: true},
				{name: mongoAuthAPIKeyOwnerIndexName, keys: bson.D{{Key: "collection", Value: mongoAscendingDirection}, {Key: "user", Value: mongoAscendingDirection}, {Key: "createdAt", Value: int32(-1)}, {Key: "_id", Value: mongoAscendingDirection}}},
				{name: mongoAuthAPIKeyExpiryIndexName, keys: bson.D{{Key: "expiresAt", Value: mongoAscendingDirection}, {Key: "_id", Value: mongoAscendingDirection}}, partialFilter: bson.D{{Key: "expiresAt", Value: bson.D{{Key: "$type", Value: "long"}}}}},
			},
		},
		{
			kind: mongoSystemAuthRateLimitIndexes, description: "authentication rate-limit state",
			physicalName: mongoAuthRateLimitCollectionName,
			definitions: []mongoIndexDefinition{
				{name: mongoAuthRateLimitExpiryIndexName, keys: bson.D{{Key: "expiresAt", Value: mongoAscendingDirection}, {Key: "_id", Value: mongoAscendingDirection}}},
			},
		},
		{
			kind: mongoSystemAuthBootstrapIndexes, description: "authentication bootstrap state",
			physicalName: mongoAuthBootstrapCollectionName,
			definitions:  []mongoIndexDefinition{},
		},
		{
			kind:         mongoSystemPreferenceIndexes,
			description:  "preference state",
			physicalName: mongoPreferenceCollectionName,
			definitions: []mongoIndexDefinition{{
				name: mongoPreferenceOwnerIndexName,
				keys: bson.D{
					{Key: "collection", Value: mongoAscendingDirection},
					{Key: "user", Value: mongoAscendingDirection},
					{Key: "key", Value: mongoAscendingDirection},
				},
				unique: true,
			}},
		},
		{
			kind:         mongoSystemDocumentLockIndexes,
			description:  "document-lock state",
			physicalName: mongoDocumentLockCollectionName,
			definitions: []mongoIndexDefinition{
				{
					name: mongoDocumentLockTargetIndexName,
					keys: bson.D{
						{Key: "collection", Value: mongoAscendingDirection},
						{Key: "document", Value: mongoAscendingDirection},
					},
					unique: true,
				},
				{
					name: mongoDocumentLockOwnerIndexName,
					keys: bson.D{
						{Key: "ownerCollection", Value: mongoAscendingDirection},
						{Key: "owner", Value: mongoAscendingDirection},
					},
				},
			},
		},
		{
			kind:         mongoSystemTaskIndexes,
			description:  "task state",
			physicalName: mongoTaskCollectionName,
			definitions: []mongoIndexDefinition{
				{
					name: mongoTaskDueIndexName,
					keys: bson.D{
						{Key: "state", Value: mongoAscendingDirection},
						{Key: "runAt", Value: mongoAscendingDirection},
						{Key: "createdAt", Value: mongoAscendingDirection},
						{Key: "_id", Value: mongoAscendingDirection},
					},
				},
				{
					name: mongoTaskLeaseIndexName,
					keys: bson.D{
						{Key: "state", Value: mongoAscendingDirection},
						{Key: "leaseExpiresAt", Value: mongoAscendingDirection},
						{Key: "runAt", Value: mongoAscendingDirection},
						{Key: "createdAt", Value: mongoAscendingDirection},
						{Key: "_id", Value: mongoAscendingDirection},
					},
					partialFilter: bson.D{{Key: "state", Value: string(store.TaskStateRunning)}},
				},
				{
					name: mongoTaskTargetIndexName,
					keys: bson.D{
						{Key: "target.collection", Value: mongoAscendingDirection},
						{Key: "target.document", Value: mongoAscendingDirection},
						{Key: "slug", Value: mongoAscendingDirection},
						{Key: "state", Value: mongoAscendingDirection},
						{Key: "runAt", Value: mongoAscendingDirection},
						{Key: "createdAt", Value: mongoAscendingDirection},
						{Key: "_id", Value: mongoAscendingDirection},
					},
					partialFilter: bson.D{{Key: "target.collection", Value: bson.D{{Key: "$type", Value: "string"}}}},
				},
				{
					name: mongoTaskRequesterIndexName,
					keys: bson.D{
						{Key: "requestedBy.collection", Value: mongoAscendingDirection},
						{Key: "requestedBy.document", Value: mongoAscendingDirection},
					},
					partialFilter: bson.D{{Key: "requestedBy.collection", Value: bson.D{{Key: "$type", Value: "string"}}}},
				},
				{
					name: mongoTaskRetentionIndexName,
					keys: bson.D{
						{Key: "retainUntil", Value: mongoAscendingDirection},
						{Key: "_id", Value: mongoAscendingDirection},
					},
					partialFilter: bson.D{{Key: "retainUntil", Value: bson.D{{Key: "$type", Value: "long"}}}},
				},
			},
		},
		{
			kind:         mongoSystemTaskConcurrencyIndexes,
			description:  "task concurrency state",
			physicalName: mongoTaskConcurrencyCollectionName,
			definitions: []mongoIndexDefinition{
				{
					name: mongoTaskConcurrencyIdentityIndexName,
					keys: bson.D{
						{Key: "queue", Value: mongoAscendingDirection},
						{Key: "key", Value: mongoAscendingDirection},
					},
					unique: true,
				},
				{
					name: mongoTaskConcurrencyTargetIndexName,
					keys: bson.D{
						{Key: "target.collection", Value: mongoAscendingDirection},
						{Key: "target.document", Value: mongoAscendingDirection},
					},
					partialFilter: bson.D{{Key: "target.collection", Value: bson.D{{Key: "$type", Value: "string"}}}},
				},
				{
					name: mongoTaskConcurrencyRequesterIndexName,
					keys: bson.D{
						{Key: "requestedBy.collection", Value: mongoAscendingDirection},
						{Key: "requestedBy.document", Value: mongoAscendingDirection},
					},
					partialFilter: bson.D{{Key: "requestedBy.collection", Value: bson.D{{Key: "$type", Value: "string"}}}},
				},
			},
		},
	}
	referencesRequired := false
	for _, collectionPlan := range collectionPlans {
		collection := collectionPlan.collection
		if mongoCollectionHasRelationships(collection) {
			referencesRequired = true
		}
		if collection.Versions != nil {
			plans = append(plans, mongoSystemIndexPlan{
				kind:         mongoSystemVersionIndexes,
				collectionID: collection.ID,
				description:  fmt.Sprintf("version state for collection %q", collection.ID),
				physicalName: physicalVersionCollectionName(collection.ID),
				definitions: []mongoIndexDefinition{{
					name: mongoVersionOwnerIndexName,
					keys: bson.D{
						{Key: mongoVersionOwnerPath, Value: mongoAscendingDirection},
						{Key: mongoVersionRevisionPath, Value: int32(-1)},
					},
					unique: true,
				}},
			})
		}
	}
	if referencesRequired {
		plans = append(plans, mongoSystemIndexPlan{
			kind:         mongoSystemReferenceIndexes,
			description:  "reference state",
			physicalName: mongoReferenceCollectionName,
			definitions: []mongoIndexDefinition{
				{
					name: mongoReferenceOwnerIndexName,
					keys: bson.D{
						{Key: "ownerCollection", Value: mongoAscendingDirection},
						{Key: "ownerDocument", Value: mongoAscendingDirection},
					},
				},
				{
					name: mongoReferenceTargetIndexName,
					keys: bson.D{
						{Key: "targetCollection", Value: mongoAscendingDirection},
						{Key: "targetDocument", Value: mongoAscendingDirection},
						{Key: "ownerCollection", Value: mongoAscendingDirection},
						{Key: "ownerDocument", Value: mongoAscendingDirection},
						{Key: "field", Value: mongoAscendingDirection},
						{Key: "locale", Value: mongoAscendingDirection},
						{Key: "occurrence", Value: mongoAscendingDirection},
					},
				},
			},
		})
	}
	sort.Slice(plans, func(left, right int) bool {
		return plans[left].physicalName < plans[right].physicalName
	})
	return plans
}

func mongoCollectionHasRelationships(collection schema.Collection) bool {
	var visit func([]schema.Field) bool
	visit = func(fields []schema.Field) bool {
		for _, field := range fields {
			if field.Relationship != nil || field.Upload != nil {
				return true
			}
			if visit(schema.ChildFields(field)) {
				return true
			}
		}
		return false
	}
	return visit(collection.Fields)
}

// mongoContentIndexes composes the narrowly required adapter metadata indexes
// with authored declarations. Exact-ID and optimistic-revision operations are
// already bounded by MongoDB's native unique _id_ index, so another revision
// index would only duplicate that lookup. Globals are singleton ID lookups and
// likewise need no collection-scan metadata indexes.
func mongoContentIndexes(collection schema.Collection) ([]mongoIndexDefinition, error) {
	return mongoContentIndexesForLocales(collection, nil)
}

func mongoContentIndexesForLocales(collection schema.Collection, locales []schema.LocaleCode) ([]mongoIndexDefinition, error) {
	definitions, err := mongoDeclaredIndexesForLocales(collection, locales)
	if err != nil {
		return nil, err
	}
	if collection.Capabilities.Global {
		return definitions, nil
	}
	if collection.Capabilities.Trash {
		definitions = append(definitions, mongoIndexDefinition{
			name: mongoContentLifecycleIndexName, identity: "metadata:lifecycle:" + string(collection.ID),
			keys: bson.D{
				{Key: mongoDeletedAtPath, Value: mongoAscendingDirection},
				{Key: mongoIDPath, Value: mongoAscendingDirection},
			},
		})
	}
	if collection.Versions != nil {
		definitions = append(definitions, mongoIndexDefinition{
			name: mongoContentPublicationIndexName, identity: "metadata:publication:" + string(collection.ID),
			keys: bson.D{
				{Key: mongoStatusPath, Value: mongoAscendingDirection},
				{Key: mongoDeletedAtPath, Value: mongoAscendingDirection},
				{Key: mongoIDPath, Value: mongoAscendingDirection},
			},
		})
	}
	sort.Slice(definitions, func(left, right int) bool {
		return definitions[left].name < definitions[right].name
	})
	return definitions, nil
}

func mongoDeclaredIndexes(collection schema.Collection) ([]mongoIndexDefinition, error) {
	return mongoDeclaredIndexesForLocales(collection, nil)
}

func mongoDeclaredIndexesForLocales(collection schema.Collection, locales []schema.LocaleCode) ([]mongoIndexDefinition, error) {
	definitions := make([]mongoIndexDefinition, 0)
	seenNames := make(map[string]string)
	appendDefinition := func(declaration string, paths []mongoPredicatePath, unique bool) error {
		keyCount := len(paths)
		if !unique {
			keyCount++
		}
		if keyCount > 32 {
			return fmt.Errorf("MongoDB index %q requires %d keys after the stable ID tiebreaker; MongoDB supports at most 32", declaration, keyCount)
		}
		name := mongoDeclaredIndexName(collection.ID, declaration, unique)
		if previous, collision := seenNames[name]; collision {
			return fmt.Errorf("deterministic MongoDB index name collision %q between %q and %q", name, previous, declaration)
		}
		seenNames[name] = declaration
		kind := "index"
		if unique {
			kind = "unique"
		}
		keys := make(bson.D, 0, len(paths)+1)
		for _, path := range paths {
			keys = append(keys, bson.E{Key: path.storagePath, Value: mongoAscendingDirection})
		}
		if !unique {
			keys = append(keys, bson.E{Key: mongoIDPath, Value: mongoAscendingDirection})
		}
		partialFilter := bson.D{}
		if unique {
			partialFilter = mongoUniqueIndexFilter(collection, paths)
		}
		definitions = append(definitions, mongoIndexDefinition{
			name: name, identity: "declared:" + kind + ":" + string(collection.ID) + ":" + declaration,
			keys: keys, unique: unique, partialFilter: partialFilter,
		})
		return nil
	}

	configuredLocales := func() ([]schema.LocaleCode, error) {
		if len(locales) == 0 {
			return nil, fmt.Errorf("localized MongoDB index declarations require application localization settings")
		}
		if _, err := mongoConfiguredLocales(locales); err != nil {
			return nil, err
		}
		return locales, nil
	}
	concretePath := func(resolved mongoPredicatePath) (mongoPredicatePath, error) {
		if len(resolved.localePaths) == 0 {
			return resolved, nil
		}
		if len(resolved.localePaths) != 1 {
			return mongoPredicatePath{}, fmt.Errorf("localized MongoDB index path must resolve exactly one locale")
		}
		resolved.storagePath = resolved.localePaths[0]
		resolved.localePaths = nil
		return resolved, nil
	}

	var visit func([]schema.Field, []schema.StableID) error
	visit = func(fields []schema.Field, ancestors []schema.StableID) error {
		for _, field := range fields {
			if field.Category == schema.FieldCategoryPresentation {
				continue
			}
			identity := append(append([]schema.StableID(nil), ancestors...), field.ID)
			if field.Index || field.Unique {
				indexLocales := []schema.LocaleCode{""}
				if field.Localized {
					var err error
					indexLocales, err = configuredLocales()
					if err != nil {
						return fmt.Errorf("field %q: %w", field.Path.String(), err)
					}
				}
				for _, locale := range indexLocales {
					scope := mongoPredicateScope{}
					declaration := "field:" + mongoStableIDChain(identity)
					if locale != "" {
						scope.localeChain = []schema.LocaleCode{locale}
						declaration += ":locale:" + string(locale)
					}
					resolved, err := resolveMongoPredicatePath(collection, field.Path, "index", scope)
					if err != nil {
						return err
					}
					resolved, err = concretePath(resolved)
					if err != nil {
						return err
					}
					if err := appendDefinition(declaration, []mongoPredicatePath{resolved}, field.Unique); err != nil {
						return err
					}
				}
			}
			if field.Type == schema.FieldTypeGroup && field.Nested != nil {
				if err := visit(field.Nested.ResolvedFields(), identity); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := visit(collection.Fields, nil); err != nil {
		return nil, err
	}

	for index, declared := range collection.Indexes {
		if len(declared.Fields) < 2 || len(declared.Fields) > 32 {
			return nil, fmt.Errorf("compound index %d must contain between 2 and 32 fields", index)
		}
		chains := make([][]schema.Field, len(declared.Fields))
		identities := make([]string, len(declared.Fields))
		seenPaths := make(map[string]struct{}, len(declared.Fields))
		for fieldIndex, path := range declared.Fields {
			if _, duplicate := seenPaths[path.String()]; duplicate {
				return nil, fmt.Errorf("compound index %d repeats field %q", index, path.String())
			}
			seenPaths[path.String()] = struct{}{}
			chain, found := mongoIndexFieldChain(collection.Fields, path.Segments())
			if !found {
				return nil, fmt.Errorf("compound index %d field %d has no stable field identity", index, fieldIndex)
			}
			chains[fieldIndex] = chain
			ids := make([]schema.StableID, len(chain))
			for chainIndex := range chain {
				ids[chainIndex] = chain[chainIndex].ID
			}
			identities[fieldIndex] = mongoStableIDChain(ids)
		}
		indexLocales := []schema.LocaleCode{""}
		for _, chain := range chains {
			if chain[len(chain)-1].Localized {
				var err error
				indexLocales, err = configuredLocales()
				if err != nil {
					return nil, fmt.Errorf("compound index %d: %w", index, err)
				}
				break
			}
		}
		for _, locale := range indexLocales {
			paths := make([]mongoPredicatePath, len(declared.Fields))
			for fieldIndex, path := range declared.Fields {
				scope := mongoPredicateScope{}
				if locale != "" {
					scope.localeChain = []schema.LocaleCode{locale}
				}
				resolved, err := resolveMongoPredicatePath(collection, path, "compound index", scope)
				if err != nil {
					return nil, fmt.Errorf("compound index %d field %d: %w", index, fieldIndex, err)
				}
				paths[fieldIndex], err = concretePath(resolved)
				if err != nil {
					return nil, fmt.Errorf("compound index %d field %d: %w", index, fieldIndex, err)
				}
			}
			declaration := "compound:" + strings.Join(identities, "\x00")
			if locale != "" {
				declaration += ":locale:" + string(locale)
			}
			if err := appendDefinition(declaration, paths, declared.Unique); err != nil {
				return nil, err
			}
		}
	}

	sort.Slice(definitions, func(left, right int) bool {
		return definitions[left].name < definitions[right].name
	})
	return definitions, nil
}

func mongoIndexFieldChain(fields []schema.Field, segments []string) ([]schema.Field, bool) {
	if len(segments) == 0 {
		return nil, false
	}
	for _, field := range fields {
		if field.Name != segments[0] || field.Category == schema.FieldCategoryPresentation {
			continue
		}
		chain := []schema.Field{field}
		if len(segments) == 1 {
			return chain, true
		}
		if field.Type != schema.FieldTypeGroup || field.Nested == nil {
			return nil, false
		}
		children, found := mongoIndexFieldChain(field.Nested.ResolvedFields(), segments[1:])
		if !found {
			return nil, false
		}
		return append(chain, children...), true
	}
	return nil, false
}

func mongoStableIDChain(ids []schema.StableID) string {
	parts := make([]string, len(ids))
	for index, id := range ids {
		parts[index] = string(id)
	}
	return strings.Join(parts, "/")
}

func mongoDeclaredIndexName(collectionID schema.StableID, declaration string, unique bool) string {
	kind := "index"
	prefix := "z_i_"
	if unique {
		kind = "unique"
		prefix = "z_u_"
	}
	digest := sha256.Sum256([]byte("mongodb-" + kind + ":" + string(collectionID) + ":" + declaration))
	return prefix + hex.EncodeToString(digest[:mongoIndexNameHashBytes])
}

func mongoUniqueIndexFilter(collection schema.Collection, paths []mongoPredicatePath) bson.D {
	predicates := make([]bson.D, 0, len(paths)*2+1)
	seen := make(map[string]struct{})
	for _, path := range paths {
		for _, ancestor := range path.objectAncestors {
			key := ancestor + "\x00object"
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			predicates = append(predicates, mongoIndexTypePredicate(ancestor, "object"))
		}
		key := path.storagePath + "\x00" + mongoBSONType(path.kind)
		if _, exists := seen[key]; !exists {
			seen[key] = struct{}{}
			predicates = append(predicates, mongoIndexTypePredicate(path.storagePath, mongoBSONType(path.kind)))
		}
	}
	// Canonical active documents always store an explicit null deletedAt,
	// including collections without authored trash support. Keeping that guard
	// in every native unique index prevents corrupt or out-of-band lifecycle
	// shapes from joining a bounded hinted scan or changing uniqueness.
	predicates = append(predicates, mongoIndexTypePredicate(mongoDeletedAtPath, "null"))
	if len(predicates) == 1 {
		return predicates[0]
	}
	return bson.D{{Key: "$and", Value: mongoDocumentArray(predicates)}}
}

func mongoIndexTypePredicate(path, bsonType string) bson.D {
	return bson.D{{Key: path, Value: bson.D{{Key: "$type", Value: bsonType}}}}
}

func mongoIndexFingerprint(definitions []mongoIndexDefinition) (string, error) {
	digest := sha256.New()
	for _, definition := range definitions {
		keys, err := bson.Marshal(definition.keys)
		if err != nil {
			return "", err
		}
		partial, err := bson.Marshal(normalizeMongoPartialFilter(definition.partialFilter))
		if err != nil {
			return "", err
		}
		digest.Write([]byte(definition.name))
		digest.Write([]byte{0})
		digest.Write(keys)
		if definition.unique {
			digest.Write([]byte{1})
		} else {
			digest.Write([]byte{0})
		}
		digest.Write(partial)
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func normalizeMongoPartialFilter(partial bson.D) bson.D {
	if partial == nil {
		return bson.D{}
	}
	return partial
}

type mongoActualIndex struct {
	keys                    bson.Raw
	unique                  bool
	partialFilter           bson.Raw
	ownedIncompatible       bool
	unmanagedBehaviorHazard bool
}

func (backend *Store) readCollectionIndexes(ctx context.Context, plan mongoCollectionIndexPlan) (map[string]mongoActualIndex, error) {
	return backend.readNamedCollectionIndexes(ctx, plan.physicalName, fmt.Sprintf("collection %q", plan.collection.ID))
}

func (backend *Store) verifyMongoMigrationLedgerPhysicalContract(ctx context.Context) error {
	for _, ledger := range []struct {
		name        string
		description string
	}{
		{name: mongoMigrationArtifactCollectionName, description: "migration artifact ledger"},
		{name: mongoMigrationStepCollectionName, description: "migration step ledger"},
		{name: mongoMigrationLeaseCollectionName, description: "migration lease"},
	} {
		description := fmt.Sprintf("%s namespace %q", ledger.description, ledger.name)
		actual, err := backend.readNamedCollectionIndexes(ctx, ledger.name, description)
		if err != nil {
			return err
		}
		if err := compareMongoNamedIndexSets(description, nil, actual, false); err != nil {
			return err
		}
	}
	return nil
}

func (backend *Store) readNamedCollectionIndexes(ctx context.Context, physicalName, description string) (map[string]mongoActualIndex, error) {
	exists, err := backend.verifyMongoCollectionContract(ctx, physicalName, description)
	if err != nil {
		return nil, err
	}
	if !exists {
		return map[string]mongoActualIndex{}, nil
	}
	cursor, err := backend.database.Collection(physicalName).Indexes().List(ctx)
	if err != nil {
		if mongoErrorHasCode(err, 26) {
			return map[string]mongoActualIndex{}, nil
		}
		return nil, fmt.Errorf("inspect MongoDB indexes for %s: %w", description, translateMongoError(ctx, err))
	}
	defer func() {
		closeContext, cancel := context.WithTimeout(context.Background(), defaultCloseTimeout)
		defer cancel()
		_ = cursor.Close(closeContext)
	}()

	actual := make(map[string]mongoActualIndex)
	nativeIDSeen := false
	for cursor.Next(ctx) {
		raw := append(bson.Raw(nil), cursor.Current...)
		name, ok := raw.Lookup("name").StringValueOK()
		if !ok || name == "" {
			return nil, fmt.Errorf("inspect MongoDB indexes for %s: stored index has no valid name", description)
		}
		if _, duplicate := actual[name]; duplicate {
			return nil, fmt.Errorf("inspect MongoDB indexes for %s: duplicate index name %q", description, name)
		}
		if name == "_id_" {
			if !mongoRawNativeIDIndexMatches(raw) {
				return nil, fmt.Errorf("MongoDB physical index drift for %s: native ID index does not match the required semantic shape", description)
			}
			nativeIDSeen = true
		}
		keys, ok := raw.Lookup("key").DocumentOK()
		if !ok {
			return nil, fmt.Errorf("inspect MongoDB index %q for %s: invalid key contract", name, description)
		}
		unique := false
		if value := raw.Lookup("unique"); value.Type != 0 {
			var valid bool
			unique, valid = value.BooleanOK()
			if !valid {
				return nil, fmt.Errorf("inspect MongoDB index %q for %s: invalid unique contract", name, description)
			}
		}
		var partial bson.Raw
		if value := raw.Lookup("partialFilterExpression"); value.Type != 0 {
			partial, ok = value.DocumentOK()
			if !ok {
				return nil, fmt.Errorf("inspect MongoDB index %q for %s: invalid partial filter", name, description)
			}
			elements, elementsErr := partial.Elements()
			if elementsErr != nil {
				return nil, fmt.Errorf("inspect MongoDB index %q for %s: invalid partial filter", name, description)
			}
			if len(elements) == 0 {
				partial = nil
			}
		}
		actual[name] = mongoActualIndex{
			keys: append(bson.Raw(nil), keys...), unique: unique,
			partialFilter:           append(bson.Raw(nil), partial...),
			ownedIncompatible:       mongoOwnedIndexHasIncompatibleOptions(raw),
			unmanagedBehaviorHazard: mongoUnmanagedIndexChangesBehavior(raw, unique),
		}
	}
	if err := cursor.Err(); err != nil {
		return nil, fmt.Errorf("inspect MongoDB indexes for %s: %w", description, translateMongoError(ctx, err))
	}
	if !nativeIDSeen {
		return nil, fmt.Errorf("MongoDB physical index drift for %s: native ID index does not match the required semantic shape", description)
	}
	return actual, nil
}

func (backend *Store) verifyMongoCollectionContract(ctx context.Context, physicalName, description string) (bool, error) {
	specifications, err := backend.database.ListCollectionSpecifications(ctx, bson.D{{Key: "name", Value: physicalName}})
	if err != nil {
		return false, fmt.Errorf("inspect MongoDB collection metadata for %s: %w", description, translateMongoError(ctx, err))
	}
	if len(specifications) == 0 {
		return false, nil
	}
	if len(specifications) != 1 || !mongoCollectionContractMatches(specifications[0], physicalName) {
		return false, fmt.Errorf("MongoDB physical collection drift for %s: expected an ordinary writable collection whose effective collation, validation, retention, encryption, and native ID semantics are compatible with Ridu", description)
	}
	return true, nil
}

func mongoCollectionContractMatches(specification mongo.CollectionSpecification, physicalName string) bool {
	if specification.Name != physicalName || specification.Type != "collection" || specification.ReadOnly {
		return false
	}
	if err := specification.Options.Validate(); err != nil {
		return false
	}
	if !mongoNativeIDSpecificationMatches(specification.IDIndex) &&
		!mongoCollectionHasCanonicalClusteredID(specification.Options) {
		return false
	}
	if mongoRawBooleanOptionEnabled(specification.Options, "capped") ||
		mongoRawOptionHasEffectiveValue(specification.Options, "expireAfterSeconds") ||
		mongoCollectionHasEnforcingValidator(specification.Options) ||
		mongoCollectionHasEffectiveEncryptedFields(specification.Options) {
		return false
	}
	collationValue := specification.Options.Lookup("collation")
	if mongoRawValueIsSemanticallyEmpty(collationValue) {
		return true
	}
	collation, ok := collationValue.DocumentOK()
	if !ok {
		return false
	}
	locale, ok := collation.Lookup("locale").StringValueOK()
	return ok && locale == "simple"
}

func mongoNativeIDSpecificationMatches(specification mongo.IndexSpecification) bool {
	return specification.Name == "_id_" && mongoNativeIDKeysMatch(specification.KeysDocument) &&
		specification.ExpireAfterSeconds == nil && (specification.Sparse == nil || !*specification.Sparse)
}

func mongoCollectionHasCanonicalClusteredID(options bson.Raw) bool {
	value := options.Lookup("clusteredIndex")
	if mongoRawValueIsSemanticallyEmpty(value) {
		return false
	}
	clustered, ok := value.DocumentOK()
	if !ok {
		return false
	}
	keys, ok := clustered.Lookup("key").DocumentOK()
	if !ok || !mongoNativeIDKeysMatch(keys) {
		return false
	}
	unique, ok := clustered.Lookup("unique").BooleanOK()
	if !ok || !unique {
		return false
	}
	if value := clustered.Lookup("name"); !mongoRawValueIsSemanticallyEmpty(value) {
		name, ok := value.StringValueOK()
		if !ok || name != "_id_" {
			return false
		}
	}
	return true
}

func mongoRawNativeIDIndexMatches(raw bson.Raw) bool {
	if err := raw.Validate(); err != nil {
		return false
	}
	elements, err := raw.Elements()
	if err != nil {
		return false
	}
	nameCount, keyCount := 0, 0
	for _, element := range elements {
		switch element.Key() {
		case "key":
			keyCount++
			keys, ok := element.Value().DocumentOK()
			if !ok || !mongoNativeIDKeysMatch(keys) {
				return false
			}
		case "name":
			nameCount++
			name, ok := element.Value().StringValueOK()
			if !ok || name != "_id_" {
				return false
			}
		}
	}
	if nameCount != 1 || keyCount != 1 ||
		mongoRawOptionHasEffectiveValue(raw, "partialFilterExpression") ||
		mongoOwnedIndexHasIncompatibleOptions(raw) {
		return false
	}
	return true
}

func mongoNativeIDKeysMatch(keys bson.Raw) bool {
	if err := keys.Validate(); err != nil {
		return false
	}
	elements, err := keys.Elements()
	if err != nil || len(elements) != 1 || elements[0].Key() != mongoIDPath {
		return false
	}
	direction, ok := elements[0].Value().Int32OK()
	return ok && direction == mongoAscendingDirection
}

func mongoOwnedIndexHasIncompatibleOptions(raw bson.Raw) bool {
	return mongoRawBooleanOptionEnabled(raw, "sparse") ||
		mongoRawBooleanOptionEnabled(raw, "hidden") ||
		mongoRawOptionHasEffectiveValue(raw, "expireAfterSeconds") ||
		mongoRawBooleanOptionEnabled(raw, "prepareUnique") ||
		mongoIndexHasNonSimpleCollation(raw)
}

func mongoUnmanagedIndexChangesBehavior(raw bson.Raw, unique bool) bool {
	return unique || mongoRawOptionHasEffectiveValue(raw, "expireAfterSeconds") ||
		mongoRawBooleanOptionEnabled(raw, "prepareUnique") ||
		!mongoIndexHasOrdinarySingleFieldKeys(raw)
}

func mongoIndexHasOrdinarySingleFieldKeys(raw bson.Raw) bool {
	keys, ok := raw.Lookup("key").DocumentOK()
	if !ok {
		return false
	}
	elements, err := keys.Elements()
	if err != nil || len(elements) != 1 || strings.Contains(elements[0].Key(), "$**") {
		return false
	}
	return mongoIndexDirectionIsAscendingOrDescending(elements[0].Value())
}

func mongoIndexDirectionIsAscendingOrDescending(value bson.RawValue) bool {
	if direction, ok := value.AsFloat64OK(); ok {
		return direction == float64(mongoAscendingDirection) || direction == float64(mongoDescendingDirection)
	}
	decimal, ok := value.Decimal128OK()
	if !ok {
		return false
	}
	coefficient, exponent, err := decimal.BigInt()
	if err != nil || coefficient.Sign() == 0 {
		return false
	}
	digits := coefficient.String()
	if digits[0] == '-' {
		digits = digits[1:]
	}
	if exponent >= 0 {
		return exponent == 0 && digits == "1"
	}
	zeroCount := -exponent
	if len(digits) != zeroCount+1 || digits[0] != '1' {
		return false
	}
	for _, digit := range digits[1:] {
		if digit != '0' {
			return false
		}
	}
	return true
}

func mongoRawBooleanOptionEnabled(raw bson.Raw, key string) bool {
	value := raw.Lookup(key)
	if mongoRawValueIsSemanticallyEmpty(value) {
		return false
	}
	enabled, ok := value.BooleanOK()
	return !ok || enabled
}

func mongoRawOptionHasEffectiveValue(raw bson.Raw, key string) bool {
	return !mongoRawValueIsSemanticallyEmpty(raw.Lookup(key))
}

func mongoRawValueIsSemanticallyEmpty(value bson.RawValue) bool {
	switch value.Type {
	case 0, bson.TypeNull, bson.TypeUndefined:
		return true
	case bson.TypeBoolean:
		enabled, ok := value.BooleanOK()
		return ok && !enabled
	case bson.TypeEmbeddedDocument:
		document, ok := value.DocumentOK()
		if !ok {
			return false
		}
		elements, err := document.Elements()
		return err == nil && len(elements) == 0
	case bson.TypeArray:
		array, ok := value.ArrayOK()
		if !ok {
			return false
		}
		values, err := array.Values()
		return err == nil && len(values) == 0
	case bson.TypeString:
		text, ok := value.StringValueOK()
		return ok && text == ""
	default:
		return false
	}
}

func mongoIndexHasNonSimpleCollation(raw bson.Raw) bool {
	value := raw.Lookup("collation")
	if mongoRawValueIsSemanticallyEmpty(value) {
		return false
	}
	collation, ok := value.DocumentOK()
	if !ok {
		return true
	}
	locale, ok := collation.Lookup("locale").StringValueOK()
	return !ok || locale != "simple"
}

func mongoCollectionHasEnforcingValidator(options bson.Raw) bool {
	validator := options.Lookup("validator")
	if mongoRawValueIsSemanticallyEmpty(validator) {
		return false
	}
	if _, ok := validator.DocumentOK(); !ok {
		return true
	}
	if level, ok := options.Lookup("validationLevel").StringValueOK(); ok && level == "off" {
		return false
	}
	if action, ok := options.Lookup("validationAction").StringValueOK(); ok && action == "warn" {
		return false
	}
	return true
}

func mongoCollectionHasEffectiveEncryptedFields(options bson.Raw) bool {
	value := options.Lookup("encryptedFields")
	if mongoRawValueIsSemanticallyEmpty(value) {
		return false
	}
	encryptedFields, ok := value.DocumentOK()
	if !ok {
		return true
	}
	fields := encryptedFields.Lookup("fields")
	if mongoRawValueIsSemanticallyEmpty(fields) {
		return false
	}
	return true
}

func compareMongoIndexSets(
	collection schema.Collection,
	desired []mongoIndexDefinition,
	actual map[string]mongoActualIndex,
	allowMissing bool,
) error {
	return compareMongoNamedIndexSets(fmt.Sprintf("collection %q", collection.ID), desired, actual, allowMissing)
}

func compareMongoNamedIndexSets(
	description string,
	desired []mongoIndexDefinition,
	actual map[string]mongoActualIndex,
	allowMissing bool,
) error {
	expected := make(map[string]mongoIndexDefinition, len(desired))
	for _, definition := range desired {
		expected[definition.name] = definition
	}
	for name, found := range actual {
		if name == "_id_" {
			continue
		}
		definition, exists := expected[name]
		if !exists {
			if strings.HasPrefix(name, "z_") {
				return fmt.Errorf("MongoDB physical index drift for %s: unexpected index %q is reserved for Ridu-owned state", description, name)
			}
			if found.unmanagedBehaviorHazard {
				return fmt.Errorf("MongoDB physical index drift for %s: unmanaged index %q can change valid writes or data lifetime", description, name)
			}
			continue
		}
		keys, err := bson.Marshal(definition.keys)
		if err != nil {
			return err
		}
		partial, err := bson.Marshal(normalizeMongoPartialFilter(definition.partialFilter))
		if err != nil {
			return err
		}
		hasPartialFilter := len(definition.partialFilter) != 0
		if found.unique != definition.unique || found.ownedIncompatible || !bytes.Equal(found.keys, keys) ||
			hasPartialFilter && !bytes.Equal(found.partialFilter, partial) ||
			!hasPartialFilter && len(found.partialFilter) != 0 {
			return fmt.Errorf("MongoDB physical index drift for %s: index %q does not match the executable manifest", description, name)
		}
	}
	if allowMissing {
		return nil
	}
	for _, definition := range desired {
		if _, exists := actual[definition.name]; !exists {
			return fmt.Errorf("MongoDB physical index drift for %s: required index %q is missing", description, definition.name)
		}
	}
	return nil
}

func mongoErrorHasCode(err error, wanted int) bool {
	var serverError mongo.ServerError
	if !errors.As(err, &serverError) {
		return false
	}
	for _, code := range serverError.ErrorCodes() {
		if code == wanted {
			return true
		}
	}
	return false
}
