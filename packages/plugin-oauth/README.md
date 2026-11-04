# `@riducms/plugin-oauth`

The admin consent page for Ridu's Go OAuth plugin (`github.com/riducms/ridu/plugins/oauth`). An MCP
client such as Claude's connector sends the person's browser to `/admin/oauth/authorize`; after
signing in, they see which app is asking, choose the user it acts as, and allow or deny it. The page
then returns the browser to the app.

Install it in the admin workspace and register the Go plugin in the executable config:

```sh
bun add --cwd admin @riducms/plugin-oauth
```

The generated registry imports `oauthAdminPlugin`; do not register it again. The page has no
navigation entry: people reach it only from a client's link. They review and revoke connections
under **Connected apps**, the plugin's `oauth-connections` collection.

See the [MCP guide](../../website/src/content/docs/mcp.md#oauth) for setup and security.
