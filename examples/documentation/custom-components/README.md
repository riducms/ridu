# Custom component documentation examples

These files supply the complete examples in `website/src/content/docs/custom-components/`
and its overview. Each `*.config.ts` is a separate `admin/src/admin.config.ts` example.
`ridu.plugins.generated.ts` stands in for an application's generated plugin imports.

The website example tests compare each published snippet with its source here.
`bun run --cwd website check:examples` compiles the Svelte and TypeScript examples;
`go test ./examples/documentation/custom-components/...` exercises the Go collections.

Format the examples with the website's Prettier settings when changing published code.
