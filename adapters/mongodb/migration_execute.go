package mongodb

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/internal/referenceindex"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

func (backend *Store) applyMongoMigrationTransactionPhase(
	ctx context.Context,
	lease *mongoMigrationLease,
	file migrationartifact.File,
	plan mongoDBArtifactReplayPlan,
	phase mongoDBArtifactReplayPhase,
	registry mongoDBDataTransformRegistry,
) (resultErr error) {
	// A semantic callback must not outlive its fence. Pause the ordinary
	// heartbeat, commit one server-time refresh immediately before opening the
	// transaction, and refresh again inside the same transaction at both ledger
	// boundaries. If the work exceeds the lease duration or another owner takes
	// over, the final conditional refresh fails and every semantic effect rolls
	// back with its step rows.
	lease.mu.Lock()
	defer lease.mu.Unlock()
	if err := lease.refreshUnlocked(ctx, backend.mongoMigrationCollection(mongoMigrationLeaseCollectionName)); err != nil {
		return err
	}
	transaction, err := backend.begin(ctx, false)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			rollbackSystemTransaction(transaction)
		}
	}()

	completed, err := backend.startMongoMigrationTransactionPhase(ctx, transaction, lease, file, phase)
	if err != nil {
		return err
	}
	if completed {
		return nil
	}
	dataTransaction := newMongoDBMigrationDataTransaction(transaction, file.Artifact)
	rebuildReferences := mongoDBMigrationPhaseNeedsReferenceRebuild(phase)
	for _, step := range phase.steps {
		switch step.kind {
		case ridumigration.StepRenameContent:
			if err := backend.executeMongoMigrationRename(ctx, transaction, plan, step.rename); err != nil {
				return fmt.Errorf("apply MongoDB migration %s step %s/%s: %w", file.Name, step.phaseID, step.stepID, err)
			}
		case ridumigration.StepDataTransform:
			registered := registry[step.transform.Name]
			if registered.Checksum != step.transform.Checksum || registered.Up == nil {
				return fmt.Errorf("MongoDB migration %s data transform %q is not bound to its immutable callback", file.Name, step.transform.Name)
			}
			if err := registered.Up(ctx, dataTransaction); err != nil {
				return fmt.Errorf("MongoDB migration %s data transform %q up: %w", file.Name, step.transform.Name, err)
			}
		case ridumigration.StepRetireResources:
			if err := backend.retireMongoMigrationResources(ctx, transaction, step.resourceIDs); err != nil {
				return fmt.Errorf("apply MongoDB migration %s step %s/%s: %w", file.Name, step.phaseID, step.stepID, err)
			}
		default:
			return fmt.Errorf("MongoDB migration %s transaction phase contains unsupported step %q", file.Name, step.kind)
		}
	}
	if rebuildReferences {
		if err := backend.rebuildMongoMigrationReferences(ctx, transaction, plan.after); err != nil {
			return fmt.Errorf("apply MongoDB migration %s phase %s reference rebuild: %w", file.Name, phase.id, err)
		}
	}
	if err := backend.completeMongoMigrationTransactionPhase(ctx, transaction, lease, file, phase); err != nil {
		return err
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit MongoDB migration %s phase %s: %w", file.Name, phase.id, err)
	}
	committed = true
	return nil
}

func mongoDBMigrationPhaseNeedsReferenceRebuild(phase mongoDBArtifactReplayPhase) bool {
	for _, step := range phase.steps {
		if step.kind == ridumigration.StepRenameContent {
			return true
		}
	}
	return false
}

func (backend *Store) startMongoMigrationTransactionPhase(ctx context.Context, transaction *documentTransaction, lease *mongoMigrationLease, file migrationartifact.File, phase mongoDBArtifactReplayPhase) (bool, error) {
	sessionContext, leave, err := transaction.enter(ctx, true)
	if err != nil {
		return false, err
	}
	defer leave()
	if err := lease.refreshUnlocked(sessionContext, backend.mongoMigrationCollection(mongoMigrationLeaseCollectionName)); err != nil {
		return false, err
	}
	complete := 0
	for _, step := range phase.steps {
		collection := backend.mongoMigrationCollection(mongoMigrationStepCollectionName)
		id := mongoMigrationStepLedgerID(file.Name, step.phaseID, step.stepID)
		raw, findErr := collection.FindOne(sessionContext, bson.D{{Key: "_id", Value: id}}).Raw()
		if findErr == nil {
			row, decodeErr := decodeMongoMigrationStepLedger(raw)
			if decodeErr != nil {
				return false, decodeErr
			}
			if !mongoMigrationStepMatches(row, file, step) || row.State != mongoMigrationStepComplete {
				return false, fmt.Errorf("MongoDB migration transaction phase %s has partial or changed durable state", phase.id)
			}
			complete++
			continue
		}
		if !errors.Is(findErr, mongo.ErrNoDocuments) {
			return false, translateMongoError(sessionContext, findErr)
		}
		row := mongoMigrationStepLedgerRow{
			ID: id, ArtifactName: file.Name, ArtifactDigest: file.Digest,
			PhaseID: step.phaseID, StepID: step.stepID, PhaseMode: step.mode, StepKind: step.kind,
			State: mongoMigrationStepRunning, Attempts: 1, Owner: lease.owner, Fence: lease.fence,
			UpdatedAt: backend.now().UTC(),
		}
		document, encodeErr := encodeMongoMigrationStepLedger(row)
		if encodeErr != nil {
			return false, encodeErr
		}
		if _, insertErr := collection.InsertOne(sessionContext, document); insertErr != nil {
			return false, translateMongoError(sessionContext, insertErr)
		}
	}
	if complete != 0 && complete != len(phase.steps) {
		return false, fmt.Errorf("MongoDB migration transaction phase %s is only partly complete", phase.id)
	}
	return complete == len(phase.steps), nil
}

func (backend *Store) completeMongoMigrationTransactionPhase(ctx context.Context, transaction *documentTransaction, lease *mongoMigrationLease, file migrationartifact.File, phase mongoDBArtifactReplayPhase) error {
	sessionContext, leave, err := transaction.enter(ctx, true)
	if err != nil {
		return err
	}
	defer leave()
	if err := lease.refreshUnlocked(sessionContext, backend.mongoMigrationCollection(mongoMigrationLeaseCollectionName)); err != nil {
		return err
	}
	for _, step := range phase.steps {
		id := mongoMigrationStepLedgerID(file.Name, step.phaseID, step.stepID)
		raw, findErr := backend.mongoMigrationCollection(mongoMigrationStepCollectionName).FindOne(sessionContext, bson.D{{Key: "_id", Value: id}}).Raw()
		if findErr != nil {
			return translateMongoError(sessionContext, findErr)
		}
		row, decodeErr := decodeMongoMigrationStepLedger(raw)
		if decodeErr != nil {
			return decodeErr
		}
		if !mongoMigrationStepMatches(row, file, step) || row.State != mongoMigrationStepRunning || row.Owner != lease.owner || row.Fence != lease.fence {
			return errMongoMigrationLeaseLost
		}
		now := backend.now().UTC()
		row.State, row.UpdatedAt, row.CompletedAt = mongoMigrationStepComplete, now, &now
		document, encodeErr := encodeMongoMigrationStepLedger(row)
		if encodeErr != nil {
			return encodeErr
		}
		result, replaceErr := backend.mongoMigrationCollection(mongoMigrationStepCollectionName).ReplaceOne(sessionContext, bson.D{
			{Key: "_id", Value: id}, {Key: "state", Value: mongoMigrationStepRunning},
			{Key: "owner", Value: lease.owner}, {Key: "fence", Value: lease.fence},
		}, document)
		if replaceErr != nil {
			return translateMongoError(sessionContext, replaceErr)
		}
		if result.MatchedCount != 1 {
			return errMongoMigrationLeaseLost
		}
	}
	return nil
}

