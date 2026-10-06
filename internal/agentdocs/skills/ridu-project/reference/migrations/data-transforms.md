<!-- Generated from website/src/content/docs/migrations/data-transforms.md by scripts/sync-agent-docs.ts. -->

# Data transforms

Some schema changes need content rewritten too: filling in a field that becomes required, copying
values into a field of a new kind, or seeding documents. A data transform is a Go function compiled
into your application that a migration runs inside its own transaction, so the schema change and
the content change succeed or fail together.

```text title="A migration with a data transform" diagram
ridu migrate create --name require-summary --transform backfill-summaries
                                                        │
 migrations/…_require-summary.ridu.json                 │ names the
 └─ phase-001                                           │ transform
    ├─ step  data_transform  backfill-summaries  ◀──────┘
    └─ step  audit_required_values

ridu migrate up
 └─ runs your compiled backfill-summaries Up function,
    then the audit, in one transaction
```

## Write a transform {#write}

A transform has a name, a checksum and two functions. `Up` runs when the migration is applied;
`Down` runs when SQLite rolls it back. Both receive a `migration.DataTransaction` that reads and
writes documents inside the migration's transaction:

```go title="internal/transforms/transforms.go"
package transforms

import (
	"context"

	"github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/store"
)

// All lists every transform a committed migration may name.
func All() []migration.DataTransform {
	return []migration.DataTransform{backfillSummaries}
}

var backfillSummaries = migration.DataTransform{
	DataTransformDescriptor: migration.DataTransformDescriptor{
		Name: "backfill-summaries",
		// Change this when Up or Down behaves differently.
		Checksum: migration.DataTransformChecksum(
			[]byte("backfill-summaries v1"),
		),
	},
	Up: func(ctx context.Context, tx migration.DataTransaction) error {
		// Posts as this migration leaves them.
		posts, err := tx.Collection("posts")
		if err != nil {
			return err
		}
		for page := 1; ; page++ {
			result, err := tx.List(ctx, store.Request{
				Collection: posts, Page: page, Limit: 100,
			})
			if err != nil {
				return err
			}
			for _, post := range result.Documents {
				// Copy the headline into posts with no summary.
				if s, _ := post.Values["summary"].StringValue(); s != "" {
					continue
				}
				_, err := tx.Update(ctx, migration.UpdateRequest{
					Request: store.Request{Collection: posts, ID: post.ID},
					Values: store.Values{
						"summary": post.Values["headline"],
					},
				})
				if err != nil {
					return err
				}
			}
			if !result.HasNextPage {
				return nil
			}
		}
	},
	// Rolling back leaves the backfilled summaries in place.
	Down: func(context.Context, migration.DataTransaction) error {
		return nil
	},
}
```

Get collections and globals from the transaction with `tx.Collection(slug)` and `tx.Global(slug)`,
and pass them to every request. They return the resource exactly as this migration leaves it, or as
it was before when the migration removes it. Don't resolve your current config instead: once a
later migration changes the collection, its shape no longer matches this migration, and
`ridu migrate verify` and every new database fail to replay the transform.

## Register and bind it {#register}

Pass your transforms to the adapter's migration driver in `cmd/server/main.go`, so every
`ridu migrate` command can run them:

```go title="cmd/server/main.go" focus={3-7}
func main() {
	applicationConfig := content.Config()
	options := []ridu.ExecuteOption{
		ridu.WithProjectMigrations(
			sqlite.ProjectMigrations(transforms.All()...),
		),
	}
	if len(os.Args) == 1 {
		options = append(options, runtimeOptions(applicationConfig)...)
	}
	if err := ridu.Execute(applicationConfig, options...); err != nil {
		log.Fatal(err)
	}
}
```

Use `postgres.ProjectMigrations` or `mongodb.ProjectMigrations` for those databases. Then bind the
transform to the migration that needs it:

```sh title="terminal"
ridu migrate create --name require-summary \
	--transform backfill-summaries
ridu migrate verify
```

The migration records the transform's name and checksum. Keep the transform registered for as long
as that migration is in your history: `verify` and every new database replay it.

## What a transform can change {#limits}

- **Unversioned collections and globals only.** A transform can't yet rewrite a versioned
  resource's current document and all its retained versions together, so changes to a versioned
  resource are refused, including one with versions but no drafts. Backfill a field of a versioned
  resource in an earlier, data-only migration.
- **One kind of semantic work per SQLite migration.** On SQLite, a migration either renames fields
  or runs a transform. On SQLite, a collection rename also needs a transform.
- **Same behavior, same checksum.** The checksum identifies the reviewed behavior. If you change
  what `Up` or `Down` does, change the checksum before you create the migration.
- **Maintenance on PostgreSQL and MongoDB.** A transform rewrites content while it runs, so `up`
  needs `--allow-maintenance` with old writers stopped. See
  [Deploy migrations](./deploy.md#maintenance).

`Update` locks and patches the stored document itself; set `ReplaceValues` to replace its authored
values instead. `Delete` also removes the state the document owns, such as its versions, sessions
and preferences.
