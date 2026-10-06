package mongodb

import (
	"context"
	"fmt"

	"github.com/riducms/ridu/internal/requiredfield"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// mongoRequiredAuditScope names the documents a MongoDB required-value audit
// reads. Drafts defer required fields, and publication validates them.
const mongoRequiredAuditScope = "every stored document except draft working copies, including trashed documents and every published head"

// auditMongoRequiredValues refuses requirements that stored documents leave
// without a value: every working document of a resource without drafts,
// non-draft working documents of a resource with drafts, and every published
// head. ctx carries the caller's transaction session. MongoDB cannot lock the
// audited collections, so writers must be stopped while it runs.
func (backend *Store) auditMongoRequiredValues(ctx context.Context, requirements []requiredfield.Requirement, development bool) error {
	if len(requirements) == 0 {
		return nil
	}
	audit := requiredfield.NewAudit(requirements)
	for _, group := range requiredfield.Groups(requirements) {
		resource := group.Resource
		// A working document of a resource with drafts holds a draft when its
		// status is draft or it has pending changes over its published head.
		working := bson.D{}
		if resource.Versions != nil && resource.Versions.Drafts {
			working = bson.D{
				{Key: mongoStatusPath, Value: bson.D{{Key: "$ne", Value: string(store.StatusDraft)}}},
				{Key: "meta.pending", Value: bson.D{{Key: "$ne", Value: true}}},
			}
		}
		if err := backend.scanMongoRequiredValues(ctx, physicalCollectionName(resource.ID), working, resource.ID, group.Roots, audit); err != nil {
			return fmt.Errorf("audit required values of %s: %w", resource.Slug, err)
		}
		if resource.Versions != nil {
			if err := backend.scanMongoRequiredValues(ctx, physicalPublishedCollectionName(resource.ID), bson.D{}, resource.ID, group.Roots, audit); err != nil {
				return fmt.Errorf("audit required values of %s: %w", resource.Slug, err)
			}
		}
	}
	return audit.Err(mongoRequiredAuditScope, development)
}

func (backend *Store) scanMongoRequiredValues(ctx context.Context, name string, filter bson.D, resource schema.StableID, roots []schema.Field, audit *requiredfield.Audit) error {
	projection := bson.D{{Key: "_id", Value: 1}}
	for _, root := range roots {
		projection = append(projection, bson.E{Key: "values." + root.Name, Value: 1})
	}
	cursor, err := backend.database.Collection(name).Find(ctx, filter, options.Find().SetProjection(projection).SetSort(bson.D{{Key: "_id", Value: 1}}))
	if err != nil {
		return translateMongoError(ctx, err)
	}
	defer cursor.Close(ctx)
	for cursor.Next(ctx) {
		id, valid := cursor.Current.Lookup("_id").StringValueOK()
		if !valid {
			return fmt.Errorf("stored MongoDB document in %s has an invalid identity", name)
		}
		raw, valid := cursor.Current.Lookup("values").DocumentOK()
		if !valid {
			return fmt.Errorf("stored MongoDB document %s has invalid values", id)
		}
		values, err := decodeValues(raw)
		if err != nil {
			return err
		}
		audit.Inspect(resource, id, values)
	}
	return translateMongoError(ctx, cursor.Err())
}

// mongoArtifactRequirements resolves an audit step against the artifact's
// after manifest.
func mongoArtifactRequirements(artifact ridumigration.Artifact, payload ridumigration.AuditRequiredValuesPayload) ([]requiredfield.Requirement, error) {
	after, err := artifact.AfterManifest()
	if err != nil {
		return nil, err
	}
	return requiredfield.Resolve(after.Snapshot(), payload.Fields)
}