func (backend *Store) dropMongoMigrationIndex(ctx context.Context, planned mongoDBPlannedIndex) error {
	actual, err := backend.readNamedCollectionIndexes(ctx, planned.collection, planned.description)
	if err != nil {
		return err
	}
	found, exists := actual[planned.name]
	if !exists {
		return nil
	}
	if err := compareMongoNamedIndexSets(planned.description, []mongoIndexDefinition{planned.definition}, map[string]mongoActualIndex{"_id_": actual["_id_"], planned.name: found}, true); err != nil {
		return err
	}
	return translateMongoError(ctx, backend.database.Collection(planned.collection).Indexes().DropOne(ctx, planned.name))
}

func (backend *Store) renameMongoMigrationResource(ctx context.Context, plan mongoDBArtifactReplayPlan, step mongoDBArtifactReplayStep) error {
	payload := step.resourceRename
	if plan.before == nil {
		return fmt.Errorf("MongoDB resource rename has no before manifest")
	}
	beforeResource, beforeOK := mongoManifestResourceByID(plan.before.Snapshot(), payload.BeforeID)
	afterResource, afterOK := mongoManifestResourceByID(plan.after.Snapshot(), payload.AfterID)
	if !beforeOK || !afterOK {
		return fmt.Errorf("MongoDB resource rename does not match immutable manifests")
	}
	if err := backend.renameMongoMigrationNamespace(ctx, physicalCollectionName(payload.BeforeID), physicalCollectionName(payload.AfterID), step.resourceRenameContentRequired); err != nil {
		return err
	}
	if beforeResource.Versions != nil || beforeResource.Capabilities.Versions || afterResource.Versions != nil || afterResource.Capabilities.Versions {
		if err := backend.renameMongoMigrationNamespace(ctx, physicalVersionCollectionName(payload.BeforeID), physicalVersionCollectionName(payload.AfterID), step.resourceRenameVersionRequired); err != nil {
			return err
		}
	}
	return nil
}

func mongoManifestResourceByID(snapshot schema.Snapshot, id schema.StableID) (schema.Collection, bool) {
	for _, resource := range append(append([]schema.Collection(nil), snapshot.Collections...), snapshot.Globals...) {
		if resource.ID == id {
			return resource, true
		}
	}
	return schema.Collection{}, false
}

func (backend *Store) renameMongoMigrationNamespace(ctx context.Context, before, after string, sourceRequired bool) error {
	names, err := backend.database.ListCollectionNames(ctx, bson.D{{Key: "name", Value: bson.D{{Key: "$in", Value: bson.A{before, after}}}}})
	if err != nil {
		return translateMongoError(ctx, err)
	}
	beforeExists, afterExists := false, false
	for _, name := range names {
		beforeExists = beforeExists || name == before
		afterExists = afterExists || name == after
	}
	perform, err := mongoDBRenameNamespaceAction(beforeExists, afterExists, sourceRequired)
	if err != nil {
		return err
	}
	if !perform {
		return nil
	}
	command := bson.D{
		{Key: "renameCollection", Value: backend.database.Name() + "." + before},
		{Key: "to", Value: backend.database.Name() + "." + after},
		{Key: "dropTarget", Value: false},
	}
	if err := backend.client.Database("admin").RunCommand(ctx, command).Err(); err != nil {
		return translateMongoError(ctx, err)
	}
	return nil
}

func mongoDBRenameNamespaceAction(beforeExists, afterExists, sourceRequired bool) (bool, error) {
	if beforeExists && afterExists {
		return false, fmt.Errorf("MongoDB resource rename target already exists while its source remains")
	}
	if !beforeExists && !afterExists {
		if sourceRequired {
			return false, fmt.Errorf("MongoDB resource rename source and target are both absent despite the immutable source physical contract")
		}
		return false, nil
	}
	if afterExists {
		return false, nil
	}
	return true, nil
}

func (backend *Store) dropMongoMigrationResources(ctx context.Context, plan mongoDBArtifactReplayPlan, ids []schema.StableID) error {
	for _, id := range ids {
		if plan.before == nil {
			return fmt.Errorf("MongoDB retired resource has no immutable before manifest")
		}
		if _, exists := mongoManifestResourceByID(plan.before.Snapshot(), id); !exists {
			return fmt.Errorf("MongoDB retired resource is outside the immutable before manifest")
		}
		for _, name := range []string{physicalCollectionName(id), physicalVersionCollectionName(id)} {
			names, err := backend.database.ListCollectionNames(ctx, bson.D{{Key: "name", Value: name}})
			if err != nil {
				return translateMongoError(ctx, err)
			}
			if len(names) == 0 {
				continue
			}
			if err := backend.database.Collection(name).Drop(ctx); err != nil {
				return translateMongoError(ctx, err)
			}
		}
	}
	return nil
}

