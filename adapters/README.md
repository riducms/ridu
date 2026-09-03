# Ridu adapters

Adapters connect Ridu to databases and object storage:

- `postgres` implements `store.Store` and its optional persistence capabilities;
- `sqlite` supports content, auth, versions, uploads, locks, preferences, and durable tasks for
  embedded local and small deployments;
- `mongodb` supports the same features on a transaction-capable replica set within its documented
  production profile;
- `storage/local` and `storage/s3` implement `storage.Backend` and optional storage capabilities.

Adapters do not implement `ridu.Plugin` or appear in `Config.Plugins`. Open them with
`ridu.WithStore` or `ridu.WithUploadStorage`. Keep connection strings and credentials in server
configuration.

Follow the setup guides for [PostgreSQL](../website/src/content/docs/postgres.md),
[SQLite](../website/src/content/docs/sqlite.md), [MongoDB](../website/src/content/docs/mongodb.md),
and [object storage](../website/src/content/docs/storage.md). Each database guide covers new and
existing projects, verification, and production limits.
