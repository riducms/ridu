# Contributing to Ridu

Discuss large public API, storage, protocol, or admin changes before implementing them. Small fixes,
tests, and documentation improvements should stay focused on one domain and preserve the existing
contracts.

## Set up the repository

Use Go 1.25 or newer and Bun 1.4.0. PostgreSQL integration uses PostgreSQL 17; SQLite uses the bundled
pure-Go driver and does not require CGO or an external database service.

```sh
git clone https://github.com/riducms/ridu.git
cd ridu
bun install --frozen-lockfile
make check
```

Use `make demo` for a local in-memory admin tour. PostgreSQL integration tests are opt-in and read
their connection details from the environment; ordinary checks do not require a running database.
Run `make sqlite-test` for changes to SQLite or shared database behavior. Use `make admin-dev` for
the framework-owned admin loop and `make documentation-check` to verify the public documentation
site.

Before changing scaffolding, release resolution, or initial generation, exercise the real CLI
binary against the repository's local versioned module proxy. The target must not already exist and
is kept afterward for inspection:

```sh
make dogfood-new TARGET=/tmp/ridu-dogfood-content
cd /tmp/ridu-dogfood-content
./.ridu/bin/ridu check
```

This uses the development-only `ridu new --release-version` override and provisions ignored local
frontend workspaces plus the matching CLI. It does not add a local `replace` directive or weaken
the project-command compatibility handshake.

## Find the right home for a change

- The root `ridu` package stays a thin facade of aliases and forwarding constructors.
- Public application contracts and runtime coordination belong in `core/`, split by named domain.
- `field`, `query`, `schema`, `store`, and `storage` remain public leaf packages and must not import
  the facade or `core`.
- Framework behavior belongs under `internal/`, named for the domain that owns it.
- Database and object-storage implementations of singular runtime ports belong under `adapters/`.
- Additive backend capabilities and their paired admin code belong under `plugins/`.
- The framework-owned web application belongs in `admin/`; reusable TypeScript contracts belong in
  `packages/`.
- Cross-surface behavior belongs in `tests/`, with readable inputs in `testdata/`.
- Isolated applications and component intake tools belong in `playground/`; production packages
  must not import from them.

For Svelte work, also read [the admin guide](./admin/README.md).

## Make and verify a change

Prefer one complete vertical behavior over unrelated changes across several subsystems. Update the
public website documentation when supported behavior changes.

```sh
make format
make check-full
```

Do not commit generated caches, build output, local dependency paths, or unrelated workspace
changes.
