---
title: 'Ridu CLI'
description: 'Create projects and plugins, generate contracts, run development, manage database migrations, and build releases.'
product: cli
eyebrow: 'CLI'
order: 150
navigation:
  section: 'Develop & operate'
  order: 20
  title: 'CLI'
---

`ridu` coordinates the Go application, generated contracts, selected database artifacts, and
Svelte admin. Run project commands anywhere beneath the directory containing `ridu.toml`; discovery
walks upward. When the CLI needs config, it executes the application-owned versioned project
command—it never parses Go source as a substitute.

## Command map {#commands}

| Command         | Purpose                                                                                                                          |
| --------------- | -------------------------------------------------------------------------------------------------------------------------------- |
| `ridu new`      | Scaffold a generated application and write its initial contracts.                                                                |
| `ridu doctor`   | Diagnose Go, Bun, project discovery, frontend dependencies, and selected database prerequisites.                                 |
| `ridu dev`      | Prepare the selected database and generation, then run/watch the Go API and Vite admin.                                          |
| `ridu generate` | Resolve executable config and atomically write the manifest, clients, OpenAPI, plugin registry, and configured plugin artifacts. |
| `ridu check`    | Verify generated and migration drift, Go format/vet/tests, and TypeScript/Svelte.                                                |
| `ridu build`    | Regenerate, build the admin, and atomically emit one Go application binary.                                                      |
| `ridu migrate`  | Create immutable artifacts and run the migration commands supported by the selected adapter.                                     |
| `ridu plugin`   | Scaffold, install, or remove compiled backend/static-admin plugin pairs.                                                         |
| `ridu add`      | Alias for `ridu plugin add`.                                                                                                     |
| `ridu agent`    | Install or synchronize release-matched, offline coding-agent guidance.                                                           |
| `ridu version`  | Print the CLI release version. `--version` and `-v` are aliases.                                                                 |
| `ridu help`     | Show the top-level command list plus project-protocol and schema-manifest versions. `--help` and `-h` are aliases.               |

Exact exported command contracts also appear in the [CLI reference](/reference/cli/).

## Output and diagnostics {#output}

Lifecycle messages use local `HH:MM:SS` timestamps. Routine timestamps are muted and use a
`[ridu]` scope without repeating `INFO`; warnings and errors add an explicit level, and only error
timestamps are bold. Stable command results—help, version, prompts, migration tables, generated
paths, URLs, and machine-readable output—remain plain text. During `ridu dev`, Vite's duplicate
startup banner is suppressed and its warning/error output retains Vite's own formatting. Go server
output remains identifiable by a `[server]` badge. Foreground dependency installation retains the
selected manager's native progress and summary output.

Colour is detected and downsampled for the active terminal. Set `NO_COLOR` (including to an empty
value) to disable Ridu-owned ANSI styling, or set `RIDU_ACCESSIBLE=1` to combine unstyled logs with
the CLI's plain-text prompts. Child-process ANSI content is forwarded unchanged.

## Create a project {#new}

Run `new` in a terminal to open the project wizard:

```bash title="terminal"
ridu new
```

It collects the project directory, template, database, package manager, and coding-agent guidance,
describes each choice, and asks for confirmation before writing. One prompt is active at a time;
completed answers collapse into compact history and later prompts remain hidden. Set
`RIDU_ACCESSIBLE=1` for stable plain-text prompts suitable for screen readers. After generation,
run the exact directory, install, and `dev` commands printed under **Next**.

<details class="docs-disclosure">
<summary>Automate or preselect the wizard</summary>
<div class="docs-disclosure-body">

Every supplied choice skips its matching prompt. Specify every choice for a non-interactive script:

```bash title="terminal"
ridu new \
	--template starter \
	--database postgres \
	--package-manager npm \
	--agent codex \
	--module github.com/acme/my-cms \
	--scope @acme \
	my-cms
```

```text title="ridu new --help"
ridu new [--template starter|blank] [--database postgres|sqlite|mongodb]
  [--package-manager npm|bun|pnpm|yarn]
  [--agent codex|claude|cursor|all|none]
  [--no-agent] [--module path] [--scope @scope]
  [--release-version version] [directory]
```

</div>
</details>

