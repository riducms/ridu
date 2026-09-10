# `create-ridu`

Create a project with the native Ridu CLI from the same Ridu release:

```sh
npm create ridu@latest my-app
bun create ridu@latest my-app
pnpm create ridu@latest my-app
yarn create ridu my-app
```

Omit `my-app` to choose the project directory inside the wizard.

The initializer detects npm, Bun, pnpm, or Yarn and records that choice in the generated
`ridu.toml`. Options after a launcher's argument separator are forwarded to `ridu new`:

```sh
npm create ridu@latest -- --template blank --database sqlite my-app
```

The initializer runs the matching `@riducms/cli` release. Generated projects retain that launcher
as an exact development dependency. After
creation, install and run scripts with the same package manager. For example, npm users run
`npm install`, `npm run dev`, and `npm run ridu -- <command>` without installing a global CLI.

On the first run, the launcher shows the native CLI download status immediately, with elapsed time
and received bytes. A percentage appears when the server supplies a usable total. CI and piped
output receive plain progress lines every five seconds. Downloads fail with recovery instructions
after 30 seconds without a response or further data; set `RIDU_CLI_DOWNLOAD_TIMEOUT_MS` to a larger
millisecond value on a slow connection. A cached CLI starts without download messages.

Any earlier registry download is managed by your package runner, before `create-ridu` can print.
See the [launcher README](../cli/README.md) for download details and a local preview command.
