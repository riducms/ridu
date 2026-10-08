package mongodb

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/riducms/ridu/internal/fieldchange"
	"github.com/riducms/ridu/internal/requiredfield"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const mongoDevelopmentSchemaCollection = "z_ridu_schema"
const mongoDevelopmentSchemaID = "last-synchronized"

func unknownMongoDevelopmentSchema() error {
	return fmt.Errorf("RIDU_DEVELOPMENT_SCHEMA_UNKNOWN: MongoDB has existing collections but no last-synchronized schema record, so the schema that produced its stored content is unknown; restore a database backup with its schema record, or preserve any needed content and use a new dedicated development database; generated files, .ridu caches and physical indexes cannot establish the previous schema")
}

// DevelopmentManifest returns the database-owned last synchronized schema.
// Existing indexes or JSON documents do not prove the previous field kinds, so
// collections without a record are an error; a fresh database has none.
func (backend *Store) DevelopmentManifest(ctx context.Context) (schema.Manifest, bool, error) {
	if err := backend.prepareIndexOperation(ctx); err != nil {
		return schema.Manifest{}, false, err
	}
	if err := backend.verifyMongoMigrationLedgerPhysicalContract(ctx); err != nil {
		return schema.Manifest{}, false, err
	}
	state, err := backend.readMongoMigrationLedgerState(ctx)
	if err != nil {
		return schema.Manifest{}, false, err
	}
	applied := make(map[string]string, len(state.artifacts))
	for _, artifact := range state.artifacts {
		applied[artifact.Name] = artifact.Digest
	}
	for _, step := range state.steps {
		if step.State != mongoMigrationStepComplete || applied[step.ArtifactName] != step.ArtifactDigest {
			return schema.Manifest{}, false, fmt.Errorf("RIDU_DEVELOPMENT_SCHEMA_UNKNOWN: MongoDB migration work is incomplete; finish it using the original immutable migration history before development synchronization")
		}
	}
	manifest, err := backend.readMongoDevelopmentManifest(ctx)
	if err != nil {
		return schema.Manifest{}, false, err
	}
	if manifest != nil {
		return *manifest, true, nil
	}
	names, err := backend.database.ListCollectionNames(ctx, bson.D{})
	if err != nil {
		return schema.Manifest{}, false, translateMongoError(ctx, err)
	}
	for _, name := range names {
		if name != mongoMigrationLeaseCollectionName && name != mongoDevelopmentSchemaCollection {
			return schema.Manifest{}, false, unknownMongoDevelopmentSchema()
		}
	}
	return schema.Manifest{}, false, nil
}

func (backend *Store) readMongoDevelopmentManifest(ctx context.Context) (*schema.Manifest, error) {
	raw, err := backend.mongoMigrationCollection(mongoDevelopmentSchemaCollection).FindOne(ctx, bson.D{{Key: "_id", Value: mongoDevelopmentSchemaID}}).Raw()
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, translateMongoError(ctx, err)
	}
	count, err := backend.mongoMigrationCollection(mongoDevelopmentSchemaCollection).CountDocuments(ctx, bson.D{}, options.Count().SetLimit(2))
	if err != nil {
		return nil, translateMongoError(ctx, err)
	}
	if count != 1 {
		return nil, fmt.Errorf("RIDU_DEVELOPMENT_SCHEMA_UNKNOWN: MongoDB synchronized schema must contain exactly one authoritative record")
	}
	manifest, err := decodeMongoDevelopmentManifest(raw)
	if err != nil {
		return nil, fmt.Errorf("RIDU_DEVELOPMENT_SCHEMA_UNKNOWN: invalid recorded MongoDB manifest: %w", err)
	}
	return &manifest, nil
}

