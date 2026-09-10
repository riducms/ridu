# Payload playground

This application was created with Payload's blank project generator:

```sh
bunx create-payload-app@3.87.0 payload -t blank --use-bun --no-agent
```

The generator resolved Payload `3.88.0`, Next.js `16.3.0`, and SQLite. The repository keeps the
generated application isolated from Ridu's root Bun workspace.

The generated baseline received a few small repository overlays: Bun is used consistently, the
SQLite environment example matches the selected adapter, the Next.js 16 ESLint configuration uses
its native flat-config exports, and the unused Mongo/pnpm Docker files were removed.

## Run it

```sh
cp .env.example .env # only when .env does not already exist
bun install --frozen-lockfile
bun dev
```

Open <http://localhost:3100/admin> and create the first user when prompted. Alongside Payload's
generated `users` and `media` collections, `workshops` mirrors the Ridu playground's fields,
nested sessions, rich-text Workshop card, and city-in-title validation rule. Its SQLite database,
uploaded media, dependencies, Next.js build output, and local environment are ignored.

## Check it

```sh
bun run generate:types
bun run generate:importmap
bun run lint
bun run build
```

This remains an exploratory UI comparison rather than a compatibility claim. Stable comparisons
belong in `tests/contracts/`; this application is where we can first experiment and learn.
