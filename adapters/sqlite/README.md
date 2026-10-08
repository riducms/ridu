# Ridu SQLite adapter

`sqlite` is Ridu's embedded database adapter for local and small deployments. It supports content,
relationships, versions, auth, preferences, document locks, upload references, and durable tasks
through a pure-Go SQLite driver. Production does not need CGO, Node, or a separate database service.

New projects can select it during scaffolding:

```sh
ridu new
```

Choose **SQLite** in the wizard and review the summary. For non-interactive setup, use the flags in
the SQLite guide.

Open never creates Ridu tables. Use `Store.Migrate` only for explicit development bootstrap, or use
the immutable artifact API for a deployed database. Existing projects must update `ridu.toml`, the
server's adapter factory, and committed migrations together; deployed servers require an absolute
`RIDU_SQLITE_PATH` on durable local storage.

Draft-enabled versioned content keeps one working head and an independently selected live head;
the live head is not a retained-history version and survives history pruning. New databases and
immutable migrations use the current `1.3.0` planner and physical layout. Older planner histories
and databases missing the live-head table are unsupported; readiness and development synchronization
reject them instead of converting stored content.

A collection or global that starts keeping versions does so in a migration of its own that records
what its stored documents become. In the migration's transaction every stored document, trashed
ones included, gets a first version and, for `published`, a live head; `require-empty` stops the
migration while any document is stored. `migrate down` keeps each document's latest working content
and removes its versions and live head. Development schema sync enables versions only on a resource
that stores none.

See the [SQLite guide](../../website/src/content/docs/sqlite.md) for new- and
existing-project wiring, development sync, immutable migrations, backup/restore, startup
verification, and known limits.

The adapter supports local SQLite files and private `:memory:` databases. It does not provide
Payload's remote libSQL transport or a Cloudflare D1 compatibility mode.

Filters and access predicates compile to SQL JSON expressions where SQL can match exactly what the
in-process matcher matches; the rest of a predicate stays in the same `WHERE` clause as a call to
that matcher. Membership filters on set-valued fields (lists, has-many selects, relationships and
uploads, and polymorphic relationships) compile with `json_each`, through groups, localized
containers, and array and Blocks rows. Items are compared by their JSON type, so the SQL also
agrees with the matcher on values stored in an older field shape.

Version lists and counts apply access predicates to the retained snapshot, within its collection
and parent document. Supported scalar and nonlocalized group predicates, and membership predicates
at any compilable path, use the ordinary document query compiler with snapshot sources, so counts
do not deserialize snapshot values in Go. Scalar comparisons at repeated or plugin paths, scalar
localized group descent, numeric/boolean locale fallback, operands that disagree with the current
scalar type, Unicode substring matching, and snapshot timestamp comparisons retain the existing
matcher fallback. These preserve access to snapshots from older field shapes. Returned version
lists still decode their authorized snapshots.