func (backend *Store) executeMongoMigrationRename(ctx context.Context, transaction *documentTransaction, plan mongoDBArtifactReplayPlan, intent ridumigration.Rename) error {
	if plan.before == nil {
		return fmt.Errorf("MongoDB content rename has no before manifest")
	}
	beforeBySlug := mongoSemanticCollectionsBySlug(plan.before.Snapshot().Collections)
	afterBySlug := mongoSemanticCollectionsBySlug(plan.after.Snapshot().Collections)
	beforeResource, beforeOK := beforeBySlug[intent.CollectionBefore]
	afterResource, afterOK := afterBySlug[intent.CollectionAfter]
	if !beforeOK || !afterOK {
		return fmt.Errorf("MongoDB content rename does not match immutable manifests")
	}
	pairs := intent.Fields
	if intent.FieldBefore != "" {
		pairs = []ridumigration.FieldRename{{Before: intent.FieldBefore, After: intent.FieldAfter}}
	}
	if len(pairs) != 0 {
		fieldSchemas := mongoDBMigrationOwnerFieldSchemas(plan, afterResource.ID)
		if err := backend.rewriteMongoMigrationDocuments(ctx, transaction, afterResource, fieldSchemas, pairs, "", ""); err != nil {
			return fmt.Errorf("rewrite resource %s field content: %w", afterResource.ID, err)
		}
	}
	if intent.FieldBefore == "" && intent.CollectionBefore != intent.CollectionAfter {
		resources := append(append([]schema.Collection(nil), plan.after.Snapshot().Collections...), plan.after.Snapshot().Globals...)
		for _, resource := range resources {
			fieldSchemas := mongoDBMigrationOwnerFieldSchemas(plan, resource.ID)
			if err := backend.rewriteMongoMigrationDocuments(ctx, transaction, resource, fieldSchemas, nil, string(intent.CollectionBefore), string(intent.CollectionAfter)); err != nil {
				return fmt.Errorf("rewrite resource %s relationship content: %w", resource.ID, err)
			}
		}
		if err := backend.rewriteMongoMigrationFrameworkState(ctx, transaction, beforeResource.ID, afterResource.ID); err != nil {
			return fmt.Errorf("rewrite framework state %s -> %s: %w", beforeResource.ID, afterResource.ID, err)
		}
	}
	return nil
}

type mongoDBMigrationFieldSchemas struct {
	before []schema.Field
	after  []schema.Field
}

func (schemas mongoDBMigrationFieldSchemas) all() [][]schema.Field {
	result := make([][]schema.Field, 0, 2)
	if schemas.before != nil {
		result = append(result, schemas.before)
	}
	if schemas.after != nil {
		result = append(result, schemas.after)
	}
	return result
}

func (backend *Store) rewriteMongoMigrationDocuments(ctx context.Context, transaction *documentTransaction, resource schema.Collection, fieldSchemas mongoDBMigrationFieldSchemas, pairs []ridumigration.FieldRename, beforeSlug, afterSlug string) error {
	sessionContext, leave, err := transaction.enter(ctx, true)
	if err != nil {
		return err
	}
	defer leave()
	if fieldSchemas.after == nil {
		fieldSchemas.after = resource.Fields
	}
	if err := rewriteMongoDocumentCollection(sessionContext, backend.database.Collection(physicalCollectionName(resource.ID)), fieldSchemas, pairs, beforeSlug, afterSlug, false); err != nil {
		return err
	}
	if resource.Versions != nil || resource.Capabilities.Versions {
		return rewriteMongoDocumentCollection(sessionContext, backend.database.Collection(physicalVersionCollectionName(resource.ID)), fieldSchemas, pairs, beforeSlug, afterSlug, true)
	}
	return nil
}

func rewriteMongoDocumentCollection(ctx context.Context, collection *mongo.Collection, fieldSchemas mongoDBMigrationFieldSchemas, pairs []ridumigration.FieldRename, beforeSlug, afterSlug string, version bool) error {
	cursor, err := collection.Find(ctx, bson.D{})
	if err != nil {
		return translateMongoError(ctx, err)
	}
	defer cursor.Close(ctx)
	for cursor.Next(ctx) {
		raw := cursor.Current
		documentRaw := raw
		prefix := ""
		if version {
			var ok bool
			documentRaw, ok = raw.Lookup(mongoVersionSnapshotPath).DocumentOK()
			if !ok {
				return fmt.Errorf("stored MongoDB version has an invalid snapshot")
			}
			prefix = mongoVersionSnapshotPath + "."
		}
		document, decodeErr := decodeDocument(documentRaw)
		if decodeErr != nil {
			return decodeErr
		}
		changed, renameErr := renameMongoStoreFieldsBySchema(document.Values, fieldSchemas, pairs)
		if renameErr != nil {
			return renameErr
		}
		if beforeSlug != "" {
			for _, fields := range fieldSchemas.all() {
				changed = rewriteMongoCollectionReferences(fields, document.Values, beforeSlug, afterSlug) || changed
			}
		}
		if !changed {
			continue
		}
		values, encodeErr := encodeValues(document.Values)
		if encodeErr != nil {
			return encodeErr
		}
		id, ok := raw.Lookup("_id").StringValueOK()
		if !ok {
			return fmt.Errorf("stored MongoDB migration document has invalid identity")
		}
		if _, updateErr := collection.UpdateOne(ctx, bson.D{{Key: "_id", Value: id}}, bson.D{{Key: "$set", Value: bson.D{{Key: prefix + "values", Value: values}}}}); updateErr != nil {
			return translateMongoError(ctx, updateErr)
		}
	}
	return translateMongoError(ctx, cursor.Err())
}

type mongoDBFieldRenameContainer struct {
	field    schema.Field
	blockKey string
}

func renameMongoStoreFieldsBySchema(values store.Values, schemas mongoDBMigrationFieldSchemas, pairs []ridumigration.FieldRename) (bool, error) {
	ordered, err := orderMongoDBFieldRenames(pairs)
	if err != nil {
		return false, err
	}
	working := store.CloneValues(values)
	changed := false
	for _, pair := range ordered {
		moved, moveErr := renameMongoStoreFieldBySchema(working, schemas, pair)
		if moveErr != nil {
			return false, moveErr
		}
		changed = moved || changed
	}
	if !changed {
		return false, nil
	}
	clear(values)
	for key, value := range working {
		values[key] = value
	}
	return true, nil
}

func orderMongoDBFieldRenames(pairs []ridumigration.FieldRename) ([]ridumigration.FieldRename, error) {
	type rankedRename struct {
		pair        ridumigration.FieldRename
		beforeDepth int
		afterDepth  int
	}
	ranked := make([]rankedRename, len(pairs))
	for index, pair := range pairs {
		before, beforeErr := query.ParsePath(pair.Before)
		after, afterErr := query.ParsePath(pair.After)
		if beforeErr != nil || afterErr != nil {
			return nil, fmt.Errorf("invalid immutable MongoDB field rename path")
		}
		ranked[index] = rankedRename{pair: pair, beforeDepth: len(before.Segments()), afterDepth: len(after.Segments())}
	}
	// Descendants must move before their containers. This is a topological
	// order of the immutable before-schema field tree and keeps every source
	// address reachable regardless of the serialized intent.Fields order.
	sort.Slice(ranked, func(left, right int) bool {
		if ranked[left].beforeDepth != ranked[right].beforeDepth {
			return ranked[left].beforeDepth > ranked[right].beforeDepth
		}
		if ranked[left].afterDepth != ranked[right].afterDepth {
			return ranked[left].afterDepth > ranked[right].afterDepth
		}
		if ranked[left].pair.Before != ranked[right].pair.Before {
			return ranked[left].pair.Before < ranked[right].pair.Before
		}
		return ranked[left].pair.After < ranked[right].pair.After
	})
	ordered := make([]ridumigration.FieldRename, len(ranked))
	for index, rename := range ranked {
		ordered[index] = rename.pair
	}
	return ordered, nil
}

