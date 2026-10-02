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

See the [SQLite guide](../../website/src/content/docs/sqlite.md) for new- and
existing-project wiring, development sync, immutable migrations, backup/restore, startup
verification, and known limits.

The adapter supports local SQLite files and private `:memory:` databases. It does not provide
Payload's remote libSQL transport or a Cloudflare D1 compatibility mode.

Version lists and counts apply access predicates to the retained snapshot, within its collection
and parent document. Supported scalar and nonlocalized group predicates use the ordinary document
query compiler with snapshot sources, so counts do not deserialize snapshot values in Go. Repeated
or plugin paths, localized group descent, numeric/boolean locale fallback, operands that disagree
with the current scalar type, Unicode substring matching, and snapshot timestamp comparisons
retain the existing matcher fallback. These preserve access to snapshots from older field shapes.
Returned version lists still decode their authorized snapshots.
