# Ridu MongoDB adapter

`github.com/riducms/ridu/adapters/mongodb` requires a writable replica-set primary with logical
sessions and transactions. Opening it does not create collections, indexes, or migration state.

Draft-enabled versioned content keeps a working document and a separate live-head document;
the live head is independent of retained version history. MongoDB migration history uses the
current planner `5.0.0`; older planner artifacts and physical layouts are unsupported and must
be handled outside Ridu before startup. Readiness rejects incomplete live-head coverage.
Unique-index additions to versioned content use a maintenance-admitted,
resumable reservation rebuild before the new indexes are built; typed field renames
rebuild those reservations in their semantic transaction.

Document locks are fence writes, because MongoDB has no row locks. A reference takes a shared
lock: it increments one of the target's fence records in `z_ridu_reference_fences`, one per open
transaction of a Store, so saves that share a popular target do not conflict. An update or delete
takes an exclusive lock: it increments the document's own fence and each of its fence records,
which conflicts with every holder. The records stay for later saves, at most 32 per document, and
are removed with the document, its collection's retirement or a collection rename. A transaction that meets a conflicting lock before its first
content write restarts on a newer snapshot, locks again what it held and waits its turn instead of
failing. Within one Store the turns are queued, as PostgreSQL queues lock requests: a waiting
update or delete holds back the references of transactions that began after it, so a stream of
saves cannot starve it, and a transaction waiting for another one of the Store resumes when that
one ends or gives way. The queue is local to the process; replicas coordinate only through the
fences and retry after a jittered backoff. Operation statements carry a server time limit of the
caller's deadline, at most 60 seconds. Database-side shape guards for counts and selections check
the authored root and each repeated field's row identity, so their size follows definitions rather
than block placements. A filter through repeated fields nests one `$elemMatch` per level and guards
the rows along its own path by their declared fields; see the
[concurrency and limits](../../website/src/content/docs/mongodb.md#concurrency) section of the
guide.

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
