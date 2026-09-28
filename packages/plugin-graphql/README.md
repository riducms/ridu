# `@riducms/plugin-graphql`

The admin GraphQL playground for Ridu's Go GraphQL plugin. Admins open **GraphQL** in the
sidebar to write operations with schema-aware completion and validation, run them under the current
admin access rules, inspect responses, and browse the schema docs.

Install it with the Go plugin:

```sh
ridu add graphql \
  --go-package github.com/riducms/ridu/plugins/graphql \
  --admin-package @riducms/plugin-graphql
bun run dev
```

The generated registry imports `graphqlAdminPlugin`; do not register it again. The Go plugin serves
the schema and endpoint through the `graphql-playground` admin loader, so the playground works with
a custom `Path` and never needs public introspection. Set `DisablePlayground` on the Go plugin to
remove the route and loader, then remove this package.

The editor, GraphQL language tools, and docs explorer load only when the playground opens. Its
`ridu-graphql*` presentation is semantic SCSS in the shared component layer; applications can
override those hooks.

See the [GraphQL guide](../../website/src/content/docs/graphql.md#playground) for setup, keyboard
shortcuts, and security.
