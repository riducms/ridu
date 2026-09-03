# `@riducms/build`

Build-time configuration for Ridu admin applications and Svelte plugins.

It provides the UnoCSS preset, Svelte preprocessing for Vite, `@/` and Ridu source aliases, Vite
library externalisation, icon setup, dependency deduplication, and schema reload during development.
Pass application or plugin options to its factories instead of copying config files. Frontend
packages do not need a separate `svelte.config.js`.

Use `@riducms/ui` for runtime components and variants.
