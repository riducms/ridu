# Payload to Ridu concept map

This routes investigation; it is not a static parity claim. Check the installed
`ridu-project/reference/capabilities.md`, the target project's manifest, generated contracts, and
tests before marking anything supported.

| Payload concept | Ridu owner | Translation rule |
| --- | --- | --- |
| `buildConfig` | `ridu.Config` | Compose executable Go constructors. Do not create a YAML/JSON schema or parse Go source. |
| Collection/global config | `ridu.Collection` / `ridu.Global` | Preserve slugs, labels, admin metadata, indexes, auth/upload/version policy, and executable rules deliberately. |
| Fields | Concrete fluent field factories or compiled field plugin | Map validation, nested shape, localization, defaults, relationships, and admin presentation—not only the field name. |
| Collection access | `ridu.CollectionAccess` / `ridu.GlobalAccess` | Return allow, deny, or a query predicate; filtered decisions stay atomic in the store operation. |
| Field access | Attached `field.Access` | Port read redaction separately from create/update admission and prove occurrence-bound nested behavior. |
| Hooks | collection, field, global, or auth hooks | Preserve phase, transaction reuse, original/current document semantics, and after-commit side effects. |
| Payload Local API | Ridu `LocalAPI` / generated typed handles | Pass the actor and exact actor collection; do not call a store adapter directly. |
| REST/GraphQL | REST + generated Fetch SDK; optional compiled GraphQL plugin | Do not preserve Payload wire shapes unless a consumer explicitly requires a reviewed adapter. |
| Admin React component | Svelte admin plugin contract | Rebuild observable interaction with static registration; do not port React internals. |
| Payload plugin | focused compiled Go plugin plus optional static Svelte pair | Re-express public behavior. npm-plugin compatibility and runtime installation are excluded. |
| Jobs/tasks | compiled Ridu task and durable queue contracts | Preserve idempotency, retry/lease behavior, schedules, and authorization intent. |
| Migrations | immutable Ridu artifacts plus source-import plan | Treat Payload migrations as evidence, not SQL to replay against Ridu tables. |
| Generated Payload types | Ridu manifest/OpenAPI/Go/TypeScript contracts | Compare shapes, then regenerate from Go config and check drift. |

Payload's database layout, Next.js runtime, React component API, GraphQL names, REST envelopes, and
npm plugin interface are not compatibility targets by default. A present field type is not enough:
query behavior, relationships, migrations, generated types, access, admin editing, and
serialization must agree.

Choose `adapt` only with explicit product acceptance and a testable replacement behavior.
Otherwise mark the row `blocked` and keep the original system in service for that slice.