func renameMongoStoreFieldBySchema(values store.Values, schemas mongoDBMigrationFieldSchemas, pair ridumigration.FieldRename) (bool, error) {
	beforePath, beforeErr := query.ParsePath(pair.Before)
	afterPath, afterErr := query.ParsePath(pair.After)
	if beforeErr != nil || afterErr != nil {
		return false, fmt.Errorf("invalid immutable MongoDB field rename path")
	}
	beforeSegments, afterSegments := beforePath.Segments(), afterPath.Segments()
	if len(beforeSegments) == 0 || len(afterSegments) == 0 {
		return false, fmt.Errorf("invalid immutable MongoDB field rename path")
	}
	source, destination := beforeSegments[len(beforeSegments)-1], afterSegments[len(afterSegments)-1]
	if source == destination {
		return false, nil
	}

	type traversal struct {
		containers []mongoDBFieldRenameContainer
		found      bool
	}
	traversals := []traversal{
		mongoDBFieldRenameTraversal(schemas.before, pair.Before),
		mongoDBFieldRenameTraversal(schemas.after, pair.After),
	}
	if !traversals[0].found && !traversals[1].found {
		return false, fmt.Errorf("MongoDB field rename does not match immutable field schemas")
	}
	changed := false
	for _, candidate := range traversals {
		if !candidate.found {
			continue
		}
		moved, err := renameMongoStoreFieldInContainers(values, candidate.containers, source, destination)
		if err != nil {
			return false, err
		}
		changed = moved || changed
	}
	return changed, nil
}

func mongoDBFieldRenameTraversal(fields []schema.Field, path string) struct {
	containers []mongoDBFieldRenameContainer
	found      bool
} {
	containers, found := findMongoDBFieldRenameTraversal(fields, path, nil)
	return struct {
		containers []mongoDBFieldRenameContainer
		found      bool
	}{containers: containers, found: found}
}

func findMongoDBFieldRenameTraversal(fields []schema.Field, path string, parents []mongoDBFieldRenameContainer) ([]mongoDBFieldRenameContainer, bool) {
	for _, field := range fields {
		if field.Path.String() == path {
			return append([]mongoDBFieldRenameContainer(nil), parents...), true
		}
		if field.Nested != nil {
			next := appendMongoDBFieldRenameContainer(parents, mongoDBFieldRenameContainer{field: field})
			if containers, found := findMongoDBFieldRenameTraversal(field.Nested.Fields, path, next); found {
				return containers, true
			}
		}
		if field.Blocks != nil {
			for _, block := range field.Blocks.Types {
				next := appendMongoDBFieldRenameContainer(parents, mongoDBFieldRenameContainer{field: field, blockKey: block.Key})
				if containers, found := findMongoDBFieldRenameTraversal(block.Fields, path, next); found {
					return containers, true
				}
			}
		}
	}
	return nil, false
}

func appendMongoDBFieldRenameContainer(values []mongoDBFieldRenameContainer, value mongoDBFieldRenameContainer) []mongoDBFieldRenameContainer {
	result := make([]mongoDBFieldRenameContainer, len(values), len(values)+1)
	copy(result, values)
	return append(result, value)
}

func renameMongoStoreFieldInContainers(values store.Values, containers []mongoDBFieldRenameContainer, source, destination string) (bool, error) {
	if len(containers) == 0 {
		value, sourceExists := values[source]
		_, destinationExists := values[destination]
		if sourceExists && destinationExists {
			return false, fmt.Errorf("MongoDB field rename target already contains data")
		}
		if !sourceExists {
			return false, nil
		}
		delete(values, source)
		values[destination] = value
		return true, nil
	}
	container := containers[0]
	value, exists := values[container.field.Name]
	if !exists {
		return false, nil
	}
	updated, changed, err := renameMongoStoreFieldInsideContainer(value, container, containers[1:], source, destination)
	if err != nil {
		return false, err
	}
	if changed {
		values[container.field.Name] = updated
	}
	return changed, nil
}

func renameMongoStoreFieldInsideContainer(value store.Value, container mongoDBFieldRenameContainer, remaining []mongoDBFieldRenameContainer, source, destination string) (store.Value, bool, error) {
	if container.field.Localized {
		localized, valid := value.ObjectValue()
		if !valid {
			return value, false, nil
		}
		container.field.Localized = false
		changed := false
		for locale, localizedValue := range localized {
			updated, localeChanged, err := renameMongoStoreFieldInsideContainer(localizedValue, container, remaining, source, destination)
			if err != nil {
				return value, false, err
			}
			if localeChanged {
				localized[locale] = updated
				changed = true
			}
		}
		if changed {
			return store.Object(localized), true, nil
		}
		return value, false, nil
	}

	switch container.field.Type {
	case schema.FieldTypeGroup:
		object, valid := value.ObjectValue()
		if !valid {
			return value, false, nil
		}
		changed, err := renameMongoStoreFieldInContainers(object, remaining, source, destination)
		if err != nil {
			return value, false, err
		}
		if changed {
			return store.Object(object), true, nil
		}
	case schema.FieldTypeArray:
		items, valid := value.Values()
		if !valid {
			return value, false, nil
		}
		changed := false
		for index, item := range items {
			object, valid := item.ObjectValue()
			if !valid {
				continue
			}
			itemChanged, err := renameMongoStoreFieldInContainers(object, remaining, source, destination)
			if err != nil {
				return value, false, err
			}
			if itemChanged {
				items[index] = store.Object(object)
				changed = true
			}
		}
		if changed {
			return store.List(items...), true, nil
		}
	case schema.FieldTypeBlocks:
		items, valid := value.Values()
		if !valid {
			return value, false, nil
		}
		changed := false
		for index, item := range items {
			object, valid := item.ObjectValue()
			if !valid {
				continue
			}
			blockType, _ := object["blockType"].StringValue()
			if blockType != container.blockKey {
				continue
			}
			itemChanged, err := renameMongoStoreFieldInContainers(object, remaining, source, destination)
			if err != nil {
				return value, false, err
			}
			if itemChanged {
				items[index] = store.Object(object)
				changed = true
			}
		}
		if changed {
			return store.List(items...), true, nil
		}
	}
	return value, false, nil
}

func mongoDBMigrationOwnerFieldSchemas(plan mongoDBArtifactReplayPlan, physicalOwnerID schema.StableID) mongoDBMigrationFieldSchemas {
	var result mongoDBMigrationFieldSchemas
	if plan.before != nil {
		before := plan.before.Snapshot()
		for _, resource := range append(append([]schema.Collection(nil), before.Collections...), before.Globals...) {
			mappedID := resource.ID
			if mapped := plan.collectionMapping[resource.ID]; mapped != "" {
				mappedID = mapped
			}
			if mappedID == physicalOwnerID {
				result.before = resource.Fields
				break
			}
		}
	}
	after := plan.after.Snapshot()
	for _, resource := range append(append([]schema.Collection(nil), after.Collections...), after.Globals...) {
		if resource.ID == physicalOwnerID {
			result.after = resource.Fields
			break
		}
	}
	return result
}

