# `@riducms/cli`

This package runs the native Go CLI for the matching Ridu release:

```sh
npx @riducms/cli new my-content
bunx @riducms/cli new my-content
pnpm dlx @riducms/cli new my-content
yarn dlx @riducms/cli new my-content
```

For the conventional project-creation entry point, use `npm create ridu@latest my-app`. The
`create-ridu` initializer delegates to this package with the `new` command; this package remains
the launcher for `ridu version`, `dev`, `generate`, migrations, and every other CLI command.

The launcher downloads and caches the matching native command for macOS, Linux, or Windows. It
forwards arguments, signals, and exit status to that command.

New scaffolds pin this package at the exact coordinated version. From a generated project, use:

```sh
npm install
npm run dev
```

Use the non-writing form separately in CI to verify committed contracts:

```sh
npm run ridu -- generate --check
```

The scaffold records npm, Bun, pnpm, or Yarn in `ridu.toml`, and the native CLI uses it for frontend
installs and admin/plugin commands. The package-local command avoids global version drift while
still using the ordinary npm registry.

Go users can install the same command directly:

```sh
go install github.com/riducms/ridu/cmd/ridu@v0.1.0
```

Set `RIDU_BINARY` to an already-installed binary path when downloads are managed centrally.
