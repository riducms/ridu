# Payload inventory

Build an evidence-backed inventory before translating code. Prefer executed configuration,
generated types, migrations, and observed consumers over directory-name guesses.

## Establish the source

Record:

- Payload version, repository revision, package manager, database adapter, and deployment runtime;
- the config entry and how environment or plugin composition changes it;
- generated `payload-types.ts`, migrations, and any schema snapshots;
- consumers of Payload's Local API, REST, GraphQL, generated types, auth, jobs, and hooks;
- data volume, locales, drafts/versions, uploads, scheduled work, and external identities.

Useful discovery searches include:

```sh
rg -n "buildConfig|collections:|globals:|plugins:|db:|editor:" .
rg -n "access:|hooks:|endpoints:|auth:|upload:|versions:|localized:" src
rg -n "payload\.(find|findByID|create|update|delete|login|jobs)" .
rg --files | rg "payload-types|migrations|payload\.config|collections|globals"
```

Inspect composed helpers and imported plugins. A short config file may execute into a large schema.

## Migration ledger

Use one row per observable contract:

| Source evidence | Required behavior | Ridu mapping | Status | Data action | Proof |
| --- | --- | --- | --- | --- | --- |
| Exact file/runtime/type/migration | What users or consumers observe | Public Ridu owner | supported/adapt/blocked/excluded | preserve/transform/rebuild/drop | Test, count, browser flow, or compiled consumer |

Inventory nested fields independently when validation, localization, access, relationships, or
hooks differ. Record hooks by phase and whether they perform external side effects. Record access
rules for anonymous, authenticated, role/tenant-filtered, document-specific, and field-redacted
cases.

## Secrets and identity

Payload password hashes, sessions, verification/reset tokens, API keys, and plugin credentials are
not ordinary document values. Prefer re-authentication, invitation, or password reset unless the
installed Ridu release has an explicit reviewed credential-import contract. Preserve stable IDs
where relationships or external consumers depend on them, and record every remap.
