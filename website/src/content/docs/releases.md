---
title: 'Releases and compatibility'
description: 'Keep Ridu packages aligned, understand supported environments, and upgrade an application safely.'
product: cli
eyebrow: 'Ship'
order: 250
navigation:
  section: 'Develop & operate'
  order: 90
  title: 'Releases and compatibility'
---

Ridu uses one version across its Go module, project-local CLI, npm packages, and official plugins.
Keep them on the same exact release version. Mixing versions can fail during generation, plugin registration,
or startup.

## Supported matrix {#supported-matrix}

| Surface         | Supported environment                                                                                 |
| --------------- | ----------------------------------------------------------------------------------------------------- |
| Go              | Go 1.26 or newer                                                                                      |
| Initializer     | Node.js 20 or newer for the launcher alone; generated admin tooling has stricter requirements below.  |
| Generated admin | Node.js 20.19+ (20.x), 22.12+ (22.x), or 24+; use 24+ for a new project.                              |
| Source checkout | Bun 1.4.0                                                                                             |
| PostgreSQL      | PostgreSQL 17.x                                                                                       |
| SQLite          | The bundled driver, using a local file on one application host                                        |
| MongoDB         | Community 8.2.9, SCRAM-SHA-256, verified TLS, and a writable three-member replica set on Linux x86-64 |
| Browser tests   | Chromium                                                                                              |

PostgreSQL-compatible services must provide the PostgreSQL behavior Ridu depends on, including
transaction semantics, migration locks, concurrent indexes, and TLS. Test a managed service before
using it in production.

MongoDB support does not extend to Atlas, DocumentDB, Cosmos DB, standalone servers, other versions
or topologies, or other operating systems and architectures. Follow the supported setup in
[MongoDB](/docs/mongodb/).

## Versioning rules {#versioning}

Before `v1.0.0`, a release may change a public contract. Upgrade one project at a time and read its
release notes before updating. Only the latest pre-1 release is supported.

From `v1.0.0`, breaking changes to public Go, CLI, REST, SDK, and extension contracts require a major
version. Additions use a minor version and compatible fixes use a patch version.

## Contracts fail mismatches early {#contract-versions}

Ridu checks related versions during generation and startup:

| Contract            | What happens when versions do not match                                      |
| ------------------- | ---------------------------------------------------------------------------- |
| Schema manifest     | Readers reject an unsupported manifest version.                              |
| Project command     | The CLI stops before reading config from an incompatible application.        |
| Migration artifacts | The adapter rejects unsupported or altered migration history.                |
| Backend plugins     | Registration fails outside the plugin's supported Ridu range.                |
| Admin plugins       | Generation fails when the browser package does not match its backend plugin. |
| REST and SDK        | Stable envelopes and error codes allow compatible additive responses.        |

Fix the package versions and regenerate. Do not copy generated files from another project or bypass
plugin checks.

## What counts as public {#public-surface}

Compatibility covers documented exported Go packages, CLI commands, REST and protocol contracts,
published TypeScript packages, generated application contracts, and documented plugin interfaces.

Go `internal/` packages, `.ridu/` intermediates, unexported symbols, and generated implementation
details are not customization surfaces. Change the executable config and regenerate instead of
editing generated output.

## Upgrade a project {#upgrade}

Read the release notes, then run the new release's CLI with `upgrade`. Running the target version
means the upgrade itself always uses the new release's rules:

```bash title="terminal" package-manager="npm"
npx @riducms/cli@X.Y.Z upgrade X.Y.Z
```

```bash title="terminal" package-manager="bun"
bunx @riducms/cli@X.Y.Z upgrade X.Y.Z
```

```bash title="terminal" package-manager="pnpm"
pnpm dlx @riducms/cli@X.Y.Z upgrade X.Y.Z
```

```bash title="terminal" package-manager="yarn"
yarn dlx @riducms/cli@X.Y.Z upgrade X.Y.Z
```

The command upgrades the project as one unit:

1. It sets every `@riducms/*` package in the project's `package.json` files, the official plugins
   in `ridu.plugins.json`, and the `github.com/riducms/ridu` Go module to the release.
2. It installs the new packages and runs `go mod tidy`.
3. With the newly installed CLI, it runs `ridu generate`, `ridu agent sync` when agent guidance is
   installed, and `ridu migrate create --name ridu-X-Y-Z`. If the release does not change your
   schema, no migration is created.

Review the changed files and any new [migration](/docs/migrations/), then run `ridu check`,
`ridu migrate verify`, and your application's tests. Rehearse the deployment on a recent restored
backup before production.

Pass `--no-install` to rewrite only the version pins; the command then prints the remaining steps.
It leaves linked packages, such as `workspace:` or `file:` dependencies, unchanged and warns when
a `go.work` file still points the Ridu module at a local checkout.

Never edit an applied migration. Add a forward migration to correct it.

## Verify an installation {#framework-gate}

Check that the project resolves one Ridu version across its tools:

```bash title="terminal"
ridu version
go list -m github.com/riducms/ridu
```

Compare those versions with the `@riducms/*` versions in the project's package files and lockfile.
`ridu doctor` checks basic prerequisites and structural project settings; it does not compare all
release versions, compile Go config, verify generated contracts, or test database connectivity.
Run `ridu generate --check` to resolve executable config and detect generated-contract drift, then
`ridu check` for the project's Go and frontend checks.

## Before production {#application-gate}

Before sending traffic to an upgraded application:

- run `ridu migrate verify` against restored data;
- restore the database and upload storage together and test representative media;
- confirm database transport, credentials, topology, and timeouts;
- monitor `/healthz`, `/readyz`, request errors, tasks, latency, and storage health;
- exercise authentication, writes, access-controlled reads, uploads, drafts, and rollback on a
  canary; and
- record who approves the traffic switch and who can roll it back.

See [Production](/docs/production/), [Security](/docs/security/), [Migrations](/docs/migrations/), and
[Troubleshooting](/docs/troubleshooting/) for the complete operational paths.