func rewriteMongoCollectionReferences(fields []schema.Field, values store.Values, before, after string) bool {
	changed := false
	for _, field := range fields {
		value, exists := values[field.Name]
		if !exists {
			continue
		}
		updated, fieldChanged := rewriteMongoCollectionReferenceField(field, value, before, after)
		if fieldChanged {
			values[field.Name] = updated
			changed = true
		}
	}
	return changed
}

func rewriteMongoCollectionReferenceField(field schema.Field, value store.Value, before, after string) (store.Value, bool) {
	if field.Localized {
		localized, valid := value.ObjectValue()
		if !valid {
			return value, false
		}
		field.Localized = false
		changed := false
		for locale, localizedValue := range localized {
			updated, localeChanged := rewriteMongoCollectionReferenceFieldValue(field, localizedValue, before, after)
			if localeChanged {
				localized[locale] = updated
				changed = true
			}
		}
		if changed {
			return store.Object(localized), true
		}
		return value, false
	}
	return rewriteMongoCollectionReferenceFieldValue(field, value, before, after)
}

func rewriteMongoCollectionReferenceFieldValue(field schema.Field, value store.Value, before, after string) (store.Value, bool) {
	if field.Plugin != nil && len(field.Plugin.ReferenceKeys) != 0 {
		keys := make(map[string]struct{}, len(field.Plugin.ReferenceKeys))
		for _, key := range field.Plugin.ReferenceKeys {
			keys[key] = struct{}{}
		}
		return rewriteMongoDeclaredPluginCollectionReferences(value, keys, before, after)
	}
	switch field.Type {
	case schema.FieldTypeRelationship:
		return rewriteMongoPolymorphicRelationshipSlugValue(field, value, before, after)
	case schema.FieldTypeGroup:
		object, valid := value.ObjectValue()
		if !valid || field.Nested == nil {
			return value, false
		}
		if rewriteMongoCollectionReferences(field.Nested.Fields, object, before, after) {
			return store.Object(object), true
		}
	case schema.FieldTypeArray:
		items, valid := value.Values()
		if !valid || field.Nested == nil {
			return value, false
		}
		changed := false
		for index, item := range items {
			object, valid := item.ObjectValue()
			if !valid {
				continue
			}
			if rewriteMongoCollectionReferences(field.Nested.Fields, object, before, after) {
				items[index] = store.Object(object)
				changed = true
			}
		}
		if changed {
			return store.List(items...), true
		}
	case schema.FieldTypeBlocks:
		items, valid := value.Values()
		if !valid || field.Blocks == nil {
			return value, false
		}
		changed := false
		for index, item := range items {
			object, valid := item.ObjectValue()
			if !valid {
				continue
			}
			blockType, _ := object["blockType"].StringValue()
			for _, block := range field.Blocks.Types {
				if block.Key != blockType {
					continue
				}
				if rewriteMongoCollectionReferences(block.Fields, object, before, after) {
					items[index] = store.Object(object)
					changed = true
				}
				break
			}
		}
		if changed {
			return store.List(items...), true
		}
	}
	return value, false
}

func rewriteMongoPolymorphicRelationshipSlugValue(field schema.Field, value store.Value, before, after string) (store.Value, bool) {
	relationship := field.Relationship
	if relationship == nil || !relationship.Polymorphic {
		return value, false
	}
	if !relationship.HasMany {
		return rewriteMongoPolymorphicRelationshipSlugObject(value, before, after)
	}
	items, valid := value.Values()
	if !valid {
		return value, false
	}
	changed := false
	for index, item := range items {
		updated, itemChanged := rewriteMongoPolymorphicRelationshipSlugObject(item, before, after)
		if itemChanged {
			items[index] = updated
			changed = true
		}
	}
	if changed {
		return store.List(items...), true
	}
	return value, false
}

func rewriteMongoPolymorphicRelationshipSlugObject(value store.Value, before, after string) (store.Value, bool) {
	object, valid := value.ObjectValue()
	if !valid {
		return value, false
	}
	relationTo, valid := object["relationTo"].StringValue()
	if !valid || relationTo != before {
		return value, false
	}
	object["relationTo"] = store.String(after)
	return store.Object(object), true
}

func rewriteMongoDeclaredPluginCollectionReferences(value store.Value, keys map[string]struct{}, before, after string) (store.Value, bool) {
	if object, valid := value.ObjectValue(); valid {
		changed := false
		for key, child := range object {
			if _, declared := keys[key]; declared {
				if collection, stringValue := child.StringValue(); stringValue && collection == before {
					object[key] = store.String(after)
					changed = true
					continue
				}
			}
			updated, childChanged := rewriteMongoDeclaredPluginCollectionReferences(child, keys, before, after)
			if childChanged {
				object[key] = updated
				changed = true
			}
		}
		if changed {
			return store.Object(object), true
		}
		return value, false
	}
	if items, valid := value.Values(); valid {
		changed := false
		for index, item := range items {
			updated, itemChanged := rewriteMongoDeclaredPluginCollectionReferences(item, keys, before, after)
			if itemChanged {
				items[index] = updated
				changed = true
			}
		}
		if changed {
			return store.List(items...), true
		}
	}
	return value, false
}

func (backend *Store) retireMongoMigrationResources(ctx context.Context, transaction *documentTransaction, ids []schema.StableID) error {
	sessionContext, leave, err := transaction.enter(ctx, true)
	if err != nil {
		return err
	}
	defer leave()
	values := make(bson.A, len(ids))
	for index, id := range ids {
		values[index] = string(id)
	}
	deletions := []struct {
		collection string
		filter     bson.D
	}{
		{mongoReferenceCollectionName, bson.D{{Key: "$or", Value: bson.A{bson.D{{Key: "ownerCollection", Value: bson.D{{Key: "$in", Value: values}}}}, bson.D{{Key: "targetCollection", Value: bson.D{{Key: "$in", Value: values}}}}}}}},
		{mongoAuthCredentialCollectionName, bson.D{{Key: "collection", Value: bson.D{{Key: "$in", Value: values}}}}},
		{mongoAuthSessionCollectionName, bson.D{{Key: "collection", Value: bson.D{{Key: "$in", Value: values}}}}},
		{mongoAuthTokenCollectionName, bson.D{{Key: "collection", Value: bson.D{{Key: "$in", Value: values}}}}},
		{mongoAuthAPIKeyCollectionName, bson.D{{Key: "collection", Value: bson.D{{Key: "$in", Value: values}}}}},
		{mongoAuthBootstrapCollectionName, bson.D{{Key: "collection", Value: bson.D{{Key: "$in", Value: values}}}}},
		{mongoPreferenceCollectionName, bson.D{{Key: "collection", Value: bson.D{{Key: "$in", Value: values}}}}},
		{mongoDocumentLockCollectionName, bson.D{{Key: "$or", Value: bson.A{bson.D{{Key: "collection", Value: bson.D{{Key: "$in", Value: values}}}}, bson.D{{Key: "ownerCollection", Value: bson.D{{Key: "$in", Value: values}}}}}}}},
		{mongoTaskCollectionName, bson.D{{Key: "$or", Value: bson.A{bson.D{{Key: "target.collection", Value: bson.D{{Key: "$in", Value: values}}}}, bson.D{{Key: "requestedBy.collection", Value: bson.D{{Key: "$in", Value: values}}}}}}}},
		{mongoTaskConcurrencyCollectionName, bson.D{{Key: "$or", Value: bson.A{bson.D{{Key: "target.collection", Value: bson.D{{Key: "$in", Value: values}}}}, bson.D{{Key: "requestedBy.collection", Value: bson.D{{Key: "$in", Value: values}}}}}}}},
	}
	for _, deletion := range deletions {
		if _, err := backend.database.Collection(deletion.collection).DeleteMany(sessionContext, deletion.filter); err != nil {
			return translateMongoError(sessionContext, err)
		}
	}
	return nil
}

