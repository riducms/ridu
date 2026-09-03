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
Keep them on the same release line. Mixing versions can fail during generation, plugin registration,
or startup.

## Supported matrix {#supported-matrix}

| Surface         | Supported environment                                                                                 |
| --------------- | ----------------------------------------------------------------------------------------------------- |
| Go              | Go 1.25 or newer                                                                                      |
| Initializer     | Node.js 20 or newer                                                                                   |
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

Read the release notes, then upgrade the project as one unit:

1. Update `@riducms/cli` to the target version.
2. Update the Go module, framework npm packages, and official plugins to the same release line.
3. Run `ridu generate` and review the generated changes.
4. Create and review a [migration](/docs/migrations/) when the model changed.
5. Run `ridu check`, `ridu migrate verify`, and the application's tests.
6. Rehearse the deployment on a recent restored backup.

Set the version once so the CLI and Go module cannot drift:

```bash title="terminal" package-manager="npm"
TARGET_VERSION="X.Y.Z"
npm install --save-dev --save-exact "@riducms/cli@$TARGET_VERSION"
go get "github.com/riducms/ridu@v$TARGET_VERSION"
npm run ridu -- generate
```

```bash title="terminal" package-manager="bun"
TARGET_VERSION="X.Y.Z"
bun add --dev --exact "@riducms/cli@$TARGET_VERSION"
go get "github.com/riducms/ridu@v$TARGET_VERSION"
bun run ridu -- generate
```

```bash title="terminal" package-manager="pnpm"
TARGET_VERSION="X.Y.Z"
pnpm add --save-dev --save-exact --workspace-root "@riducms/cli@$TARGET_VERSION"
go get "github.com/riducms/ridu@v$TARGET_VERSION"
pnpm run ridu generate
```

```bash title="terminal" package-manager="yarn"
TARGET_VERSION="X.Y.Z"
yarn add --dev --exact --ignore-workspace-root-check "@riducms/cli@$TARGET_VERSION"
go get "github.com/riducms/ridu@v$TARGET_VERSION"
yarn run ridu generate
```

Never edit an applied migration. Add a forward migration to correct it.

## Verify an installation {#framework-gate}

Check that the project resolves one Ridu version across its tools:

```bash title="terminal"
ridu version
go list -m github.com/riducms/ridu
```

Then run `ridu doctor` to find a mismatched CLI, Go module, package, project file, or generated
contract.

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
