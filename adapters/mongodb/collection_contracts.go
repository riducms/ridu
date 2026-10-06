package mongodb

import (
	"sync"
	"unsafe"

	"github.com/riducms/ridu/schema"
)

// mongoCollectionContracts remembers, for each collection ID, the resolved
// collection value whose adapter contract last validated, with the facts the
// request path derives from its schema. Validation walks every field
// placement, which for a block-heavy schema is tens of thousands of fields;
// repeating it per request dominated read CPU.
//
// A resolved manifest is immutable, so a collection value is identified by
// its top-level settings and the identity of its field and index arrays: every
// copy of one manifest's collection shares them. Entries hold those arrays, so
// an address cannot be reused while it is remembered, and one entry per ID
// bounds what a retired manifest can keep reachable. A collection value from
// another manifest replaces the entry after validating; alternating manifests
// only revalidate.
type mongoCollectionContracts struct {
	mu      sync.RWMutex
	entries map[schema.StableID]mongoCollectionContract
}

type mongoCollectionContract struct {
	identity mongoCollectionIdentity
	// hasRelationships reports a relationship or upload field at any depth,
	// so writes maintain the reference index.
	hasRelationships bool
}

type mongoCollectionIdentity struct {
	id           schema.StableID
	slug         schema.CollectionSlug
	capabilities schema.Capabilities
	fields       *schema.Field
	fieldCount   int
	indexes      *schema.CollectionIndex
	indexCount   int
	auth         *schema.AuthSettings
	upload       *schema.UploadSettings
	versions     *schema.VersionSettings
	documentLock *schema.DocumentLockSettings
}

func mongoCollectionIdentityOf(collection schema.Collection) mongoCollectionIdentity {
	return mongoCollectionIdentity{
		id: collection.ID, slug: collection.Slug, capabilities: collection.Capabilities,
		fields: unsafe.SliceData(collection.Fields), fieldCount: len(collection.Fields),
		indexes: unsafe.SliceData(collection.Indexes), indexCount: len(collection.Indexes),
		auth: collection.Auth, upload: collection.Upload,
		versions: collection.Versions, documentLock: collection.DocumentLock,
	}
}

// collectionContract validates collection once per resolved value and returns
// its derived request facts.
func (backend *Store) collectionContract(collection schema.Collection) (mongoCollectionContract, error) {
	identity := mongoCollectionIdentityOf(collection)
	contracts := &backend.contracts
	contracts.mu.RLock()
	contract, found := contracts.entries[collection.ID]
	contracts.mu.RUnlock()
	if found && contract.identity == identity {
		return contract, nil
	}
	if err := validateCollectionEnvelope(collection); err != nil {
		return mongoCollectionContract{}, err
	}
	contract = mongoCollectionContract{identity: identity, hasRelationships: mongoCollectionHasRelationships(collection)}
	contracts.mu.Lock()
	if contracts.entries == nil {
		contracts.entries = make(map[schema.StableID]mongoCollectionContract)
	}
	contracts.entries[collection.ID] = contract
	contracts.mu.Unlock()
	return contract, nil
}

// validateCollectionEnvelope is validateCollectionEnvelope for the request
// path, remembered per resolved collection value.
func (backend *Store) validateCollectionEnvelope(collection schema.Collection) error {
	_, err := backend.collectionContract(collection)
	return err
}

// collectionHasRelationships is mongoCollectionHasRelationships for a
// validated request collection.
func (backend *Store) collectionHasRelationships(collection schema.Collection) (bool, error) {
	contract, err := backend.collectionContract(collection)
	return contract.hasRelationships, err
}
