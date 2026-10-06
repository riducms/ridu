// Package datatransform is the migration.DataTransaction that every official
// adapter gives a compiled data transform. It admits each request against the
// exact before and after manifests of the running artifact, performs updates
// from the stored document it locks itself, and removes the framework state a
// deleted document owns. Adapters supply only their document operations.
package datatransform

import (
	"context"
	"fmt"
	"reflect"

	"github.com/riducms/ridu/internal/embedded"
	"github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// Documents is the adapter transaction a data transform runs in.
type Documents interface {
	Create(context.Context, store.CreateRequest) (store.Document, error)
	Find(context.Context, store.Request) (store.Document, error)
	List(context.Context, store.Request) (store.Page, error)
	Update(context.Context, store.UpdateRequest) (store.Document, error)
	Trash(context.Context, store.Request) (store.Document, error)
	Restore(context.Context, store.Request) (store.Document, error)
	Delete(context.Context, store.Request) (store.Document, error)
	DeleteDocumentState(context.Context, store.DocumentReference) error
}

// Options adapt the transaction to one adapter.
type Options struct {
	// Engine names the adapter in errors, such as "PostgreSQL".
	Engine string
	// Admit, when set, runs after validation and before each operation with
	// the admitted resource and its locales.
	Admit func(schema.Collection, []schema.LocaleCode) error
}

// Transaction implements migration.DataTransaction over Documents.
type Transaction struct {
	before         *schema.Snapshot
	after          schema.Snapshot
	documents      Documents
	engine         string
	admit          func(schema.Collection, []schema.LocaleCode) error
	versioned      map[schema.StableID]struct{}
	resourceShapes map[schema.StableID][]schema.Collection
	locales        map[string]struct{}
}

var _ migration.DataTransaction = (*Transaction)(nil)

// New returns the data transaction for one artifact's transform.
func New(documents Documents, artifact migration.Artifact, options Options) *Transaction {
	transaction := &Transaction{
		before: artifact.Before, after: artifact.After,
		documents: documents, engine: options.Engine, admit: options.Admit,
		versioned:      make(map[schema.StableID]struct{}),
		resourceShapes: make(map[schema.StableID][]schema.Collection),
		locales:        make(map[string]struct{}),
	}
	if artifact.Before != nil {
		transaction.collect(*artifact.Before)
	}
	transaction.collect(artifact.After)
	return transaction
}

func (transaction *Transaction) collect(snapshot schema.Snapshot) {
	if snapshot.Application.Localization != nil {
		for _, locale := range snapshot.Application.Localization.Locales {
			transaction.locales[string(locale.Code)] = struct{}{}
		}
	}
	resources := append(append([]schema.Collection(nil), snapshot.Collections...), snapshot.Globals...)
	for _, resource := range resources {
		if resource.Versions != nil || resource.Capabilities.Versions {
			transaction.versioned[resource.ID] = struct{}{}
		}
		known := false
		for _, shape := range transaction.resourceShapes[resource.ID] {
			if reflect.DeepEqual(shape, resource) {
				known = true
				break
			}
		}
		if !known {
			transaction.resourceShapes[resource.ID] = append(transaction.resourceShapes[resource.ID], resource)
		}
	}
}

// Collection returns the collection with slug from the artifact's after
// manifest, or from its before manifest when the artifact removes it.
func (transaction *Transaction) Collection(slug string) (schema.Collection, error) {
	return transaction.resource("collection", slug, func(snapshot schema.Snapshot) []schema.Collection {
		return snapshot.Collections
	})
}

// Global returns the global with slug, resolved like Collection.
func (transaction *Transaction) Global(slug string) (schema.Collection, error) {
	return transaction.resource("global", slug, func(snapshot schema.Snapshot) []schema.Collection {
		return snapshot.Globals
	})
}

func (transaction *Transaction) resource(kind, slug string, resources func(schema.Snapshot) []schema.Collection) (schema.Collection, error) {
	snapshots := []schema.Snapshot{transaction.after}
	if transaction.before != nil {
		snapshots = append(snapshots, *transaction.before)
	}
	for _, snapshot := range snapshots {
		for _, resource := range resources(snapshot) {
			if string(resource.Slug) == slug {
				return resource, nil
			}
		}
	}
	return schema.Collection{}, fmt.Errorf("%s data transform %s %q is not in this migration's before or after manifest", transaction.engine, kind, slug)
}

func (transaction *Transaction) Create(ctx context.Context, request store.CreateRequest) (store.Document, error) {
	if err := transaction.requireMutation(request.Collection, request.Values); err != nil {
		return store.Document{}, err
	}
	if err := transaction.requireLocales(request.Locales); err != nil {
		return store.Document{}, err
	}
	if err := transaction.runAdmit(request.Collection, request.Locales); err != nil {
		return store.Document{}, err
	}
	return transaction.documents.Create(ctx, request)
}

func (transaction *Transaction) Find(ctx context.Context, request store.Request) (store.Document, error) {
	if err := transaction.requireRead(request); err != nil {
		return store.Document{}, err
	}
	return transaction.documents.Find(ctx, request)
}

func (transaction *Transaction) List(ctx context.Context, request store.Request) (store.Page, error) {
	if err := transaction.requireRead(request); err != nil {
		return store.Page{}, err
	}
	return transaction.documents.List(ctx, request)
}

// Update locks and reads the whole stored document, as the operation engine
// does, and applies the request to it. A transform cannot supply the current
// document, so a projected or edited copy can never become stored state.
func (transaction *Transaction) Update(ctx context.Context, request migration.UpdateRequest) (store.Document, error) {
	if err := transaction.requireMutation(request.Collection, request.Values); err != nil {
		return store.Document{}, err
	}
	if err := transaction.requireRead(request.Request); err != nil {
		return store.Document{}, err
	}
	current, err := transaction.documents.Find(ctx, request.Request.CurrentRequest())
	if err != nil {
		return store.Document{}, err
	}
	return transaction.documents.Update(ctx, store.UpdateRequest{
		Request: request.Request, Values: request.Values, ReplaceValues: request.ReplaceValues, Current: &current,
	})
}

func (transaction *Transaction) Trash(ctx context.Context, request store.Request) (store.Document, error) {
	if err := transaction.requireWrite(request); err != nil {
		return store.Document{}, err
	}
	return transaction.documents.Trash(ctx, request)
}

func (transaction *Transaction) Restore(ctx context.Context, request store.Request) (store.Document, error) {
	if err := transaction.requireWrite(request); err != nil {
		return store.Document{}, err
	}
	return transaction.documents.Restore(ctx, request)
}

// Delete removes the document and, like an operation-engine delete, the
// framework state it owns, so a document later created with the same ID
// inherits none of it.
func (transaction *Transaction) Delete(ctx context.Context, request store.Request) (store.Document, error) {
	if err := transaction.requireWrite(request); err != nil {
		return store.Document{}, err
	}
	deleted, err := transaction.documents.Delete(ctx, request)
	if err != nil {
		return store.Document{}, err
	}
	if err := transaction.documents.DeleteDocumentState(ctx, store.DocumentReference{CollectionID: request.Collection.ID, DocumentID: deleted.ID}); err != nil {
		return store.Document{}, err
	}
	return deleted, nil
}

func (transaction *Transaction) requireWrite(request store.Request) error {
	if err := transaction.requireMutation(request.Collection, nil); err != nil {
		return err
	}
	return transaction.requireRead(request)
}

func (transaction *Transaction) requireRead(request store.Request) error {
	if err := transaction.requireResource(request.Collection); err != nil {
		return err
	}
	if err := transaction.requireLocales(request.Locales); err != nil {
		return err
	}
	if err := transaction.requireLocales(request.LocaleChain); err != nil {
		return err
	}
	for id, collection := range request.Collections {
		if id != collection.ID {
			return fmt.Errorf("%s data transform collection map key %q does not match immutable resource %q", transaction.engine, id, collection.ID)
		}
		if id == request.Collection.ID && !reflect.DeepEqual(collection, request.Collection) {
			return fmt.Errorf("%s data transform resource %q uses inconsistent immutable shapes in one request", transaction.engine, id)
		}
		if err := transaction.requireResource(collection); err != nil {
			return err
		}
	}
	return transaction.runAdmit(request.Collection, request.Locales)
}

func (transaction *Transaction) runAdmit(collection schema.Collection, locales []schema.LocaleCode) error {
	if transaction.admit == nil {
		return nil
	}
	return transaction.admit(collection, locales)
}

func (transaction *Transaction) requireMutation(collection schema.Collection, values store.Values) error {
	if err := transaction.requireResource(collection); err != nil {
		return err
	}
	if _, versioned := transaction.versioned[collection.ID]; versioned {
		return fmt.Errorf("%s data transforms cannot mutate versioned resource %q until retained snapshots can be rewritten atomically", transaction.engine, collection.ID)
	}
	if values != nil {
		return transaction.validateValues(collection.Fields, values, string(collection.ID), nil)
	}
	return nil
}

func (transaction *Transaction) requireResource(collection schema.Collection) error {
	shapes := transaction.resourceShapes[collection.ID]
	if len(shapes) == 0 {
		return fmt.Errorf("%s data transform resource %q is outside the immutable artifact manifests", transaction.engine, collection.ID)
	}
	for _, shape := range shapes {
		if reflect.DeepEqual(shape, collection) {
			return nil
		}
	}
	return fmt.Errorf("%s data transform resource %q must exactly match its immutable before or after manifest shape", transaction.engine, collection.ID)
}

func (transaction *Transaction) requireLocales(locales []schema.LocaleCode) error {
	for _, locale := range locales {
		if _, allowed := transaction.locales[string(locale)]; !allowed {
			return fmt.Errorf("%s data transform locale %q is outside the immutable manifests", transaction.engine, locale)
		}
	}
	return nil
}

func (transaction *Transaction) validateValues(fields []schema.Field, values store.Values, path string, special map[string]struct{}) error {
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
			return fmt.Errorf("%s data transform value %q is outside the immutable resource shape", transaction.engine, path+"."+name)
		}
		if err := transaction.validateValue(field, value, path+"."+name); err != nil {
			return err
		}
	}
	return nil
}