`--module` defaults to `example.com/<project>`, `--scope` to `@<project>`, `--database` to
`postgres`, and `--package-manager` to `npm` outside the wizard. Select `sqlite` explicitly for the embedded single-host adapter or `mongodb` for the
bounded generated-project replica-set profile. MongoDB production support covers only the exact
Linux x86-64, Community 8.2.9, authenticated verified-TLS three-member profile in its
[adapter guide](/docs/mongodb/).
`--release-version` is a framework-development dependency override, not an application version.
Use `--no-agent` or `--agent none` to omit agent guidance. Non-interactive runs
default to `starter` and the Codex-compatible layout. `starter` includes the example posts
collection, while `blank` keeps only the authentication collection and framework wiring.
The command scaffolds, runs `go mod tidy`, and generates initial schema, client, OpenAPI, and admin
plugin contracts. Unless omitted, it also installs the selected agent's root entrypoint and two
complete local skills: `ridu-project` for ordinary application work and `payload-to-ridu` for
assessment and migration. Their focused references are embedded in the CLI release, so agents can
work without reaching into Ridu's private development repository or requiring network access. If
dependency setup or initial generation fails, Ridu keeps the new directory and prints recovery
commands; it does not discard your scaffold.

Run `ridu doctor` when setup is uncertain. It accepts no flags or positional arguments. PostgreSQL
projects need `DATABASE_URL` or Docker/OrbStack for the scaffolded service. SQLite projects report
their configured path or the `.ridu/development.sqlite` default and do not require Docker. MongoDB
projects need a replica-set `DATABASE_URL` or Docker/OrbStack for the scaffolded development
replica set.

## Coding-agent guidance {#agent}

Generated projects use the layout selected by `ridu new`:

| Selection           | Root entrypoint | Local skills                                     |
| ------------------- | --------------- | ------------------------------------------------ |
| `codex` or `cursor` | `AGENTS.md`     | `.agents/skills/{ridu-project,payload-to-ridu}/` |
| `claude`            | `CLAUDE.md`     | `.claude/skills/{ridu-project,payload-to-ridu}/` |
| `all`               | Both            | Both skill roots                                 |
| `none`              | None            | None                                             |

Add another layout to an existing project with `ridu agent install --agent <agent>`. The command
preserves an existing `AGENTS.md` or `CLAUDE.md` and refuses to replace an untracked skill file.
`.ridu-agent-docs.json` records only framework-managed skill files and their digests; commit it with
the installed guidance.

After upgrading the CLI, run `ridu agent sync`. Sync updates only files recorded in that manifest,
writes each file atomically, and leaves root project instructions alone. If a managed reference was
edited, sync stops before changing anything so you can move the project-specific note into
`AGENTS.md`, `CLAUDE.md`, or `PROJECT.md` first. The same public documentation is available at
[`/llms.txt`](/llms.txt), [`/llms-full.txt`](/llms-full.txt), and release-pinned
`/v/<version>/llms-full.txt` URLs for tools that consume HTTP documentation feeds.

## Development lifecycle {#dev}

```bash title="terminal"
ridu dev
```

In order, `dev` installs missing frontend dependencies with the package manager selected in `ridu.toml`, reads the selected database, builds one disposable Go
candidate, resolves config by executing that exact binary, atomically writes generated contracts,
applies only the non-destructive development schema plan, and reuses the candidate as the server.
It starts Vite when an admin directory is configured, waits for both, then watches Go config. Each
accepted edit prints its build, manifest, contract, database, and total timings. A generated Go
contract change enters a bounded consistency loop only when the server imports that package, and
each rebuilt executable resolves its own manifest before serving. A stable CLI-owned proxy keeps
the public API address on the accepted process while a replacement starts on a private loopback
address. A newer save normally skips a stale candidate. If MongoDB has already committed additive
indexes for that candidate, Ridu first promotes the exact database-matching candidate and then
immediately processes the queued newer revision; discarding it would leave the running manifest
behind the physical plan. Every replacement must verify its own Store and pass development
`/readyz` before the proxy promotes it. Plugin-registry changes signal a full admin reload.

