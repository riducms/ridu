---
name: payload-to-ridu
description: Assess or migrate a Payload CMS application to Ridu while preserving observable schema, access, hook, data, API, and authoring behavior. Use when both Payload and Ridu are in scope; do not imply database, wire, React, Node, or npm-plugin compatibility.
---

# Migrate Payload behavior to Ridu

Translate the behavior users and consumers rely on, not Payload's Node, React, or database
implementation. Ridu's executable Go config becomes authoritative, and its manifest, migrations,
operation engine, transports, generated contracts, and admin must agree.

## Choose the evidence

For every assessment, read:

1. [references/payload-inventory.md](references/payload-inventory.md)
2. [references/concept-map.md](references/concept-map.md)
3. The installed Ridu release's
   [capability reference](../ridu-project/reference/capabilities.md)

Before importing data or proposing cutover, also read:

- [references/proof-and-cutover.md](references/proof-and-cutover.md)
- [references/migration-guide.md](references/migration-guide.md)
- The relevant Ridu contract references routed by the sibling `ridu-project` skill.

## Required workflow

1. Locate the executed Payload config, generated Payload types, migrations, collections/globals,
   plugins, jobs, and real Local API/REST/GraphQL consumers. Record the Payload version and exact
   source revision.
2. Locate `ridu.toml`, executable Go config, generated manifest, migration history, installed
   agent-doc version, and current public capability reference. Run `ridu generate --check` when
   possible.
3. Write a migration ledger with one row per observable contract: source evidence, required
   behavior, Ridu mapping, status (`supported`, `adapt`, `blocked`, or `excluded`), data action, and
   proof.
4. Port one dependency-ordered vertical slice through config, manifest, validation, storage and
   migration, operation engine, transport/generated types, admin, tests, and documentation.
5. Import normalized records through Ridu migration APIs with stable source identities and explicit
   relationship ordering. Do not copy Payload tables or plugin-private storage directly.
6. Prove allowed, filtered, denied, redacted, invalid, migration replay, data-count, relationship,
   upload, locale, draft, and consumer-compilation behavior as applicable.

Never copy Payload password hashes, sessions, API keys, reset or verification tokens as ordinary
content. Stop for direction when behavior changes, access intent is ambiguous, source data is
nondeterministic, a destructive step is required, or no released Ridu contract supports a required
plugin or field behavior.
