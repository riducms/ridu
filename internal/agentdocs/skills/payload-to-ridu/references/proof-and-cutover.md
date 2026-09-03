# Proof and cutover

## Per-slice proof

For every migrated behavior, verify the participating layers and mark non-participating layers
explicitly. Cover as applicable:

- deterministic manifest resolution and generation;
- valid, invalid, anonymous, authenticated, filtered, denied, and redacted operations;
- migration creation/replay and stable-ID data transformation;
- the REST, SDK, or Local API paths used by real consumers;
- admin loading, empty, success, validation, denied, stale/conflict, keyboard, focus, and narrow
  states; and
- generated Go/TypeScript compilation plus `ridu generate --check`.

Do not declare parity from static compilation when the claim is behavioral.

## Data rehearsal

Use a recent restored source snapshot in an isolated target. Record:

- source and target counts by collection, global, status, and locale;
- stable-ID and relationship reconciliation, including polymorphic and nested references;
- upload object existence, checksums, metadata, and access behavior;
- version, draft, and selected-current-document policy;
- rejected rows with stable reasons and an explicit disposition; and
- duration, resumability checkpoint, and recovery after interruption.

Never mutate the production Payload database during rehearsal. Ridu imports still run validation,
hooks, policy, transactions, and field contracts unless a narrowly reviewed migration boundary says
otherwise.

## Cutover gate

Cut over only after:

1. the ledger has no unexplained blocked rows in released scope;
2. destructive findings and credential transition are approved;
3. the final delta/import strategy is rehearsed and bounded;
4. real consumers compile and pass against generated Ridu contracts;
5. backup, restore, rollback, or forward-recovery responsibility is recorded; and
6. operators can verify migrations, jobs, uploads, auth, readiness, and admin access on the exact
   release binary.

Keep Payload read-only or otherwise recoverable until acceptance checks pass. Do not claim
database, wire, plugin, or UI compatibility beyond the ledger's proven behaviors.
