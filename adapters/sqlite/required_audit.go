package sqlite

import (
	"context"
	"fmt"

	"github.com/riducms/ridu/internal/requiredfield"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// sqliteRequiredAuditScope names the rows a SQLite required-value audit
// reads. Drafts defer required fields, and publication validates them.
const sqliteRequiredAuditScope = "every stored document except draft working copies, including trashed documents and every published row"

// auditSQLiteRequiredValues refuses requirements that stored rows leave
// without a value: every working row of a resource without drafts, non-draft
// working rows of a resource with drafts, and every published row. The
// caller's write transaction keeps other writers out until it commits.
func auditSQLiteRequiredValues(ctx context.Context, runner sqlRunner, requirements []requiredfield.Requirement, development bool) error {
	if len(requirements) == 0 {
		return nil
	}
	var installed int
	if err := runner.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'ridu_documents'`).Scan(&installed); err != nil {
		return translateError(err)
	}
	if installed == 0 {
		return nil
	}
	audit := requiredfield.NewAudit(requirements)
	for _, group := range requiredfield.Groups(requirements) {
		resource := group.Resource
		// A working row of a resource with drafts holds a draft when its status
		// is draft or its published row has pending draft changes.
		working := `SELECT working.id, working.values_json FROM ridu_documents AS working WHERE working.collection_id = ?`
		if resource.Versions != nil && resource.Versions.Drafts {
			working += ` AND working.status <> 'draft' AND NOT EXISTS (SELECT 1 FROM ridu_published_documents AS published
  WHERE published.collection_id = working.collection_id AND published.id = working.id AND published.has_draft_changes = 1)`
		}
		queries := []string{working + ` ORDER BY working.id`}
		if resource.Versions != nil {
			queries = append(queries, `SELECT id, values_json FROM ridu_published_documents WHERE collection_id = ? ORDER BY id`)
		}
		for _, query := range queries {
			if err := scanSQLiteRequiredRows(ctx, runner, query, resource.ID, audit); err != nil {
				return fmt.Errorf("audit required values of %s: %w", resource.Slug, err)
			}
		}
	}
	return audit.Err(sqliteRequiredAuditScope, development)
}

func scanSQLiteRequiredRows(ctx context.Context, runner sqlRunner, query string, resource schema.StableID, audit *requiredfield.Audit) error {
	rows, err := runner.QueryContext(ctx, query, string(resource))
	if err != nil {
		return translateError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, encoded string
		if err := rows.Scan(&id, &encoded); err != nil {
			return translateError(err)
		}
		var values store.Values
		if err := values.UnmarshalJSON([]byte(encoded)); err != nil {
			return fmt.Errorf("decode document %s: %w", id, err)
		}
		audit.Inspect(resource, id, values)
	}
	return translateError(rows.Err())
}

// sqliteRequirements lists the fields an artifact newly requires. Confirmed
// field renames keep the requiredness they already had.
func sqliteRequirements(before, after schema.Manifest, renames []ridumigration.Rename) ([]requiredfield.Requirement, error) {
	bound := requiredfield.Renames{Fields: make(map[schema.StableID]schema.Field), Blocks: make(map[string]map[schema.StableID]schema.Field)}
	if len(renames) != 0 {
		resolved, err := sqliteFieldRenames(before, after, renames)
		if err != nil {
			return nil, err
		}
		for _, rename := range resolved {
			bindSQLiteRequiredRename(bound, rename.block, rename.after.ID, rename.before)
		}
	}
	return requiredfield.Detect(before.Snapshot(), after.Snapshot(), bound), nil
}

// bindSQLiteRequiredRename records that field keeps its requiredness as id,
// among a collection's fields or those of block.
func bindSQLiteRequiredRename(bound requiredfield.Renames, block string, id schema.StableID, field schema.Field) {
	if block == "" {
		bound.Fields[id] = field
		return
	}
	if bound.Blocks[block] == nil {
		bound.Blocks[block] = make(map[schema.StableID]schema.Field)
	}
	bound.Blocks[block][id] = field
}

// auditSQLiteRollbackRequiredValues audits the fields that rolling back an
// artifact requires again: the reverse of its forward transition, with its
// renames reversed.
func auditSQLiteRollbackRequiredValues(ctx context.Context, runner sqlRunner, artifact ridumigration.Artifact, current, target schema.Manifest) error {
	renames, err := sqliteArtifactRenames(artifact)
	if err != nil {
		return err
	}
	bound := requiredfield.Renames{Fields: make(map[schema.StableID]schema.Field), Blocks: make(map[string]map[schema.StableID]schema.Field)}
	if len(renames) != 0 {
		resolved, err := sqliteFieldRenames(target, current, renames)
		if err != nil {
			return err
		}
		for _, rename := range resolved {
			bindSQLiteRequiredRename(bound, rename.block, rename.before.ID, rename.after)
		}
	}
	return auditSQLiteRequiredValues(ctx, runner, requiredfield.Detect(current.Snapshot(), target.Snapshot(), bound), false)
}

// sqliteArtifactRequirements resolves an audit step against the artifact's
// after manifest.
func sqliteArtifactRequirements(artifact ridumigration.Artifact, payload ridumigration.AuditRequiredValuesPayload) ([]requiredfield.Requirement, error) {
	after, err := artifact.AfterManifest()
	if err != nil {
		return nil, err
	}
	return requiredfield.Resolve(after.Snapshot(), payload.Fields)
}