| Option                   | Default                                             | Effect                                                           |
| ------------------------ | --------------------------------------------------- | ---------------------------------------------------------------- |
| `--database-url <url>`   | `DATABASE_URL`, then the scaffold local URL         | Choose development PostgreSQL or a MongoDB replica set.          |
| `--database-path <path>` | `RIDU_SQLITE_PATH`, then `.ridu/development.sqlite` | Choose the local SQLite file.                                    |
| `--address <host:port>`  | `127.0.0.1:8080`                                    | Bind the Go API.                                                 |
| `--admin-port <port>`    | `5173`                                              | Bind the Vite admin.                                             |
| `--no-docker`            | false                                               | Do not start the selected PostgreSQL or MongoDB Compose service. |
| `--no-install`           | false                                               | Do not install missing frontend dependencies.                    |
| `--no-sync`              | false                                               | Skip the non-destructive development schema plan.                |

Development enables explicitly insecure local cookie/database settings and the Vite origin for the
managed processes. Do not reproduce those settings as production defaults. If schema sync finds an
ambiguous rename or destructive transition, stop and create a reviewed migration rather than
expecting `dev` to guess intent.

For MongoDB, sync creates only missing indexes. The separate serving Store verifies the exact
resolved plan before cutover; incompatible physical drift rejects the candidate and leaves the
last working process and data in place. See [MongoDB](/docs/mongodb/).

## Generate and check contracts {#generated-contracts}

`ridu generate` writes these configured artifacts atomically:

- `generated/ridu.schema.json` — canonical manifest;
- `generated/ridu.openapi.json` — concrete REST contract;
- `generated/ridu.generated.go` — application Go models and handles;
- `generated/ridu.generated.ts` — application-bound TypeScript config/client;
- configured `generated.<plugin>.<artifact>` destinations — including exact SDL from the compiled GraphQL plugin;
- `admin/src/ridu.plugins.generated.ts` — validated static admin plugin imports; and
- generated compiled-plugin registration derived from `ridu.plugins.json`.

```bash title="CI"
ridu generate --check
ridu check
```

`generate --check` resolves config and reports drift without writing. Plain `generate` writes
changed files and accepts no other option. Generated paths are application-owned configuration in
`ridu.toml`; disposable intermediates live in `.ridu/`. Read [Generated contracts](/docs/generated-contracts/)
before changing generated ownership.

`ridu check` accepts no flags. It runs generation in check mode, requires migration artifact history
to match the resolved manifest, checks `gofmt`, then runs `go vet ./...`, `go test ./...`, and the
root Bun `check` script when an admin is configured. Missing frontend dependencies are an error;
install them with `ridu dev` or `npm install` first. PostgreSQL, SQLite, and MongoDB projects all
fail closed when committed migration history does not end at executable config.

## Build a release {#build}

```bash title="terminal"
ridu build
ridu build --output ./dist/cms-linux-amd64
```

`build` regenerates contracts in write mode, installs missing frontend dependencies, runs the root
Bun `build` script, then compiles the configured Go entry with a fingerprint of the exact ordered
migration filenames and artifact digests. It builds to a temporary file and renames it into place
only on success. The default target is
`dist/<project>` (`.exe` on Windows); `--output` accepts a project-relative or absolute file path.
The result contains the admin assets when the application embeds its configured
asset package. Official adapters compare the live ledger with that fingerprint at production
readiness; a direct `go build` omits it and fails closed unless the application explicitly delegates
readiness or enters the development-only startup path.

Generation during `build` is not a drift check. Run `ridu check` in CI and review/commit generated
changes before producing a release. MongoDB production use is supported only inside the adapter's
documented generated-project, Linux, and replica-set profile. See
[Production](/docs/production/) for the full cutover.

## Migrations {#migrations}

```bash title="terminal"
ridu migrate create --name add-post-summary
ridu migrate plan
ridu migrate verify
```

