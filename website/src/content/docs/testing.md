---
title: 'Testing'
description: 'Test Ridu configuration, generated contracts, database behavior, HTTP clients, and authoring flows.'
product: cli
eyebrow: 'Developer workflow'
order: 155
navigation:
  section: 'Develop & operate'
  order: 50
  title: 'Testing'
---

Test the smallest boundary that can catch the failure you care about. Keep access rules, hooks, and
validators in fast Go tests. Add a database-backed test for adapter behavior, a live server for HTTP
behavior, and a browser for authoring behavior.

## Choose the smallest useful layer {#layers}

| Layer                    | Use it for                                                                     |
| ------------------------ | ------------------------------------------------------------------------------ |
| Go unit/config           | Access rules, hooks, validators, helpers, and config resolution                |
| `plugintest`             | Public plugin configuration, validation, Local API, and REST behavior          |
| Generated-contract check | Missing or stale manifest, Go, OpenAPI, TypeScript, and admin output           |
| Database integration     | Queries, constraints, indexes, transactions, migrations, and recovery          |
| Live HTTP transport      | URLs, headers, cookies, request bodies, response envelopes, and cancellation   |
| Browser                  | Authentication, fields, forms, server issues, persistence, and access-aware UI |

Start low in the table and add a higher layer only when the boundary matters.

## Unit-test executable config {#config-tests}

Access rules, hooks, task handlers, field helpers, and plugin validators are ordinary Go functions.
Test their branches directly, then keep one resolution test that composes the application config:

```go title="content/config_test.go"
package content

import (
	"testing"

	"github.com/riducms/ridu"
)

func TestConfigResolves(t *testing.T) {
	resolved, err := ridu.Resolve(Config())
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.Collections) == 0 {
		t.Fatal("expected at least one collection")
	}
}
```

Assert stable public behavior such as issue codes and paths, access decisions, resolved fields, and
operation results. Use table-driven tests for role and operation matrices.

## In-memory tests {#in-memory}

Ridu does not publish an in-memory database adapter for applications. Unit-test pure policy and
configuration without a store, then use a disposable instance of the same adapter you deploy for
operation integration tests. This avoids passing tests against behavior your production database
does not share.

## Use `plugintest` for public plugin conformance {#plugintest}

Plugin authors can exercise the shared backend contract with `plugintest.Run`:

```go title="plugin_test.go"
func TestConformance(t *testing.T) {
	plugintest.Run(t, plugintest.Fixture{
		Plugin: plugin.New(),
		Fields: field.Fields{
			plugin.Field("value").Required(),
		},
		ValidData:   store.Values{"value": store.String("valid")},
		InvalidData: store.Values{"value": store.String("")},
		Compatibility: []plugintest.CompatibilityCase{
			{RiduVersion: ridu.FrameworkVersion, Compatible: true},
		},
	})
}
```

This covers config resolution, manifest encoding, compatibility, validation, Local API, and REST
behavior. A field with an admin component also needs frontend type checks and a browser test. See
[Build a custom field](/guides/custom-fields/).

## Check generated output {#generated-drift}

Run these commands in CI after installing project dependencies:

```sh title="terminal" package-manager="npm"
npm run ridu -- generate --check
npm run ridu -- check
```

```sh title="terminal" package-manager="bun"
bun run ridu -- generate --check
bun run ridu -- check
```

```sh title="terminal" package-manager="pnpm"
pnpm run ridu generate --check
pnpm run ridu check
```

```sh title="terminal" package-manager="yarn"
yarn run ridu generate --check
yarn run ridu check
```

`generate --check` reports missing or stale generated files without rewriting them. `ridu check`
also checks migration history, Go formatting, `go vet`, Go tests, and the configured TypeScript and
Svelte projects. Fix the source config and regenerate; do not hand-edit generated files.

## Test PostgreSQL behavior {#postgresql}

Use a disposable database or uniquely named schema for integration tests that depend on PostgreSQL
queries, constraints, indexes, locks, concurrency, task leasing, or recovery. Pass its URL through a
test-only environment variable, clean it after the test, and never point a test suite at a shared
development or production database.

For schema changes, run `ridu migrate verify` to replay migration history in a shadow schema. See
[PostgreSQL](/docs/postgres/) and [Migrations](/docs/migrations/).

## Test SQLite behavior {#sqlite}

Create each SQLite test database under `t.TempDir()`. Test the same transaction, migration, index,
and recovery paths your application uses. Do not place the file on a shared network filesystem or
use a passing SQLite test to claim multi-host support. See [SQLite](/docs/sqlite/).

## Test MongoDB behavior {#mongodb}

Use a disposable database on the supported writable replica-set topology when a test depends on
transactions, BSON encoding, indexes, migrations, or recovery. A standalone local server cannot
exercise Ridu's transaction path. Run `ridu migrate verify` with a credential allowed to create and
drop its temporary verification database. See [MongoDB](/docs/mongodb/).

## Test a live transport {#transport}

Handler tests are useful for a focused route. Keep at least one test that starts the application on
a loopback port and calls it through the generated SDK. Cover login cookies, errors, cancellation,
and the methods your frontend actually uses.

Custom endpoint tests should also cover the wrong method, body limits, actor propagation, and calls
through `PluginEndpointContext.Local`.

## Test authoring in a browser {#browser}

Run browser tests against your generated admin. Prefer a few complete journeys over a large number
of cosmetic assertions:

1. sign in;
2. create or edit a representative document;
3. confirm validation and access behavior;
4. reload and verify persisted values; and
5. exercise any custom admin field or plugin.

Test the browsers your users rely on. Ridu's own Chromium coverage does not replace an application's
browser matrix.

## Build readable test data {#fixtures}

Use stable IDs, timestamps, sort order, array/block keys, and named accounts for distinct roles.
Create one valid and one invalid value for each custom validator, and include explicit draft,
published, deleted, or versioned states only when the workflow needs them. Give every test its own
storage root, namespace, port, schema, or database and clean it afterward.

## Project commands {#commands}

An ordinary project CI job usually needs:

```sh title="terminal" package-manager="npm"
go test ./...
npm run ridu -- generate --check
npm run ridu -- migrate verify
npm run ridu -- check
npm run ridu -- build
```

The equivalent project-local commands work with Bun, pnpm, and Yarn. When one fails, identify the
boundary first: configuration, generated output, operation behavior, database/migration behavior,
HTTP encoding, or admin rendering. [Troubleshooting](/docs/troubleshooting/) follows the same split.