// The schema record stores its manifest as gzip-compressed compact JSON. The
// manifest stores each block definition once, so it grows with the schema's
// declarations; compression keeps even very large schemas far below
// MongoDB's document limit.
const (
	mongoSchemaRecordManifestField = "manifestJSONGzip"
	// mongoMaxSchemaRecordManifestBytes leaves the record's identity and
	// digest room within MongoDB's 16 MiB document limit.
	mongoMaxSchemaRecordManifestBytes = mongoMaxDocumentBytes - 4*1024
	// mongoMaxSchemaRecordJSONBytes bounds decompression of a stored record.
	mongoMaxSchemaRecordJSONBytes = 1 << 30
)

func decodeMongoDevelopmentManifest(raw bson.Raw) (schema.Manifest, error) {
	if err := requireExactKeys(raw, "MongoDB synchronized schema", "_id", mongoSchemaRecordManifestField, "manifestDigest"); err != nil {
		return schema.Manifest{}, err
	}
	id, idOK := raw.Lookup("_id").StringValueOK()
	subtype, compressed, compressedOK := raw.Lookup(mongoSchemaRecordManifestField).BinaryOK()
	recordedDigest, digestOK := raw.Lookup("manifestDigest").StringValueOK()
	if !idOK || id != mongoDevelopmentSchemaID || !compressedOK || subtype != bson.TypeBinaryGeneric || !digestOK {
		return schema.Manifest{}, fmt.Errorf("stored MongoDB synchronized schema has invalid identity or field types")
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return schema.Manifest{}, fmt.Errorf("stored MongoDB synchronized schema is not gzip-compressed: %w", err)
	}
	encoded, err := io.ReadAll(io.LimitReader(reader, mongoMaxSchemaRecordJSONBytes+1))
	if err == nil {
		err = reader.Close()
	}
	if err != nil {
		return schema.Manifest{}, fmt.Errorf("decompress stored MongoDB synchronized schema: %w", err)
	}
	if len(encoded) > mongoMaxSchemaRecordJSONBytes {
		return schema.Manifest{}, fmt.Errorf("stored MongoDB synchronized schema exceeds %d bytes of JSON", mongoMaxSchemaRecordJSONBytes)
	}
	manifest, err := schema.ParseHistorical(encoded)
	if err != nil {
		return schema.Manifest{}, err
	}
	digest, err := ridumigration.DigestManifest(manifest)
	if err != nil || digest != recordedDigest {
		return schema.Manifest{}, fmt.Errorf("stored MongoDB synchronized schema digest does not match its canonical manifest")
	}
	return manifest, nil
}

// encodeMongoDevelopmentManifest compresses the manifest for the schema
// record and rejects one MongoDB cannot store before anything is written.
func encodeMongoDevelopmentManifest(manifest schema.Manifest) ([]byte, error) {
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return nil, err
	}
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(encoded); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	if compressed.Len() > mongoMaxSchemaRecordManifestBytes {
		return nil, fmt.Errorf(
			"MongoDB cannot record this schema: its manifest is %.1f MiB of JSON and %.1f MiB compressed, over MongoDB's 16 MiB document limit; split the application's collections, globals or block definitions across smaller schemas",
			float64(len(encoded))/(1<<20), float64(compressed.Len())/(1<<20),
		)
	}
	return compressed.Bytes(), nil
}

func (backend *Store) writeMongoDevelopmentManifest(ctx context.Context, manifest schema.Manifest) error {
	compressed, err := encodeMongoDevelopmentManifest(manifest)
	if err != nil {
		return err
	}
	digest, err := ridumigration.DigestManifest(manifest)
	if err != nil {
		return err
	}
	_, err = backend.mongoMigrationCollection(mongoDevelopmentSchemaCollection).ReplaceOne(ctx,
		bson.D{{Key: "_id", Value: mongoDevelopmentSchemaID}}, bson.D{
			{Key: "_id", Value: mongoDevelopmentSchemaID},
			{Key: mongoSchemaRecordManifestField, Value: bson.Binary{Subtype: bson.TypeBinaryGeneric, Data: compressed}},
			{Key: "manifestDigest", Value: digest},
		}, options.Replace().SetUpsert(true))
	return translateMongoError(ctx, err)
}

