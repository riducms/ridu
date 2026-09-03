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