func (transaction *Transaction) validateValue(field schema.Field, value store.Value, path string) error {
	if value.Kind() == store.ValueNull {
		return nil
	}
	if field.Localized {
		localized, valid := value.CopyObject()
		if !valid {
			return fmt.Errorf("%s data transform localized value %q must be an object", transaction.engine, path)
		}
		field.Localized = false
		for locale, localizedValue := range localized {
			if _, allowed := transaction.locales[locale]; !allowed {
				return fmt.Errorf("%s data transform locale %q at %q is outside the immutable manifests", transaction.engine, locale, path)
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
			return fmt.Errorf("%s data transform group value %q must match its immutable resource shape", transaction.engine, path)
		}
		return transaction.validateValues(field.Nested.ResolvedFields(), object, path, nil)
	case schema.FieldTypeArray:
		items, valid := value.CopyList()
		if !valid || field.Nested == nil {
			return fmt.Errorf("%s data transform array value %q must match its immutable resource shape", transaction.engine, path)
		}
		special := map[string]struct{}{"_key": {}}
		for index, item := range items {
			object, valid := item.CopyObject()
			if !valid {
				return fmt.Errorf("%s data transform array row %q must be an object", transaction.engine, fmt.Sprintf("%s.%d", path, index))
			}
			if err := transaction.validateValues(field.Nested.ResolvedFields(), object, fmt.Sprintf("%s.%d", path, index), special); err != nil {
				return err
			}
		}
	case schema.FieldTypeBlocks:
		items, valid := value.CopyList()
		if !valid || field.Blocks == nil {
			return fmt.Errorf("%s data transform blocks value %q must match its immutable resource shape", transaction.engine, path)
		}
		if len(items) < field.Blocks.MinRows || field.Blocks.MaxRows > 0 && len(items) > field.Blocks.MaxRows {
			return fmt.Errorf("%s data transform blocks value %q violates its row bounds", transaction.engine, path)
		}
		special := map[string]struct{}{"_key": {}, "blockType": {}}
		for index, item := range items {
			itemPath := fmt.Sprintf("%s.%d", path, index)
			object, valid := item.CopyObject()
			if !valid {
				return fmt.Errorf("%s data transform block %q must be an object", transaction.engine, itemPath)
			}
			blockKey, valid := object["blockType"].StringValue()
			if !valid {
				return fmt.Errorf("%s data transform block %q must name an immutable block type", transaction.engine, itemPath)
			}
			// Values need only the field structure every placement shares.
			block, found := field.Blocks.Definition(blockKey)
			blockFields := block.ResolvedFields()
			if !found {
				return fmt.Errorf("%s data transform block type %q at %q is outside the immutable resource shape", transaction.engine, blockKey, itemPath)
			}
			if err := transaction.validateValues(blockFields, object, itemPath, special); err != nil {
				return err
			}
		}
	}
	return nil
}
