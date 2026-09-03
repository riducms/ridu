<!-- Generated from website/src/content/docs/installation.md by scripts/sync-agent-docs.ts. -->

# Installation

To build your first application, follow the [Quickstart](./quickstart.md). Continue here for CLI
launchers, scaffold flags, existing services, and recovery.

## Requirements {#requirements}

- Go 1.25 or newer.
- Node.js 20 or newer and one supported package manager: npm, Bun, pnpm, or Yarn.
- PostgreSQL projects: Docker/OrbStack for the generated local service, or an existing PostgreSQL
  17 database.
- MongoDB projects: Docker/OrbStack for the generated replica set, or an existing transaction-capable
  replica set. Production support is limited to the exact [MongoDB profile](./mongodb.md).
- SQLite projects need no database service or Docker.

Choose npm, Bun, pnpm, Yarn, or the native Go command. The generated project records its package
manager in `ridu.toml`.

## Install the CLI {#install-cli}

Choose the launcher you want the project to use:

```bash title="terminal" package-manager="npm"
npm create ridu@latest my-app
```

```bash title="terminal" package-manager="bun"
bun create ridu@latest my-app
```

```bash title="terminal" package-manager="pnpm"
pnpm create ridu@latest my-app
```

```bash title="terminal" package-manager="yarn"
yarn create ridu my-app
```

The positional name sets the project directory. The wizard asks for the template, database, and
optional coding-agent guidance; omit `my-app` when you want it to ask for the directory too.

To skip the JavaScript initializer, install the native command and open the wizard directly. The
native path also asks which package manager the generated project should use:

```bash title="terminal"
go install github.com/riducms/ridu/cmd/ridu@latest
ridu new
```

## Choose scaffold options {#create-project}

The target directory must not exist. The wizard derives `example.com/<project>` and `@<project>`
defaults from that directory. Supply `--module` and `--scope` when those import/package identities
must match names already reserved by your organization.

| Choice          | Values                                     | Meaning                                                                                             |
| --------------- | ------------------------------------------ | --------------------------------------------------------------------------------------------------- |
| Template        | `starter`, `blank`                         | Starter adds example Posts; blank keeps authentication and framework wiring only                    |
| Database        | `postgres`, `sqlite`, `mongodb`            | PostgreSQL is default; SQLite is single-host embedded; MongoDB uses its bounded replica-set profile |
| Package manager | `npm`, `bun`, `pnpm`, `yarn`               | Owns frontend installs, lockfiles, admin commands, and plugin package changes                       |
| Agent guidance  | `codex`, `claude`, `cursor`, `all`, `none` | Installs local project guidance, or omits it                                                        |

Run any launcher without project options for the interactive wizard. Native `ridu new` also asks
which package manager to use. Set `RIDU_ACCESSIBLE=1` for stable plain-text prompts suitable for
screen readers. Flags skip their matching prompts; use `--no-agent` to omit agent guidance.

<details class="docs-disclosure">
<summary>Automate the wizard with flags</summary>
<div class="docs-disclosure-body">

Supply every choice to run without interactive input:

```bash title="terminal"
ridu new \
	--template starter \
	--database postgres \
	--package-manager npm \
	--agent codex \
	--module github.com/acme/content \
	--scope @acme \
	content
```

JavaScript initializers accept the same arguments. npm needs the conventional separator, for
example `npm create ridu@latest -- --database sqlite content`; Bun, pnpm, and Yarn forward the
arguments without it.

</div>
</details>

The created project includes `go.sum` and its generated manifest, OpenAPI, Go, TypeScript, and admin
plugin contracts.

## Use the project-local CLI {#project-local-cli}

New projects include `@riducms/cli` as a development dependency. Install the workspace, then use its
scripts:

```bash title="terminal" package-manager="npm"
cd content
npm install
npm run ridu -- version
npm run dev
```

```bash title="terminal" package-manager="bun"
cd content
bun install
bun run ridu -- version
bun run dev
```

```bash title="terminal" package-manager="pnpm"
cd content
pnpm install
pnpm run ridu version
pnpm run dev
```

```bash title="terminal" package-manager="yarn"
cd content
yarn install
yarn run ridu version
yarn run dev
```

The `dev` script runs the API, selected development database, contract generation, and admin.
`dev:admin` starts only Vite when another process already runs the API. Use the selected manager's
`ridu` script for doctor, generation, migrations, checks, builds, and plugins.

For existing generated projects, derive the npm launcher version from the framework already pinned
in `go.mod`, then add the same scripts:

```bash title="terminal" package-manager="npm"
RIDU_VERSION="$(go list -m -f '{{.Version}}' github.com/riducms/ridu | sed 's/^v//')"
npm install --save-dev --save-exact "@riducms/cli@$RIDU_VERSION"
```

