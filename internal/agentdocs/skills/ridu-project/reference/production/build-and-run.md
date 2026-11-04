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
Apply pending migrations before starting it. The binary applies them itself with `migrate up`, so
the host needs neither the Ridu CLI nor Go.

Choose the target database below. SQLite needs an absolute path so the migration command and server
open the same file. PostgreSQL needs its production connection URL.

```sh title="terminal" group="run-production" tab="SQLite"
export RIDU_SQLITE_PATH="$PWD/.ridu/production.sqlite"

ridu migrate plan
./dist/my-app migrate up
./dist/my-app
```

```sh title="terminal" group="run-production" tab="PostgreSQL"
export DATABASE_URL='postgres://user:password@host/database?sslmode=verify-full'

ridu migrate plan
./dist/my-app migrate up
./dist/my-app
```

`ridu migrate plan` shows what will run, and needs the project. `./dist/my-app migrate up` reads the
artifacts from `migrations/` in the working directory, or from `--directory`, and applies them only
when they are exactly the history the binary was built with. It takes the same `--allow-maintenance`
and timeout flags as [`ridu migrate up`](../migrations/deploy.md).

A production image needs `dist/my-app`, the `migrations/` directory, trusted certificates and its
environment variables; it needs no Ridu CLI, Go or JavaScript runtime. Start the binary itself. The
generated `start` package script runs the same binary, which is convenient on the build machine, but
in production the package manager would stay in memory beside the server for as long as it runs.

By default the server listens on `127.0.0.1:8080`, so only programs on the same machine can reach
it, and it answers only the host names `localhost`, `127.0.0.1` and `[::1]`. When a hosting
platform assigns `PORT`, the server listens on that port on every interface. Otherwise set
`RIDU_ADDRESS` to listen elsewhere, and `RIDU_ALLOWED_HOSTS` to the names clients use:

```sh title="terminal"
RIDU_ADDRESS=':3000' RIDU_ALLOWED_HOSTS='cms.example.com' ./dist/my-app
```

A reverse proxy on the same machine, such as Caddy or nginx, can reach the default address, but
it forwards your public host name, so list that name in `RIDU_ALLOWED_HOSTS`. A container needs
`RIDU_ADDRESS=':8080'` (or `PORT`), because its loopback interface is private to the container.

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
- **A binary made with `go build` refuses to start or migrate:** use `ridu build`; it embeds the
  migration fingerprint and admin assets.
- **`migrate up` says the migrations are not the history the binary was built with:** deploy the
  `migrations/` directory from the commit the binary was built from.
- **`migrate up` says the binary has no migration driver:** projects created before Ridu 0.24.0
  register one only for data transforms. Add `ridu.WithProjectMigrations(postgres.ProjectMigrations())`,
  or the `sqlite` or `mongodb` equivalent, to the options passed to `ridu.Execute` in
  `cmd/server/main.go`, outside the `len(os.Args) == 1` branch, then build again.
- **`migrate up` reports no migrations directory:** copy `migrations/` next to the binary, run it
  from that directory, or pass `--directory`.
- **The host cannot connect to the server:** set `PORT` or `RIDU_ADDRESS`, and configure its health
  check to use `/readyz`.
- **The server stops with an error that names a variable:** for example
  `RIDU_TRUSTED_PROXY_HOPS must be a whole number of zero or more, not "-1"`. Correct or unset that
  variable, then start the server again.

Continue with the [Railway guide](./railway.md) for a concrete hosted deployment, or
the [production overview](../production.md) for security, health checks, backups, and rollout
rules.
