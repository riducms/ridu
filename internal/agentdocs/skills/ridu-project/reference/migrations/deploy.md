<!-- Generated from website/src/content/docs/migrations/deploy.md by scripts/sync-agent-docs.ts. -->

# Deploy migrations

The server never migrates a production database by itself. Run `ridu migrate up` as its own
deployment step, with the same release you are about to start.

## Release order {#order}

```text title="A release with new migrations" diagram
 1. ridu build                 records the migration history in the binary
 2. ridu migrate verify        replays the whole history in a throwaway
                               database; rehearse on a restored backup too
 3. back up                    the database and its uploads, together
 4. stop old writers           only when the pending migrations include
                               maintenance work (see below)
 5. ridu migrate up            add --allow-maintenance after step 4
 6. ridu migrate status        must report every migration applied
 7. start the new release      /readyz passes once the database has
                               exactly the history the binary was built with
```

`verify` proves the history replays; it can't reproduce production's data volume, lock timing or
content-specific collisions. That's what the restored-backup rehearsal is for. Set the migration
timeouts below your deployment deadline.

Use `ridu migrate plan --json` and `ridu migrate status --json` in deployment automation. Judge
success by `status`, never by the exit of an `up` you asked to stop early.

## Maintenance work {#maintenance}

Some migration steps rewrite content across a whole collection. An old process writing at the same
time could undo or corrupt that work, so `up` refuses them until you pass `--allow-maintenance`:

| Step                                | PostgreSQL | MongoDB |
| ----------------------------------- | ---------- | ------- |
| Content renames                     | needed     | needed  |
| Data transforms                     | needed     | needed  |
| Removing a collection or global     | needed     | needed  |
| Reference-index rebuilds            | needed     | needed  |
| Required-value audits               | —          | needed  |
| Unique indexes on versioned content | —          | needed  |

PostgreSQL's required-value audit holds writers back with a lock instead.

Pass `--allow-maintenance` only after every old application process, writer and worker has
stopped, and keep them stopped through retries until `status` completes. The flag is your promise
that they are stopped; it doesn't stop them for you. `up` asks for it only when the pending or
unfinished migrations contain such work.

SQLite needs no flag, but it still needs the same discipline: stop every process on the host that
uses the database file while `up` runs.

## Running old and new releases together {#overlap}

On PostgreSQL and MongoDB, two binaries may serve the same database at once only when they were built
from the same schema and the same migration history. A release that adds a migration can't overlap
with the one before it. SQLite never promises overlap; stop every process using the file for the
cutover.

## When `up` is interrupted {#interrupted}

```text title="What a retry of up does" diagram
SQLite       each migration commits or rolls back as a whole;
             a retry starts the interrupted migration again
PostgreSQL   committed phases and batches stay; a retry resumes
             after the last committed boundary
MongoDB      completed steps are recorded; a retry resumes the
             same migration from the first unfinished step
```

PostgreSQL's interrupted transaction leaves neither its data nor its progress record behind. A
batch phase resumes after its last committed checkpoint, and an interrupted concurrent index build
is either finished from its catalog state or removed and rebuilt. MongoDB holds an expiring lease
while it works; a crashed process or an expired lease never counts as completion.

On PostgreSQL you can stop deliberately at a committed boundary with `--stop-after-phase` or
`--stop-after-step`. When an ID repeats across migrations, write it as
`<migration-file>/<boundary-id>`. MongoDB rejects stop boundaries and resumes from its recorded
progress instead. `verify` always replays to the end.

Migration waits and timeouts are bounded by default. Override them with flags such as
`--advisory-lock-wait` and `--batch-timeout`; a zero value needs `--allow-unbounded`.

## Recover from a bad migration {#recover}

PostgreSQL and MongoDB have no down migrations. Fix forward with a new reviewed migration, or
restore the backup you took in step 3: the database and its uploads together. On SQLite,
`ridu migrate down --allow-destructive` can roll back the latest migration, but rolling back data
doesn't make an older release safe to run against it.

Never edit, rename or delete a migration that has run anywhere. `up`, `status` and readiness all
check the files against the database's record and refuse a changed history.

<details class="docs-disclosure">
<summary>MongoDB: the exact cutover sequence</summary>
<div class="docs-disclosure-body">

For any MongoDB release with a new migration, even an additive or data-only one, use separate
credentials for each step and follow this order:

1. Drain every old application replica and worker.
2. Run `DATABASE_URL="$MONGODB_OPERATIONAL_URL" ridu migrate verify`, so authority over the
   throwaway database is limited to that command. It needs no `--allow-maintenance`.
3. After `verify` succeeds, back up the selected database and its uploads with a separate,
   least-privilege credential such as `$MONGODB_BACKUP_URL`.
4. Run `DATABASE_URL="$MONGODB_MIGRATION_URL" ridu migrate up` with the database-scoped application
   or operator identity. Add `--allow-maintenance` only when the pending migrations contain
   maintenance work.
5. Run `DATABASE_URL="$MONGODB_MIGRATION_URL" ridu migrate status` and require complete history and
   exactly the indexes Ridu manages.
6. Start only the new release with `$MONGODB_APP_URL`, then wait for `/readyz`.

See [MongoDB](../mongodb.md#production) for credentials and recovery.

</div>
</details>

See [Production](../production.md) for the wider release checklist, and the
[PostgreSQL](../postgres.md) and [MongoDB](../mongodb.md) guides for connection and recovery
settings.
