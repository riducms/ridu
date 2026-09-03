# Admin server contract fixture

This server is the deliberately broad, deterministic application used to inspect Ridu's embedded
admin as a Payload-style editorial system. It is a contract fixture rather than a polished example
site: each collection exists to expose supported behavior, awkward behavior, or a missing admin
capability.

Run it from the repository root with the contract admin entry. That entry statically registers the
rich-text field plugin and the fixture plugin route:

```sh
bun run dev:admin-fixture
```

The default admin is available at <http://127.0.0.1:18081/admin/login>. To use a separately built
admin, set `RIDU_BROWSER_ADMIN_DIR`; to change the listener, set `RIDU_BROWSER_ADDRESS`. Running the
Go command without `RIDU_BROWSER_ADMIN_DIR` intentionally serves the generic framework bundle, which
cannot render application-owned field plugins such as rich text.

The fixture uses the strict in-memory store by default. Set `RIDU_POSTGRES_URL` to run the same
config against PostgreSQL. Browser runs create a unique `ridu_admin_fixture_*` schema, hold a
PostgreSQL advisory ownership lock for the server lifetime, connect through that schema's
`search_path`, apply the development plan, and seed it before every test. The schema is dropped on
graceful shutdown. An optional `RIDU_POSTGRES_FIXTURE_SCHEMA` must retain the reserved prefix; a
second process that requests the same schema is refused instead of racing a destructive reset.

The reset route is disabled during ordinary fixture use. Playwright explicitly enables it with
`RIDU_BROWSER_RESET_TOKEN`, sends that token in `X-Ridu-Test-Reset-Token`, and may only enable it on
a loopback listener. Each reset drains active requests and replaces both the fixture database and
its temporary local upload storage before reseeding.

For the runnable Payload mirror and the current side-by-side gap matrix, see
[`PAYLOAD_COMPARISON.md`](./PAYLOAD_COMPARISON.md).

## Accounts and access scenarios

| Account               | Password       | Expected view                                                                                       |
| --------------------- | -------------- | --------------------------------------------------------------------------------------------------- |
| `admin@riducms.test`  | `ridu-admin`   | Every document and protected field; destructive operations                                          |
| `editor@riducms.test` | `ridu-browser` | All editorial content, but protected fields are redacted and some destructive operations are denied |
| `demo@riducms.local`  | `ridu-demo`    | Published posts plus owned drafts/notes; protected workflow changes are denied                      |
| `api@riducms.test`    | `ridu-api`     | Authentication/API use remains valid, but the auth collection's `Admin` rule denies admin entry      |

The different accounts are intentional. Access rules are enforced by the operation/store pipeline,
so changing the session should change list results, relationship options, mutation outcomes, and
field redaction—not merely hide controls.

## What each collection probes

| Collection      | Contract surface                                                                                                                                                                                                                                                                                                                                                              |
| --------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Users           | Auth, sessions, roles, admin-entry access, API-only identities, rows, groups, field-level write rules, and protected field redaction                                                                                                                                                                                                                                           |
| Media           | Uploads, image metadata and variants, upload relationships, nested tags, recoverable trash and permanent delete                                                                                                                                                                                                                                                               |
| Categories      | Labels, unique fields, grouped SEO fields, public read/staff writes, computed output, access-aware inverse joins with configured tables and target mutations                                                                                                                                                                                                                  |
| Posts           | Drafts, revisions, autosave metadata, expiring edit locks/takeover, live preview, publish/unpublish, duplicate-aware hooks, atomic selected-document workflows, recoverable trash, every scalar family, rich text, rows, named form tabs, conditions, groups, arrays, blocks, uploads, singular/many/polymorphic relationships, nested and target-scoped document-derived option filters, field access |
| Pages           | Versioned block-driven pages, rich text inside a block, block-owned relationships/uploads, and version-history access separated from current-document reads                                                                                                                                                                                                                    |
| Events          | Dates and date-times, numbers, booleans, email validation, conditional venue/URL fields, nested schedules, JSON                                                                                                                                                                                                                                                               |
| Editorial notes | Owner-filtered reads/updates/deletes and administrator-only nested response data                                                                                                                                                                                                                                                                                              |
| Redirects       | Staff-only collection access and administrator-only deletion                                                                                                                                                                                                                                                                                                                  |
| Forms           | Payload-familiar block-based form definitions, confirmation messages/redirects, protected email delivery configuration, upload/payment options, and ordinary generated/admin contracts                                                                                                                                                                                        |
| Form submissions | Public creates with selected-form validation, persisted scalar/upload values, payment results, authenticated reads, and denied updates                                                                                                                                                                                                                                        |
| Field showcase  | Code, radio, point, UI-only and virtual content, collapsible layout, and bounded arrays with duplicate/collapse/custom-label controls                                                                                                                                                                                                                                         |

The seed deliberately includes published and draft documents, stable array/block `_key` values,
two generated image sizes, relationship population candidates, nested rich text, arbitrary JSON,
and values that differ across access roles.

## Gaps this fixture should keep visible

Do not fake unsupported Payload features inside this fixture. The current roadmap still records
localization and optional realtime multi-user presence as incomplete. Posts exercise
manifest-driven preview URL templates, breakpoint presets, responsive dimensions, zoom,
open-in-new-window, server-rendered draft authorization, and unsaved draft streaming. The fixture plugin
exercises provider wrappers, graphics/avatar, header/actions/settings, login, profile, navigation,
logout, collection list/create/edit, global, and not-found extension contracts alongside dashboard,
route, list-cell, document-action, and document-view registrations. The field showcase and Site
settings global keep recently completed parity surfaces inspectable alongside the remaining gaps.

The preview fixture intentionally runs on `127.0.0.1:18082`, separate from the admin. It installs
the same protocol exposed by `connectLivePreview`: a generated browser channel plus a five-minute
document-scoped server capability in the preview URL, an origin- and source-checked ready message,
an access-checked initial draft, current unsaved-draft delivery, and focus/pageshow/online reconnect
announcements for both the iframe and popup.

Access rules and field permissions also do not yet have a serializable admin summary. The server is
authoritative, but the admin cannot always predict a denial before a request. That is useful signal:
the fixture should make those rough edges observable instead of silently weakening its policies.