func (backend *Store) rewriteMongoMigrationFrameworkState(ctx context.Context, transaction *documentTransaction, before, after schema.StableID) error {
	sessionContext, leave, err := transaction.enter(ctx, true)
	if err != nil {
		return err
	}
	defer leave()
	if err := backend.rewriteMongoScheduledPublishMigrationState(sessionContext, before, after); err != nil {
		return err
	}
	// Rows whose primary key does not depend on the collection identity can be
	// updated directly. Identity-derived rows are rewritten through their codec
	// below so `_id` remains canonical.
	for _, update := range []struct {
		collection string
		filter     bson.D
		set        bson.D
	}{
		{mongoAuthSessionCollectionName, bson.D{{Key: "collection", Value: string(before)}}, bson.D{{Key: "collection", Value: string(after)}}},
		{mongoAuthAPIKeyCollectionName, bson.D{{Key: "collection", Value: string(before)}}, bson.D{{Key: "collection", Value: string(after)}}},
		{mongoTaskCollectionName, bson.D{{Key: "target.collection", Value: string(before)}}, bson.D{{Key: "target.collection", Value: string(after)}}},
		{mongoTaskCollectionName, bson.D{{Key: "requestedBy.collection", Value: string(before)}}, bson.D{{Key: "requestedBy.collection", Value: string(after)}}},
		{mongoTaskConcurrencyCollectionName, bson.D{{Key: "target.collection", Value: string(before)}}, bson.D{{Key: "target.collection", Value: string(after)}}},
		{mongoTaskConcurrencyCollectionName, bson.D{{Key: "requestedBy.collection", Value: string(before)}}, bson.D{{Key: "requestedBy.collection", Value: string(after)}}},
	} {
		if _, err := backend.database.Collection(update.collection).UpdateMany(sessionContext, update.filter, bson.D{{Key: "$set", Value: update.set}}); err != nil {
			return translateMongoError(sessionContext, err)
		}
	}
	return backend.rewriteMongoIdentityDerivedState(sessionContext, before, after)
}

const mongoScheduledPublishTaskSlug = "ridu-schedule-publish"

type mongoScheduledPublishTaskRewrite struct {
	before store.Task
	after  store.Task
}

func (backend *Store) rewriteMongoScheduledPublishMigrationState(ctx context.Context, before, after schema.StableID) error {
	cursor, err := backend.database.Collection(mongoTaskCollectionName).Find(ctx, bson.D{{Key: "slug", Value: mongoScheduledPublishTaskSlug}})
	if err != nil {
		return fmt.Errorf("inspect scheduled-publish tasks: %w", translateMongoError(ctx, err))
	}
	var rewrites []mongoScheduledPublishTaskRewrite
	for cursor.Next(ctx) {
		task, decodeErr := decodeMongoTask(cursor.Current)
		if decodeErr != nil {
			_ = cursor.Close(ctx)
			return fmt.Errorf("decode scheduled-publish task: %w", decodeErr)
		}
		if !mongoScheduledPublishTaskMentionsCollection(task, before) {
			continue
		}
		updated, changed, rewriteErr := rewriteMongoScheduledPublishTaskCollectionID(task, before, after)
		if rewriteErr != nil {
			_ = cursor.Close(ctx)
			return fmt.Errorf("rewrite scheduled-publish task %s: %w", task.ID, rewriteErr)
		}
		if changed {
			rewrites = append(rewrites, mongoScheduledPublishTaskRewrite{before: task, after: updated})
		}
	}
	if err := cursor.Err(); err != nil {
		_ = cursor.Close(ctx)
		return fmt.Errorf("scan scheduled-publish tasks: %w", translateMongoError(ctx, err))
	}
	_ = cursor.Close(ctx)

	byTaskID := make(map[string]mongoScheduledPublishTaskRewrite, len(rewrites))
	for _, rewrite := range rewrites {
		byTaskID[rewrite.before.ID] = rewrite
	}
	processedGuards := make(map[string]struct{})
	for _, rewrite := range rewrites {
		if rewrite.before.ConcurrencyKey == rewrite.after.ConcurrencyKey || rewrite.before.ConcurrencyKey == "" {
			continue
		}
		oldGuardID := mongoTaskConcurrencyID(rewrite.before.Queue, rewrite.before.ConcurrencyKey)
		if _, processed := processedGuards[oldGuardID]; processed {
			continue
		}
		processedGuards[oldGuardID] = struct{}{}
		raw, findErr := backend.database.Collection(mongoTaskConcurrencyCollectionName).FindOne(ctx, bson.D{{Key: "_id", Value: oldGuardID}}).Raw()
		if errors.Is(findErr, mongo.ErrNoDocuments) {
			continue
		}
		if findErr != nil {
			return fmt.Errorf("find scheduled-publish concurrency guard: %w", translateMongoError(ctx, findErr))
		}
		guard, decodeErr := decodeMongoTaskConcurrencyGuard(raw)
		if decodeErr != nil {
			return fmt.Errorf("decode scheduled-publish concurrency guard: %w", decodeErr)
		}
		owner, found := byTaskID[guard.TaskID]
		if !found || owner.before.Queue != guard.Queue || owner.before.ConcurrencyKey != guard.Key ||
			!mongoTaskGuardMatchesTask(guard, owner.before, owner.before.LeaseToken) {
			return fmt.Errorf("scheduled-publish concurrency guard does not match its immutable task identity")
		}
		updatedGuard := guard
		updatedGuard.Key = owner.after.ConcurrencyKey
		updatedGuard.ID = mongoTaskConcurrencyID(updatedGuard.Queue, updatedGuard.Key)
		updatedGuard.Target = cloneMongoTaskReference(owner.after.Target)
		updatedGuard.RequestedBy = cloneMongoTaskReference(owner.after.RequestedBy)
		encoded, encodeErr := encodeMongoTaskConcurrencyGuard(updatedGuard)
		if encodeErr != nil {
			return fmt.Errorf("encode scheduled-publish concurrency guard: %w", encodeErr)
		}
		if updatedGuard.ID == guard.ID {
			if _, replaceErr := backend.database.Collection(mongoTaskConcurrencyCollectionName).ReplaceOne(ctx, bson.D{{Key: "_id", Value: guard.ID}}, encoded); replaceErr != nil {
				return fmt.Errorf("replace scheduled-publish concurrency guard: %w", translateMongoError(ctx, replaceErr))
			}
			continue
		}
		if err := ensureMongoMigrationIdentityAbsent(ctx, backend.database.Collection(mongoTaskConcurrencyCollectionName), updatedGuard.ID, "scheduled-publish concurrency guard"); err != nil {
			return err
		}
		deleted, deleteErr := backend.database.Collection(mongoTaskConcurrencyCollectionName).DeleteOne(ctx, bson.D{{Key: "_id", Value: guard.ID}, {Key: "queue", Value: guard.Queue}, {Key: "key", Value: guard.Key}})
		if deleteErr != nil {
			return fmt.Errorf("delete source scheduled-publish concurrency guard: %w", translateMongoError(ctx, deleteErr))
		}
		if deleted.DeletedCount != 1 {
			return fmt.Errorf("source scheduled-publish concurrency guard changed during semantic rewrite")
		}
		if _, insertErr := backend.database.Collection(mongoTaskConcurrencyCollectionName).InsertOne(ctx, encoded); insertErr != nil {
			return fmt.Errorf("insert rewritten scheduled-publish concurrency guard: %w", translateMongoError(ctx, insertErr))
		}
	}

	for _, rewrite := range rewrites {
		encoded, encodeErr := encodeMongoTask(rewrite.after)
		if encodeErr != nil {
			return fmt.Errorf("encode scheduled-publish task %s: %w", rewrite.before.ID, encodeErr)
		}
		replaced, replaceErr := backend.database.Collection(mongoTaskCollectionName).ReplaceOne(ctx, bson.D{{Key: "_id", Value: rewrite.before.ID}}, encoded)
		if replaceErr != nil {
			return fmt.Errorf("replace scheduled-publish task %s: %w", rewrite.before.ID, translateMongoError(ctx, replaceErr))
		}
		if replaced.MatchedCount != 1 {
			return fmt.Errorf("scheduled-publish task %s changed during semantic rewrite", rewrite.before.ID)
		}
	}
	return nil
}

