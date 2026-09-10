# Payload live-validation investigation

Isolated executable evidence for Ridu's opt-in live server validation. This is not in the root Bun
workspace, Go workspace, production imports, or default checks.

Investigated on 2026-09-08 with **Payload 3.88.0**, Next 16.2.6, React 19.2.6, Bun 1.4.0, and
headless installed Google Chrome. The npm `latest` metadata selected 3.88.0 and contained no
`gitHead`. Git tag `v3.88.0` is annotated tag `c54dea8f4010d9cb194780f2ee1e4b3ec697f9be`, pointing
to source commit `fea6f8a47a50ff1330d8a5071b43e7dcffb97b22`.

`observations.json` is a compact, non-sensitive record of the captured results. Raw traces and
screenshots are disposable `.evidence/` output. The trace route is intentionally a loopback fixture
facility; never deploy this app.

From this directory:

```sh
bun install --frozen-lockfile
mkdir -p .evidence
bun -e 'import { randomBytes } from "node:crypto"; await Bun.write(".evidence/runtime.env", `PAYLOAD_SECRET=${randomBytes(32).toString("hex")}\nPAYLOAD_TEST_PASSWORD=${randomBytes(20).toString("hex")}\n`)'
set -a
source .evidence/runtime.env
set +a
bun run generate
bun run check
bun run dev
```

The app binds only `127.0.0.1:3417`, creates a dedicated local `fixture.db`, and seeds one disposable
user and product. In another terminal in this directory, run `bun investigate.ts` followed by
`bun context-probe.ts`. These use Playwright with the installed Chrome channel, submit deliberately
invalid writes, and leave the seeded product unchanged. Do not run both probes concurrently: they
share the trace log. Stop the development server with Ctrl-C afterward. To start from scratch,
remove this fixture's `fixture.db` and `.evidence/` only.

All settings, dependencies, database files, cache, and raw evidence stay here. Secrets are generated
per investigation and ignored. No sibling Payload repository is modified.
