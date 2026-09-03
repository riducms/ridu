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

Read the complete [PostgreSQL guide](../../website/src/content/docs/postgres.md) before changing an
adapter or deploying a schema change. It includes generated-project wiring, existing services,
`ridu migrate verify`, backup/cutover, pool bounds, and readiness checks.
