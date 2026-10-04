# Ridu MongoDB adapter

`github.com/riducms/ridu/adapters/mongodb` requires a writable replica-set primary with logical
sessions and transactions. Opening it does not create collections, indexes, or migration state.

Draft-enabled versioned content keeps a working document and a separate live-head document;
the live head is independent of retained version history. MongoDB migration history uses the
current planner `3.0.0`; older planner artifacts and physical layouts are unsupported and must
be handled outside Ridu before startup. Readiness rejects incomplete live-head coverage.
Unique-index additions to versioned content use a maintenance-admitted,
resumable reservation rebuild before the new indexes are built; typed field renames
rebuild those reservations in their semantic transaction.

New projects can select it during scaffolding:

```sh
ridu new
```

Choose **MongoDB** in the wizard and review the summary. For non-interactive setup, use the flags in
the MongoDB guide.

Production support covers generated starter and blank projects using MongoDB Community 8.2.9,
SCRAM-SHA-256, a writable three-member replica set, verified TLS, and Linux x86-64. It does
not cover Atlas, DocumentDB, Cosmos DB, standalone servers, other versions, topologies, platforms,
or generic Mongo-compatible services.

Read the complete [MongoDB guide](../../website/src/content/docs/mongodb.md) for existing-project
`ridu.toml` and adapter-factory wiring, replica-set Compose setup, development/index verification,
migrations, credentials, and the production profile. Ridu does not provide a cross-adapter live-data
migration; write and verify a custom migration when moving existing PostgreSQL or SQLite data.
