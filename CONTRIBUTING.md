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

## Test value and verification tiers

A useful test protects a named behavior, detects a plausible defect, and adds evidence that the
retained tests do not already provide. Judge that evidence against execution time, setup, flakiness
and maintenance cost. Test count and coverage percentage alone do not establish value.

- Assert observable results, persisted state, or an explicit boundary. For example, zero store
  reads can be an authorization guarantee; an exact private helper call sequence usually is not.
- Use the cheapest layer that can prove the claim. Parsing and registry rules belong in unit
  tests; reactive ownership needs compiled Svelte; routing/static imports/server integration needs
  the built admin; published-package behavior needs a generated application.
- A test named for bounded work must assert a bound or structure. Logging sizes and durations
  belongs in benchmarks. Keep a small correctness case alongside large scale workloads.
- Before consolidating or retiring a test, name its retained replacement and check that it catches
  the same plausible regression. Similar domain names across engine, HTTP and adapters can protect
  different trust boundaries. Use a focused semantic mutation when the overlap is uncertain.
- Use controlled clocks and observable completion rather than wall-clock sleeps. Every browser
  worker retains its own server, data and uploads, and every test retains its own reset/session.
- Temporary migration tombstones should leave the gate after the migration is complete. Current
  public contracts remain protected by actual consumers, version rejection, runtime behavior and
  focused dependency-boundary assertions. Retiring a name-absence guard intentionally ends that
  historical prohibition; a compiled consumer proves current API usability, not absence of every
  former identifier.

| Command | Evidence |
| --- | --- |
| Focused package or component tests | The affected claim during editing; expand when a shared contract changes. |
| `make check-dev` | Formatting, vet, short Go tests, generated/TypeScript contracts, unit tests and real Svelte component tests. |
| `make check-fast` | Development evidence plus generated-project/release-shaped fixtures, production builds and real Vite build integration. |
| `make check-full` | The integration handoff gate, adding the live SDK and built-admin browser workflows. |
| `make performance-check` | Serial create/update/population workloads, generated-definition scale/compile matrix and controlled browser performance thresholds. |
| `make cli-clean-install-test` | A real CLI binary and generated external application using fresh disposable Go caches. |
| `make release-check` | Full integration with clean CLI caches, serial performance checks, plus database, documentation, security and package/reproducibility qualification. |

The full JS `bun run test` includes build integration. `bun run test:workspace` is the prepared
unit/component/contract subset; `bun run test:build-integration` runs the real Vite integration
fixture explicitly. Runtime packages must be built before consuming their compiled contracts.

Development CLI fixtures use dedicated ignored caches under `.ridu/test-cache/cli/`. Their local
Go module versions identify the exact captured source contents, so a changed checkout cannot reuse
an older module under the same version. Set `RIDU_TEST_CLEAN_CACHE=true` to use empty disposable
module/build caches instead; release qualification does so. Frontend publication snapshots are
prepared freshly and then copied for repeated publication assertions.
These caches are disposable and retain older source snapshots; remove `.ridu/test-cache/cli/`
when reclaiming disk space, with no CLI fixture running.

The four independent type-check groups run in parallel after runtime packages are built. Full
checks finish frontend builds and live SDK verification before overlapping Go tests and browser
work. Do not start another encompassing gate against the same checkout. Playwright output belongs
under distinct `.ridu/` subdirectories, outside Go's `./...` package traversal, so cleanup cannot
race package discovery. Performance measurements run separately from that concurrent gate.

Measure both whole commands and individual tests when changing this setup. Go JSON events and
Playwright's JSON reporter expose case timings; distinguish test-result cache hits, build/setup,
skips and worker parallelism. Never sum nested test durations or promise that removing parallel
work saves the same amount of gate wall time. Run the affected checks while editing, then one full
handoff gate; repeat it only after subsequent changes or failures invalidate the evidence.