func (backend *Store) requireMongoDevelopmentManifest(ctx context.Context, expected schema.Manifest) error {
	manifest, err := backend.readMongoDevelopmentManifest(ctx)
	if err != nil {
		return err
	}
	if manifest == nil {
		return unknownMongoDevelopmentSchema()
	}
	if !manifest.SameStorage(expected) {
		return fmt.Errorf("recorded MongoDB schema differs from the requested migration head; keep the authoritative schema record and create matching reviewed history before baseline adoption or migration")
	}
	return nil
}

// SyncDevelopmentSchema verifies additive index sync under the migration lease,
// then records its manifest in a fenced transaction. Failed builds never
// certify the candidate schema.
func (backend *Store) SyncDevelopmentSchema(ctx context.Context, manifest schema.Manifest) error {
	normalized, err := normalizeMongoMigrationRunnerOptions(RunnerOptions{})
	if err != nil {
		return err
	}
	return backend.withMongoMigrationLease(ctx, normalized, func(runContext context.Context, lease *mongoMigrationLease) error {
		before, exists, err := backend.DevelopmentManifest(runContext)
		if err != nil {
			return err
		}
		var changes []fieldchange.Change
		var requirements []requiredfield.Requirement
		var enabling []schema.Collection
		if exists {
			enabling = ridumigration.VersionsEnabled(before.Snapshot(), manifest.Snapshot(), nil)
			changes = fieldchange.Detect(before.Snapshot(), manifest.Snapshot())
			requirements = requiredfield.Detect(before.Snapshot(), manifest.Snapshot(), requiredfield.Renames{})
		}
		// A resource that starts keeping versions must store no documents, and
		// stored values must fit a changed field kind and fill a field that
		// becomes required, both before index work and again when the schema
		// record is published.
		requireEmpty := func(sessionContext context.Context) error {
			if err := backend.requireMongoDevelopmentVersionsEmpty(sessionContext, enabling); err != nil {
				return err
			}
			if len(changes) != 0 {
				reports := fieldchange.Reports(changes)
				if err := backend.scanMongoFieldKinds(sessionContext, &documentTransaction{store: backend}, changes, reports, false, mongoManifestLocales(before)); err != nil {
					return err
				}
				if err := fieldchange.RequireEmpty(reports); err != nil {
					return err
				}
			}
			return backend.auditMongoRequiredValues(sessionContext, requirements, true)
		}
		if len(enabling) != 0 || len(changes) != 0 || len(requirements) != 0 {
			if err := lease.transaction(runContext, requireEmpty); err != nil {
				return err
			}
		}
		if exists {
			// The old manifest owns the active published-head layout. Check its
			// coverage before rebuilding candidate reservations, so a missing
			// live head cannot be silently treated as an unpublished document.
			previous := before.Snapshot()
			for _, collection := range append(previous.Collections, previous.Globals...) {
				if collection.Versions != nil {
					if err := backend.verifyMongoPublishedHeadCoverage(runContext, collection); err != nil {
						return err
					}
				}
			}
		}
		// Candidate unique declarations cover both working and published heads.
		// Rebuild while the lease fences writers, before admitting new indexes.
		if err := backend.rebuildMongoHeadReservations(runContext, manifest); err != nil {
			return fmt.Errorf("rebuild MongoDB active unique-head reservations: %w", err)
		}
		if err := backend.syncMongoIndexes(runContext, manifest); err != nil {
			return err
		}
		return lease.transaction(runContext, func(sessionContext context.Context) error {
			if err := requireEmpty(sessionContext); err != nil {
				return err
			}
			if err := backend.writeMongoDevelopmentManifest(sessionContext, manifest); err != nil {
				return err
			}
			return lease.refreshUnlocked(sessionContext, backend.mongoMigrationCollection(mongoMigrationLeaseCollectionName))
		})
	})
}