```bash title="terminal" package-manager="bun"
RIDU_VERSION="$(go list -m -f '{{.Version}}' github.com/riducms/ridu | sed 's/^v//')"
bun add --dev --exact "@riducms/cli@$RIDU_VERSION"
```

```bash title="terminal" package-manager="pnpm"
RIDU_VERSION="$(go list -m -f '{{.Version}}' github.com/riducms/ridu | sed 's/^v//')"
pnpm add --save-dev --save-exact "@riducms/cli@$RIDU_VERSION"
```

```bash title="terminal" package-manager="yarn"
RIDU_VERSION="$(go list -m -f '{{.Version}}' github.com/riducms/ridu | sed 's/^v//')"
yarn add --dev --exact "@riducms/cli@$RIDU_VERSION"
```

Add the selected manager to `ridu.toml`. Existing version-1 projects that omit this key continue to
use Bun for compatibility.

```toml title="ridu.toml" add={3}
version = 1
database = "postgres"
package_manager = "npm"
```

Then add manager-neutral root scripts. Keep an existing `dev:admin` script, or use the
manager-specific workspace command emitted by a new scaffold.

```jsonc title="package.json" add={3,6-9}
{
	"devDependencies": {
		"@riducms/cli": "X.Y.Z"
	},
	"scripts": {
		"dev": "ridu dev",
		"ridu": "ridu",
		"check": "ridu check",
		"build": "ridu build"
	}
}
```

Replace `X.Y.Z` with the value printed in `RIDU_VERSION`; do not leave the placeholder in the file.
If you prefer a global command, install it with
`npm install --global @riducms/cli@latest` or `go install
github.com/riducms/ridu/cmd/ridu@latest`. Global commands can drift from older projects, so pin the
project-local package for repeatable work and automation.

## Open the admin {#open-admin}

Run the selected manager's `dev` script from the project directory and open the printed admin URL
(normally `http://localhost:8080/admin`). PostgreSQL and MongoDB start their local services when
needed; SQLite opens its local development file. On an empty auth collection, the admin shows
one-time first-user setup.

The [Quickstart](./quickstart.md#open-the-admin) continues from here with the first account,
document, and generated SDK read.

## Use an existing database service {#existing-database}

For PostgreSQL or MongoDB, bind the URL to the command and tell development not to start Compose:

```bash title="terminal" package-manager="npm"
DATABASE_URL="$DEVELOPMENT_DATABASE_URL" npm run dev -- --no-docker
```

```bash title="terminal" package-manager="bun"
DATABASE_URL="$DEVELOPMENT_DATABASE_URL" bun run dev -- --no-docker
```

```bash title="terminal" package-manager="pnpm"
DATABASE_URL="$DEVELOPMENT_DATABASE_URL" pnpm run dev --no-docker
```

```bash title="terminal" package-manager="yarn"
DATABASE_URL="$DEVELOPMENT_DATABASE_URL" yarn run dev --no-docker
```

Without an explicit MongoDB URL, development uses
`mongodb://127.0.0.1:27029/ridu?directConnection=true&replicaSet=ridu-rs0`. `--no-docker` therefore
requires `DATABASE_URL`/`--database-url` for MongoDB. A standalone `mongod` is not supported because
Ridu operations require transactions.

For SQLite development, use `--database-path ./data/content.sqlite`, `RIDU_SQLITE_PATH`, or the
project-local `.ridu/development.sqlite` default. The generated production binary requires an
absolute `RIDU_SQLITE_PATH` or absolute `file:` URI.

See each adapter guide for setup and production limits:

- [Add PostgreSQL to an existing project](./postgres.md#existing-project)
- [Add SQLite to an existing project](./sqlite.md#existing-project)
- [Add MongoDB to an existing project](./mongodb.md#existing-project)

Ridu does not provide a generic live cross-adapter migration facility. Moving existing data between
adapters is an application-owned export, transform, validation, recovery, and cutover.

`ridu dev --no-install` skips dependency installation, and `--no-sync` disables additive
development sync. Use these options only when another tool performs the omitted step.

## Recover an interrupted installation {#recovery}

If dependency setup fails, fix the reported cause, install the missing dependencies, and restart
development:

```bash title="terminal"
cd content
go mod tidy
npm install
npm run ridu -- doctor
npm run dev
```

The example uses npm; replace its commands with the manager recorded in `ridu.toml`.
`npm run ridu -- doctor` checks Go, the selected manager, `ridu.toml`, config discovery, and adapter
prerequisites without mutating production data. Continue with
[Troubleshooting](./troubleshooting.md) when a healthy scaffold still does not start.

Next read [Project structure](https://riducms.com/guides/project-structure/), [Fields](./fields.md), and
[Generated contracts](./generated-contracts.md).