func ensureMongoMigrationIdentityAbsent(ctx context.Context, collection *mongo.Collection, id, kind string) error {
	err := collection.FindOne(ctx, bson.D{{Key: "_id", Value: id}}).Err()
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect target %s identity: %w", kind, translateMongoError(ctx, err))
	}
	return fmt.Errorf("target %s identity already exists", kind)
}

func mongoScheduledPublishTaskMentionsCollection(task store.Task, collectionID schema.StableID) bool {
	if task.Slug != mongoScheduledPublishTaskSlug {
		return false
	}
	if task.Target != nil && task.Target.CollectionID == collectionID || task.RequestedBy != nil && task.RequestedBy.CollectionID == collectionID {
		return true
	}
	var input map[string]json.RawMessage
	if json.Unmarshal(task.Input, &input) != nil || input == nil {
		return false
	}
	for _, key := range []string{"collectionID", "requestedByCollectionID"} {
		var value string
		if encoded, exists := input[key]; exists && json.Unmarshal(encoded, &value) == nil && value == string(collectionID) {
			return true
		}
	}
	return false
}

func rewriteMongoScheduledPublishTaskCollectionID(task store.Task, before, after schema.StableID) (store.Task, bool, error) {
	var input map[string]json.RawMessage
	if err := json.Unmarshal(task.Input, &input); err != nil || input == nil {
		if err == nil {
			err = fmt.Errorf("input must be a JSON object")
		}
		return store.Task{}, false, err
	}
	readRequired := func(key string) (string, error) {
		var value string
		encoded, exists := input[key]
		if !exists || json.Unmarshal(encoded, &value) != nil || value == "" {
			return "", fmt.Errorf("input.%s must be a non-empty string", key)
		}
		return value, nil
	}
	collectionID, err := readRequired("collectionID")
	if err != nil {
		return store.Task{}, false, err
	}
	documentID, err := readRequired("documentID")
	if err != nil {
		return store.Task{}, false, err
	}
	if task.Target == nil || string(task.Target.CollectionID) != collectionID || task.Target.DocumentID != documentID {
		return store.Task{}, false, fmt.Errorf("target reference does not match built-in input")
	}
	requestedCollection, requestedCollectionPresent, err := mongoMigrationOptionalJSONString(input, "requestedByCollectionID")
	if err != nil {
		return store.Task{}, false, err
	}
	requestedDocument, requestedDocumentPresent, err := mongoMigrationOptionalJSONString(input, "requestedByUserID")
	if err != nil {
		return store.Task{}, false, err
	}
	if requestedCollectionPresent != requestedDocumentPresent || requestedCollectionPresent != (task.RequestedBy != nil) ||
		(requestedCollectionPresent && (string(task.RequestedBy.CollectionID) != requestedCollection || task.RequestedBy.DocumentID != requestedDocument)) {
		return store.Task{}, false, fmt.Errorf("requester reference does not match built-in input")
	}

	updated := cloneMongoTask(task)
	changed := false
	if collectionID == string(before) {
		collectionID = string(after)
		input["collectionID"], _ = json.Marshal(collectionID)
		updated.Target.CollectionID = after
		updated.ConcurrencyKey = mongoScheduledPublishConcurrencyKey(after, documentID)
		changed = true
	}
	if requestedCollectionPresent && requestedCollection == string(before) {
		input["requestedByCollectionID"], _ = json.Marshal(string(after))
		updated.RequestedBy.CollectionID = after
		changed = true
	}
	if !changed {
		return task, false, nil
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return store.Task{}, false, err
	}
	updated.Input = encoded
	return updated, true, nil
}

func mongoMigrationOptionalJSONString(input map[string]json.RawMessage, key string) (string, bool, error) {
	encoded, exists := input[key]
	if !exists {
		return "", false, nil
	}
	var value *string
	if err := json.Unmarshal(encoded, &value); err != nil {
		return "", false, fmt.Errorf("input.%s must be a string or null", key)
	}
	if value == nil {
		return "", false, nil
	}
	if *value == "" {
		return "", false, fmt.Errorf("input.%s must be a non-empty string when present", key)
	}
	return *value, true, nil
}

