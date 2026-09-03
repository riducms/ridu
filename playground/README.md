# Ridu and Payload playground

This directory holds two isolated applications created by their normal project generators:

- [`ridu/`](./ridu/) is created by the current checkout's real `ridu new` command through the
  repository dogfood workflow.
- [`payload/`](./payload/) is created by `create-payload-app` from Payload's blank template.

They are deliberately outside the root Go module and Bun workspace. Framework code must never
import from the playground, and the playground must not become another implementation of Ridu. It
exists for small, observable experiments such as defining the same collection in both systems,
creating matching content, and rehearsing a migration.

The tiny `go.mod` files at the playground and Payload roots are isolation boundaries only. They
prevent root Go tooling from walking into generated applications or JavaScript dependencies; the
Payload application does not use Go.

The initial applications are generator baselines. They do not yet claim matching schemas or a
working Payload-to-Ridu migration.

The Ridu application enables its optional GraphQL plugin and commits `generated/ridu.graphql`.
That file is produced by ordinary `ridu generate` and gives the playground a reviewable schema for
future Ridu/Payload API comparisons without requiring network introspection.

The Ridu baseline intentionally has no immutable migration artifact yet. `ridu generate --check`
and `ridu build` are expected to pass; `ridu check` will report an empty migration history until the
first shared experiment creates an initial migration.

## Start the applications

Hydrate the Ridu packages and CLI from the current checkout, then start the generated application:

```sh
make playground-ridu-dev
```

Press Ctrl+C to stop Ridu, then use `make playground-ridu-down` to stop its PostgreSQL container.
The wrapper uses port `54339`, keeping the playground separate from Ridu's ordinary generated
development port.

On a fresh database, visiting the Ridu admin redirects to `/admin/create-first-user`, matching
Payload's initial setup shape. If this playground was started before that flow existed, run
`make playground-ridu-reset` once from the repository root to delete only the playground PostgreSQL
volume, then start it again. This intentionally removes all Ridu playground content.

In another terminal, prepare and start Payload:

```sh
cd playground/payload
cp .env.example .env # only when .env does not already exist
bun install --frozen-lockfile
bun dev
```

Payload uses SQLite at `payload.db` so this first baseline does not require another database
service. Ridu retains its generated PostgreSQL Compose setup. Local databases, uploads, secrets,
dependencies, build output, and the hydrated checkout packages are ignored.

The committed [`go.work`](./go.work) makes the generated Ridu application use the framework source
from this checkout. `make playground-ridu-hydrate` recreates its ignored release-shaped frontend
packages, installs their dependencies, and builds the local CLI after a fresh clone or framework
package change.

## Intended progression

1. Run both untouched applications.
2. Add one small matching collection to each application.
3. Add deterministic seed content.
4. Export the Payload content into Ridu's normalized migration shape.
5. Import it into Ridu and compare records, relationships, and files.

Project-specific setup and run commands live in each generated application's README.
