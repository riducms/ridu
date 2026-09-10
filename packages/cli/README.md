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

Go users can install the same command directly. Match the version to the project’s `@riducms/cli`:

```sh
go install github.com/riducms/ridu/cmd/ridu@v0.2.0
```

Set `RIDU_BINARY` to an already-installed binary path when downloads are managed centrally.

## First download

On a cache miss, the launcher immediately announces the release and platform on stderr. A terminal
gets one animated line with elapsed time and received bytes; a percentage appears only when the
server supplies a usable total. Verification and installation have their own status messages.
Cached binaries and `RIDU_BINARY` skip this output, and the native command retains stdout.

Piped output, CI, `TERM=dumb`, `NO_COLOR`, and `RIDU_ACCESSIBLE=1` use plain lines at startup,
every five seconds while waiting, and at stage changes. Each request times out after 30 seconds
without a response or further data, including while waiting for checksums. For slow connections,
set `RIDU_CLI_DOWNLOAD_TIMEOUT_MS` to a larger positive millisecond value. Failed downloads are
not cached; retry after checking connectivity and access to the release URL printed in the error.

The package runner downloads `create-ridu` and `@riducms/cli` before this code can execute, so its
registry/bootstrap output is controlled by npm, Bun, pnpm, or Yarn. Once downloaded, the native
command owns the wizard and template setup, announces Go dependency resolution and inherits its
output. Frontend installation is a separate next step printed by `ridu new`.

## Preview the download locally

From the repository root on macOS or Linux:

```sh
bun run --cwd packages/cli preview:download
bun run --cwd packages/cli preview:download --unknown-size
bun run --cwd packages/cli preview:download --timeout
bun run --cwd packages/cli preview:download --non-tty
```

The preview takes about ten seconds, using the actual launcher against a delayed loopback HTTP
server and a disposable cache. It uses no internet connection, never executes the fixture binary,
creates no project, and removes its cache afterward. The timeout scenario intentionally exits
unsuccessfully. Flags can be combined to inspect errors or unknown sizes in plain output.
