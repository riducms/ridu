# Ridu PostgreSQL adapter

`github.com/riducms/ridu/adapters/postgres` is the official networked database adapter for ordinary
production and multi-replica Ridu applications. It implements content, auth, relationships,
localization, versions, uploads, locks, preferences, tasks, immutable migrations, and exact
readiness through the public store boundary.

New projects can select it during scaffolding:

```sh
ridu new
```

Choose **PostgreSQL** in the wizard and review the summary before creating the project. Use the
complete guide's flag reference only for automation.

Existing projects must update `ridu.toml`, the server's adapter factory, development wiring, and
committed migration history together. Runtime credentials belong in `DATABASE_URL`; production
connections must require verified TLS. Opening the adapter does not create or migrate tables.

Only the current PostgreSQL planner (`atlas` `1.3.0`) and physical schema are supported. Histories
and development databases from earlier layouts are rejected rather than converted. Create a fresh
database and current migration history for those layouts. Ordinary authored schema migrations, data
transforms, verification, and recovery remain available. Startup never alters the schema.

Each collection or global stores its working documents in a typed table. A versioned resource also
has a typed live table generated from the same definition: identical columns, unique constraints,
declared and GIN indexes, and relationship foreign keys, plus `has_draft_changes`. Its live row
references the working row and is deleted with it. Publishing copies the saved working row into the
live table with one `INSERT … SELECT`; saving a draft leaves the live row unchanged. The live row is
independent of version-history pruning, which keeps JSON snapshots in `ridu_versions`.

Published reads use the live table through the ordinary typed predicate compiler, scanner,
planner statistics, and indexes, so filters, sorts, counts, and pagination behave and scale like
working reads. Trash and restore mirror deletion state into the live row atomically. Locked
published reads also lock the working row to coordinate reference admission with mutations and
deletion. Unlocked working reads of draft-enabled resources return the live revision and
pending-draft flag from the same statement. A locking working read takes them from a second
statement in the same pipeline: when the lock waits for a concurrent publication or unpublication,
READ COMMITTED returns the newest working row but keeps the statement's original snapshot for every
other table, so only a statement that starts after the lock sees the matching live state. Trash
mirrors into the live row the same way. A localized field has one native column per configured
locale; reads select those columns and assemble the locale-keyed value in Go, holding only locales
with a stored value.

A filter through arrays and blocks is one `EXISTS` over the rows its path reaches. A single repeated
level expands its JSONB array; consecutive levels are enumerated by one strict `jsonb_path_query`
that binds each block level to its type, since a correlated subquery per level costs several times
as much on every scanned document. A level reached through a localized container is read with SQL
locale fallback from the enclosing row instead.

An update writes only while the working row is still the version its locked `Current` read
describes: the `UPDATE` matches `updated_at` and, when present, `_revision`. Every write advances
`updated_at` strictly, even twice in one transaction, so a stale `Current` is a conflict. Saving a
document's revision 1 version discards history a removed document with the same ID left behind.

The reference index (`ridu_document_references`) always equals the references of the stored working
and live rows. Every path that removes a row removes its index rows in the same transaction: delete,
unpublication, and resource retirement; development schema synchronization rebuilds the index when
reference fields change. Creation therefore inserts index rows without replacing, and deleting a
referenced document treats an index row without its owner row as corruption.

Unique constraints are enforced by each table's unique indexes and reserve values across both
tables. Runtime admission compares the saved document's complete tuples with the other documents
of both tables; writes with a null component in every unique tuple do not need the collection
admission lock. Restoring trash checks both rows. Migrations audit both tables before building a
new unique index and report a duplicate as a conflict.

Migrations treat both tables as one resource: renames, retirements, nested JSON and reference
rewrites, and development field-kind recovery change the working and live tables together.

Read the complete [PostgreSQL guide](../../website/src/content/docs/postgres.md) before changing an
adapter or deploying a schema change. It includes generated-project wiring, existing services,
`ridu migrate verify`, backup/cutover, pool bounds, and readiness checks.
