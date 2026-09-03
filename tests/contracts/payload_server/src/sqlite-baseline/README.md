# Payload SQLite side-by-side baseline

This compact fixture is the Payload half of Ridu's pinned SQLite comparison. It intentionally
uses Payload `3.87.0`, the version locked by this fixture, and remains separate from the PostgreSQL
performance migrations in `../migrations`.

`bun run check:sqlite-side-by-side:payload` copies the committed migration history to a temporary
directory and asks Payload to create a migration with `--skip-empty`. A new artifact fails the
check. It then applies the committed migration twice to a fresh SQLite file, reverses and reapplies
it, and proves:

- one migration ledger entry after idempotent replay;
- create, read by ID, filtered list, update, and delete;
- populated author and category relationships; and
- two retained versions after create and update.

The matching Ridu contract makes the same assertions, including down/reapply ledger state, against
a fresh Ridu SQLite database. From the repository root, `make sqlite-payload-baseline-test` runs
both halves together.
Regenerate the committed Payload artifact only when this compact schema intentionally changes:

```sh
bun run generate:sqlite-baseline-migration
```