func mongoScheduledPublishConcurrencyKey(collectionID schema.StableID, documentID string) string {
	key := string(collectionID) + ":" + documentID
	if len(key) <= store.MaxTaskConcurrencyKeyBytes {
		return key
	}
	digest := sha256.Sum256([]byte(key))
	return "sha256-" + hex.EncodeToString(digest[:])
}

func (backend *Store) rewriteMongoIdentityDerivedState(ctx context.Context, before, after schema.StableID) error {
	type rewriteSpec struct {
		collection string
		decode     func(bson.Raw) (any, error)
		encode     func(any) (bson.D, error)
	}
	specs := []rewriteSpec{
		{mongoAuthCredentialCollectionName, func(raw bson.Raw) (any, error) {
			value, err := decodeMongoAuthCredential(raw)
			value.CollectionID = after
			return value, err
		}, func(value any) (bson.D, error) { return encodeMongoAuthCredential(value.(mongoAuthCredential)) }},
		{mongoAuthTokenCollectionName, func(raw bson.Raw) (any, error) {
			value, err := decodeMongoAuthToken(raw)
			value.Token.CollectionID = after
			return value, err
		}, func(value any) (bson.D, error) { return encodeMongoAuthToken(value.(mongoAuthToken)) }},
		{mongoAuthBootstrapCollectionName, func(raw bson.Raw) (any, error) {
			value, err := decodeMongoAuthBootstrapGuard(raw)
			value.CollectionID = after
			return value, err
		}, func(value any) (bson.D, error) { return encodeMongoAuthBootstrapGuard(value.(mongoAuthBootstrapGuard)) }},
		{mongoPreferenceCollectionName, func(raw bson.Raw) (any, error) {
			value, err := decodeMongoPreference(raw)
			value.CollectionID = after
			return value, err
		}, func(value any) (bson.D, error) { return encodeMongoPreference(value.(store.Preference)) }},
		{mongoDocumentLockCollectionName, func(raw bson.Raw) (any, error) {
			value, err := decodeMongoDocumentLock(raw)
			if value.CollectionID == before {
				value.CollectionID = after
			}
			if value.OwnerCollectionID == before {
				value.OwnerCollectionID = after
			}
			return value, err
		}, func(value any) (bson.D, error) { return encodeMongoDocumentLock(value.(store.DocumentLock)) }},
	}
	for _, spec := range specs {
		filter := bson.D{{Key: "$or", Value: bson.A{bson.D{{Key: "collection", Value: string(before)}}, bson.D{{Key: "ownerCollection", Value: string(before)}}}}}
		if spec.collection != mongoDocumentLockCollectionName {
			filter = bson.D{{Key: "collection", Value: string(before)}}
		}
		cursor, err := backend.database.Collection(spec.collection).Find(ctx, filter)
		if err != nil {
			return fmt.Errorf("inspect %s: %w", spec.collection, translateMongoError(ctx, err))
		}
		for cursor.Next(ctx) {
			oldID, ok := cursor.Current.Lookup("_id").StringValueOK()
			if !ok {
				_ = cursor.Close(ctx)
				return fmt.Errorf("stored MongoDB framework row has invalid identity")
			}
			value, decodeErr := spec.decode(cursor.Current)
			if decodeErr != nil {
				_ = cursor.Close(ctx)
				return fmt.Errorf("decode %s: %w", spec.collection, decodeErr)
			}
			document, encodeErr := spec.encode(value)
			if encodeErr != nil {
				_ = cursor.Close(ctx)
				return fmt.Errorf("encode %s: %w", spec.collection, encodeErr)
			}
			newID := ""
			for _, element := range document {
				if element.Key == "_id" {
					newID, _ = element.Value.(string)
					break
				}
			}
			if newID == "" {
				_ = cursor.Close(ctx)
				return fmt.Errorf("rewritten MongoDB framework row has invalid identity")
			}
			if newID == oldID {
				if _, err := backend.database.Collection(spec.collection).ReplaceOne(ctx, bson.D{{Key: "_id", Value: oldID}}, document); err != nil {
					_ = cursor.Close(ctx)
					return fmt.Errorf("replace %s identity: %w", spec.collection, translateMongoError(ctx, err))
				}
			} else {
				deleted, err := backend.database.Collection(spec.collection).DeleteOne(ctx, bson.D{{Key: "_id", Value: oldID}})
				if err != nil {
					_ = cursor.Close(ctx)
					return fmt.Errorf("delete source %s identity: %w", spec.collection, translateMongoError(ctx, err))
				}
				if deleted.DeletedCount != 1 {
					_ = cursor.Close(ctx)
					return fmt.Errorf("source %s identity changed during semantic rewrite", spec.collection)
				}
				if _, err := backend.database.Collection(spec.collection).InsertOne(ctx, document); err != nil {
					_ = cursor.Close(ctx)
					return fmt.Errorf("insert rewritten %s identity: %w", spec.collection, translateMongoError(ctx, err))
				}
			}
		}
		if err := cursor.Err(); err != nil {
			_ = cursor.Close(ctx)
			return fmt.Errorf("scan %s: %w", spec.collection, translateMongoError(ctx, err))
		}
		_ = cursor.Close(ctx)
	}
	return nil
}

func (backend *Store) rebuildMongoMigrationReferences(ctx context.Context, transaction *documentTransaction, manifest schema.Manifest) error {
	sessionContext, leave, err := transaction.enter(ctx, true)
	if err != nil {
		return err
	}
	defer leave()
	if _, err := backend.database.Collection(mongoReferenceCollectionName).DeleteMany(sessionContext, bson.D{}); err != nil {
		return translateMongoError(sessionContext, err)
	}
	for _, resource := range append(append([]schema.Collection(nil), manifest.Snapshot().Collections...), manifest.Snapshot().Globals...) {
		cursor, err := backend.database.Collection(physicalCollectionName(resource.ID)).Find(sessionContext, bson.D{})
		if err != nil {
			return translateMongoError(sessionContext, err)
		}
		for cursor.Next(sessionContext) {
			document, decodeErr := decodeCollectionDocument(cursor.Current, resource)
			if decodeErr != nil {
				_ = cursor.Close(sessionContext)
				return decodeErr
			}
			for _, entry := range referenceindex.Collect(resource, document) {
				encoded, encodeErr := encodeMongoReferenceEntry(entry)
				if encodeErr != nil {
					_ = cursor.Close(sessionContext)
					return encodeErr
				}
				if _, insertErr := backend.database.Collection(mongoReferenceCollectionName).InsertOne(sessionContext, encoded); insertErr != nil {
					_ = cursor.Close(sessionContext)
					return translateMongoError(sessionContext, insertErr)
				}
			}
		}
		if err := cursor.Err(); err != nil {
			_ = cursor.Close(sessionContext)
			return translateMongoError(sessionContext, err)
		}
		_ = cursor.Close(sessionContext)
	}
	return nil
}
