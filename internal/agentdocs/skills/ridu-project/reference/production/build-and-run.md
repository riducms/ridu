<!-- Generated from website/src/content/docs/production/build-and-run.md by scripts/sync-agent-docs.ts. -->

# Build and run

A Ridu build produces one executable in `dist/`. The executable contains the Go server, API,
workers, generated schema, migration-history fingerprint, and compiled admin.

## Build your app {#build}

From the project root, run:

```sh title="terminal"
ridu build
```

For a project named `my-app`, the last line is:

```text title="output"
Built dist/my-app
```

New projects already contain an initial migration. If an older project has no files in
`migrations/`, create its first artifact and try the build again:

```sh title="terminal"
ridu migrate create --name initial
ridu build
```

Review and commit the new `*.ridu.json` file. Future schema changes need a new migration before
`ridu build` will accept them.

Before a release, run the broader checks and replay the migration history:

```sh title="terminal"
ridu check
ridu migrate verify
ridu build
```

SQLite verification uses a temporary database and needs no path. PostgreSQL and MongoDB
verification need `DATABASE_URL` because they create an isolated shadow schema or database.

## Apply migrations and start {#run}

The binary checks that the target database has the exact migration history used during the build.
Apply pending migrations before starting it.

Choose the target database below. SQLite needs an absolute path so the migration command and server
open the same file. PostgreSQL needs its production connection URL.

```sh title="terminal" group="run-production" tab="SQLite"
export RIDU_SQLITE_PATH="$PWD/.ridu/production.sqlite"

ridu migrate plan
ridu migrate up
ridu migrate status
./dist/my-app
```

```sh title="terminal" group="run-production" tab="PostgreSQL"
export DATABASE_URL='postgres://user:password@host/database?sslmode=verify-full'

ridu migrate plan
ridu migrate up
ridu migrate status
./dist/my-app
```

The generated `start` package script runs the same binary, so `bun run start`, `npm run start`,
`pnpm run start`, or `yarn start` is convenient when testing on the build machine. A production
image only needs `dist/my-app`, trusted certificates, and its environment variables.

The server listens on `:8080` by default. Set `RIDU_ADDRESS` when you control the full address, or
set `PORT` when a hosting platform assigns the port:

```sh title="terminal"
RIDU_ADDRESS=':3000' ./dist/my-app
```

## Choose another output path {#output}

Use `--output` when a deployment expects a particular filename:

```sh title="terminal"
ridu build --output ./dist/server
./dist/server
```

The output path may be relative to the project or absolute. Ridu writes a temporary executable and
only replaces the destination after the complete build succeeds.

## Fix a failed first build {#troubleshooting}

- **`migration artifact history is empty`:** run `ridu migrate create --name initial`, review the
  generated file, then build again.
- **`ridu migrate plan needs a SQLite database`:** set `RIDU_SQLITE_PATH` to an absolute path or
  pass `--database-path <path>`.
- **Startup reports pending or mismatched migrations:** run `ridu migrate status`, restore the
  committed artifacts if they changed, then run `ridu migrate up`.
- **A binary made with `go build` refuses to start:** use `ridu build`; it embeds the migration
  fingerprint and admin assets.
- **The host cannot connect to the server:** set `PORT` or `RIDU_ADDRESS`, and configure its health
  check to use `/readyz`.

Continue with the [Railway guide](./railway.md) for a concrete hosted deployment, or
the [production overview](../production.md) for security, health checks, backups, and rollout
rules.
