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