Those are preparation commands, not a MongoDB production cutover order. The exact MongoDB sequence
is documented below and in the [MongoDB guide](/docs/mongodb/#production).

PostgreSQL and MongoDB expose the five forward-production commands. SQLite exposes those commands
plus its reviewed local reversible lifecycle:

| Subcommand | Database?         | Behavior                                                                                                                                                                                                                                                                          |
| ---------- | ----------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `create`   | No                | Compare resolved config with the latest artifact manifest, plan the transition, and write a new immutable artifact. PostgreSQL and MongoDB can confirm detected renames; SQLite and MongoDB data changes can name a compiled transform. Requires `--name <lowercase-kebab-case>`. |
| `plan`     | Yes               | Read-only listing of artifacts, phases, steps, and committed progress. A missing SQLite file reports every artifact pending without creating it. `--json` emits machine-readable status.                                                                                          |
| `status`   | Yes               | Read-only ledger/history and physical-state status. SQLite opens an existing file read-only without changing its journal mode. `--json` is available.                                                                                                                             |
| `verify`   | Adapter-dependent | Replay complete admitted history in an isolated PostgreSQL schema, temporary SQLite database, or random MongoDB shadow database and assert the final state. MongoDB needs a controlled credential able to create and drop that shadow database.                                   |
| `up`       | Yes               | Validate and apply pending history. PostgreSQL uses an advisory lock and resumable phases; MongoDB uses a fenced lease and durable step resumption; SQLite applies all pending artifacts atomically under one writer transaction.                                                 |
| `down`     | SQLite            | Reverse the latest applied SQLite artifact after explicit destructive approval.                                                                                                                                                                                                   |
| `reset`    | SQLite            | Reverse every applied SQLite artifact atomically.                                                                                                                                                                                                                                 |
| `refresh`  | SQLite            | Reverse and reapply committed SQLite history atomically.                                                                                                                                                                                                                          |
| `fresh`    | SQLite            | Drop non-internal objects and replay committed SQLite history atomically.                                                                                                                                                                                                         |

PostgreSQL and MongoDB database-backed subcommands take `--database-url <url>` or `DATABASE_URL` and
accept `--allow-insecure-database` only as an explicit local plaintext or certificate-verification
override. SQLite uses
`--database-path <path>` or `RIDU_SQLITE_PATH`; its `verify` command replays in a temporary database
and needs no deployment path. An explicit `--database-path` may be project-relative;
`RIDU_SQLITE_PATH` must be an absolute file path or `file:` URI, matching the generated runtime.
SQLite mutating migration commands reject `:memory:` because their successful state must survive
the CLI process. `create` accepts `--name <name>` and adapter-specific review options:

- PostgreSQL and MongoDB accept `--accept-renames` for unambiguous proposed renames.
- SQLite accepts `--transform <name>` for a registered transaction-bound data transform and rejects
  `--accept-renames`; canonical JSON rewrites use the compiled transform instead.
- MongoDB also accepts `--transform <name>` for a registered compiled transform bound to its exact
  artifact and runner.
- All three accept `--allow-destructive` to record planner-confirmed destructive changes after review. It does not
  connect to or modify a database.

MongoDB `create` always runs without a database URL. New planner-`2.0.0` artifacts record physical
index steps, explicit rename intent, compiled transforms, reviewed resource retirement, and final
assertions in immutable history. Authenticated planner-`1.0.0` history remains a supported
immutable prefix that is validated and replayed before v2 rather than rewritten. `plan`, `status`,
`verify`, and `up` use the production runner; `down`, `reset`, `refresh`, and `fresh` remain
unsupported.

Keep the running MongoDB application database-scoped. Use separate short-lived credentials for
verification and backup/restore work in production.

PostgreSQL `up` and `verify`, and MongoDB `up` and `verify`, share the applicable bounded runner
controls:

| Option                                     | Adapter  | Meaning                                                                               |
| ------------------------------------------ | -------- | ------------------------------------------------------------------------------------- |
| `--advisory-lock-wait <duration>`          | Both     | Maximum wait for PostgreSQL's advisory lock or MongoDB's fenced migration lease.      |
| `--lock-timeout <duration>`                | Postgres | PostgreSQL lock wait per phase.                                                       |
| `--statement-timeout <duration>`           | Postgres | Transactional statement duration.                                                     |
| `--batch-timeout <duration>`               | Postgres | One checkpoint batch duration.                                                        |
| `--concurrent-index-timeout <duration>`    | Both     | PostgreSQL concurrent-index duration or the complete MongoDB migration operation.     |
| `--idle-in-transaction-timeout <duration>` | Postgres | Idle time inside a transaction phase.                                                 |
| `--allow-unbounded`                        | Both     | Explicitly admit zero runner waits/timeouts; MongoDB lease expiry remains bounded.    |
| `--allow-maintenance`                      | Both     | Assert that every old process/writer/worker is stopped for a traffic-sensitive phase. |

For PostgreSQL, only `up` accepts `--stop-after-phase <id>` and `--stop-after-step <id>`; qualify a
repeated ID as `<artifact-name>/<boundary-id>`. A successful boundary stop means progress was
committed, not that history is current—run `status` before admitting application traffic. SQLite
rejects these phased-runner options because its artifact history commits atomically. MongoDB also
rejects explicit stop boundaries; an interrupted run resumes from its durable completed steps.

For a MongoDB release with any new migration artifact—including a same-manifest data-only
artifact—first drain all old processes and workers. Then run
`DATABASE_URL="$MONGODB_OPERATIONAL_URL" ridu migrate verify`, capture the matched selected-database
and upload snapshot, run `DATABASE_URL="$MONGODB_MIGRATION_URL" ridu migrate up` with the selected
app/operator identity, run post-`up` `status` with that URL, and only then start the binary with
`$MONGODB_APP_URL`.

> [!IMPORTANT]
> PostgreSQL and MongoDB have no generated `ridu migrate down`; use a reviewed forward correction or
> restore the complete coordinated recovery point. SQLite artifacts carry explicit reversible steps
> and expose `down`/`reset`/`refresh`/`fresh` only with destructive approval. Never edit an applied
> artifact for any adapter.

The flags above are safety admissions, not broad bypasses: `--allow-destructive` cannot override a
semantic safeguard, and `--allow-maintenance` does not stop old writers for you. Read the selected
adapter's [PostgreSQL migration workflow and recovery policy](/docs/migrations/) or
[SQLite migration and recovery contract](/docs/sqlite/#migrations), or the bounded
[MongoDB production profile](/docs/mongodb/#migrations), then use
[Troubleshooting](/docs/troubleshooting/#migration-refused) when a plan fails closed.

## Plugins {#plugins}

```text title="ridu plugin --help"
ridu plugin new <directory> --key <key> --module <go-module> --admin-package <npm-package>

ridu plugin add <key> --go-package <package>
  [--go-version <version>] [--constructor New]
  [--admin-package <package>] [--admin-version <version>] [--no-install]

ridu plugin remove <key> [--no-install]
```

`plugin new` scaffolds paired Go and admin packages with the given stable key, module, and published
admin package name. `plugin add` defaults Go/admin versions to `latest` and the exported zero-argument
constructor to `New`; `ridu add` is its exact alias. It installs dependencies unless `--no-install`,
updates registration, regenerates contracts, and rolls back the project mutation if compatibility
or generation fails.

`plugin remove` removes registration, then removes the unshared paired admin package from the root
and admin manifests and tidies Go dependencies unless `--no-install`. That package remains installed
when another plugin still references it. Separately installed field-type packages remain
application-owned. The command regenerates and rolls back registration on contract failure.
Removal never silently drops plugin-owned database state: create and review the resulting migration
before applying any plugin down steps. See [Plugins](/docs/plugins/).

## `ridu.toml` {#project-file}

```toml title="ridu.toml"
version = 1
database = "postgres" # or "sqlite" or "mongodb"; credentials remain runtime-owned
package_manager = "npm" # or "bun", "pnpm", or "yarn"
entry = "./cmd/server"
admin = "./admin"
schema = "./generated/ridu.schema.json"
client = "./generated/ridu.generated.ts"
migrations = "./migrations"
openapi = "./generated/ridu.openapi.json"
# Set only after enabling the optional GraphQL plugin.
# generated.graphql.schema = "./generated/ridu.graphql"
assets = "./internal/adminassets/dist"
plugins = "./ridu.plugins.json"
plugin_go = "./content/ridu_plugins.generated.go"
```

Version 1 accepts these structural keys plus generic `generated.<plugin>.<artifact>` destinations.
`database`, `entry`, `schema`, `plugins`, and `plugin_go` are required; `database` must explicitly
select `postgres`, `sqlite`, or `mongodb` because the CLI does not infer an adapter;
`package_manager` selects npm, Bun, pnpm, or Yarn for frontend operations. Existing version-1 files
that omit it retain Bun for compatibility, while new scaffolds always write the selection.
`admin`, `client`, `migrations`, `openapi`, and `assets` may be empty when the project omits those
outputs. Other paths are relative to the project root and may not escape it. The file is structural CLI
configuration, not the CMS schema—application behavior remains in executable Go config. See
[Configuration](/docs/configuration/) and [Project structure](/guides/project-structure/).
