# Payload parity server

This is a self-contained Payload `3.87.0` application, matching the stable stack used by the local
`payload-playground`, that mirrors the Ridu
[`admin_server`](../admin_server/) contract fixture. Keeping both applications in this repository
makes it possible to compare their admin behavior without modifying or bootstrapping Payload's
development monorepo.

The fixture deliberately contains two layers:

- the same eight editorial collections, roles, access scenarios, hooks, relationships, drafts,
  uploads, and seed data as Ridu;
- a **Comparison reference** group containing a stable spread of Payload fields and global behavior
  for side-by-side review. The current gap assessment lives in the adjacent Ridu fixture audit.

## Run it

Install its isolated dependencies once from the Ridu repository root:

```sh
bun run setup:payload-fixture
```

Generate the import map and exact Payload types after schema changes:

```sh
bun run generate:payload-fixture
```

Start the Payload admin at <http://localhost:3000/admin>:

```sh
bun run dev:payload-fixture
```

The fixture owns a separate `bun.lock` and `node_modules`. It is intentionally not a member of
Ridu's frontend workspace, because Payload 3 and Ridu use different Lexical versions. Its SQLite
database, uploaded media, Next cache, and dependencies are ignored.

SQLite remains the zero-configuration default. For PostgreSQL, set `PAYLOAD_DATABASE_URL` to a
`postgres://` or `postgresql://` URL, apply the committed migration, and then build or start the
fixture:

```sh
PAYLOAD_DATABASE_URL='postgres://user@127.0.0.1:5432/payload_parity?sslmode=disable' \
  ./node_modules/.bin/payload migrate
```

Performance builds set `RIDU_PAYLOAD_STANDALONE=true` to use Next's standalone output, so deployment
size and runtime behavior can be measured without the fixture's development dependency tree. See
[`../../performance/README.md`](../../performance/README.md) for the matched Ridu comparison.

## Accounts

| Account               | Password       | Scenario                                        |
| --------------------- | -------------- | ----------------------------------------------- |
| `admin@riducms.test`  | `ridu-admin`   | Full visibility and destructive access          |
| `editor@riducms.test` | `ridu-browser` | Editorial access with protected fields redacted |
| `demo@riducms.local`  | `ridu-demo`    | Published content plus owned drafts and notes   |

The database seeds itself on first initialization and does not duplicate the seed on subsequent
starts. Delete `payload-parity.db` and restart when a clean database is useful.

See [`../admin_server/PAYLOAD_UI_AUDIT.md`](../admin_server/PAYLOAD_UI_AUDIT.md) for the current
screen-by-screen findings and
[`../admin_server/PAYLOAD_COMPARISON.md`](../admin_server/PAYLOAD_COMPARISON.md) for the broader
behavioral gap matrix and side-by-side commands.
