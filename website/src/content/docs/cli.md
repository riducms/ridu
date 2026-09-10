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

Use the Ridu CLI to create a project, run it in development, generate contracts, manage migrations,
and produce the production binary. Run commands anywhere inside a project; Ridu finds the nearest
`ridu.toml` automatically.

The examples use `ridu …` directly so the command stays the same for every package manager.
Generated projects also expose `dev`, `check`, `build`, `migrate`, and `start` package scripts.

## Start here {#start}

```sh title="terminal"
ridu dev       # Start the API and admin with live reload.
ridu check     # Check generated files, migrations, Go, TypeScript, and Svelte.
ridu build     # Write the production binary to dist/<project>.
```

A newly generated project includes its initial migration, so the first `ridu build` works after
installing dependencies. To run the result, apply its migrations to a selected database and start
the file in `dist/`. Follow [Build and run](/docs/production/build-and-run/) for the exact commands.

The sections below follow the common project tasks. Use the [CLI reference](/reference/cli/) when
you need every command signature and option in one place.

## Create a project {#new}

Run `new` in a terminal to open the project wizard:

```bash title="terminal"
ridu new
```

The wizard asks for the project directory, template, database, package manager, and coding-agent
guidance. After generation, run the directory, install, and `dev` commands printed under **Next**.
Set `RIDU_ACCESSIBLE=1` for plain-text prompts suitable for screen readers.

### Create without prompts {#new-flags}

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

Outside the wizard, `starter`, PostgreSQL, npm, and Codex guidance are the defaults. Use `blank` for
only the authentication collection and admin shell, `sqlite` for a single-host embedded database,
or `--agent none` to omit agent files. See the [MongoDB guide](/docs/mongodb/) before selecting its
bounded production profile.

### What the command writes {#new-output}

`ridu new` creates the application, runs `go mod tidy`, and writes the initial schema, clients,
OpenAPI document, admin plugin registry, and migration. If dependency setup or generation fails,
Ridu keeps the directory and prints the commands needed to finish it.

### Check your setup {#doctor}

```sh title="terminal"
ridu doctor
```

`doctor` checks Go, the selected package manager, the project file, frontend dependencies, and the
local database prerequisites. Use `ridu generate --check` and `ridu check` for the deeper project
checks.

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

`dev` installs missing frontend dependencies, prepares the local database, updates generated
contracts, and starts the Go API and Vite admin. When Go config changes, Ridu builds and checks a
replacement before sending traffic to it, so a failed edit leaves the last working server running.

To use another API or admin port:

```bash title="terminal"
ridu dev --address 127.0.0.1:3000 --admin-port 5174
```

Pass `--database-url <url>` for PostgreSQL or MongoDB, or `--database-path <path>` for SQLite.
Without those flags, Ridu reads `DATABASE_URL` or `RIDU_SQLITE_PATH`, then falls back to the
scaffold's local development database. `--no-docker`, `--no-install`, and `--no-sync` skip their
corresponding setup step. The [CLI reference for `ridu dev`](/reference/cli/dev/) lists every option
and default.

If schema sync finds an ambiguous rename or destructive transition, `dev` stops and asks for a
reviewed migration. See the selected [database adapter](/docs/adapters/) for its development rules.

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
admin `check` script with the package manager selected in `ridu.toml`. Missing frontend
dependencies are an error; install them with `ridu dev` or the selected package manager first.
PostgreSQL, SQLite, and MongoDB projects all fail closed when committed migration history does not
end at executable config.

## Build and run a release {#build}

```bash title="terminal"
ridu build
```

`build` updates generated contracts, builds the Svelte admin, and writes one executable to
`dist/<project>`. It embeds the admin and the exact migration history needed for production
readiness. Use `--output <path>` when a platform expects another filename.

If an older project has no migration files, the CLI now prints the recovery command. Run it once,
review the file, and build again:

```bash title="terminal"
ridu migrate create --name initial
ridu build
```

The generated `start` script launches the output, or you can run `./dist/<project>` directly. See
[Build and run](/docs/production/build-and-run/) for SQLite and PostgreSQL examples and
[Deploy to Railway](/docs/production/railway/) for build, migration, start, variables, and health
check settings.

## Migrations {#migrations}

```bash title="terminal"
ridu migrate create --name add-post-summary
ridu migrate verify
```

`create` writes an artifact without connecting to a database. `plan` and `status` inspect a
selected database without changing it. `verify` replays the complete history in an isolated target,
and `up` applies pending migrations. Select the database with `DATABASE_URL` for PostgreSQL or
MongoDB, or `RIDU_SQLITE_PATH` for SQLite. SQLite `verify` is the exception: it uses a temporary
database and needs no path. SQLite also provides the explicitly destructive `down`, `reset`,
`refresh`, and `fresh` commands for its reversible local artifacts.

Follow [Migrations](/docs/migrations/) for the complete authoring and deployment workflow. The
[migration command reference](/reference/cli/migrate-up/) lists every runner option, while each
[database adapter](/docs/adapters/) explains its own connection and recovery rules.

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

## Output and diagnostics {#output}

Lifecycle messages use local `HH:MM:SS` timestamps. Routine timestamps are muted and use a
`[ridu]` scope without repeating `INFO`; warnings and errors add an explicit level, and only error
timestamps are bold. Stable command results—help, version, prompts, migration tables, generated
paths, URLs, and machine-readable output—remain plain text. During `ridu dev`, Vite's duplicate
startup banner is suppressed and its warning/error output retains Vite's own formatting. Go server
output remains identifiable by a `[server]` badge. Foreground dependency installation retains the
selected manager's native progress and summary output.

On its first run, `create-ridu` downloads the matching native CLI before opening the wizard. It
shows elapsed time and received bytes, with a percentage when the server provides a usable total.
CI and piped output receive plain progress lines every five seconds. Download messages go to stderr;
cached runs skip them. Any earlier package-registry download uses your package runner's own output.

If the release server sends no response or further data for 30 seconds, the download stops with
the failing URL and recovery instructions. Check connectivity and access to that URL, then retry.
For a slow connection, increase `RIDU_CLI_DOWNLOAD_TIMEOUT_MS` (milliseconds), or set `RIDU_BINARY`
to an already-installed binary matching your Ridu release.

Colour is detected and downsampled for the active terminal. Set `NO_COLOR` (including to an empty
value) to disable Ridu-owned ANSI styling, or set `RIDU_ACCESSIBLE=1` to combine unstyled logs with
the CLI's plain-text prompts. Child-process ANSI content is forwarded unchanged.

## Get help {#help}

```sh title="terminal"
ridu help
ridu version
```

`ridu help` prints the command list and protocol versions. `ridu <command> --help` shows the
signature for one command. `--version` and `-v` are aliases for `ridu version`.
