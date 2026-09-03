# Ridu plugins

Plugins add CMS or admin behavior. Available examples include GraphQL and MCP transports and the
paired Go/Svelte rich-text field.

`mcp/` exposes selected, access-controlled content reads through stateless Streamable HTTP. It
remains a separate module-level import so applications that do not enable MCP do not carry the
protocol runtime.

A backend plugin implements `ridu.Plugin` plus the capability interfaces it needs. Register it in
`Config.Plugins`. A descriptor can add generation metadata and an admin pair, but must never contain
live clients, credentials, or secrets.

Database and object-storage packages are [adapters](../adapters/), not plugins. See the
[Plugin guide](../website/src/content/docs/plugins.md) for installation, capabilities, migrations,
and version compatibility.
