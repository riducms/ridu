# `@riducms/build`

Build-time configuration for Ridu admin applications and Svelte plugins.

It provides the UnoCSS preset, Svelte preprocessing for Vite, `@/` and Ridu source aliases, Vite
library externalisation, icon setup, dependency deduplication, and schema reload during development.
Pass application or plugin options to its factories instead of copying config files. Frontend
packages do not need a separate `svelte.config.js`.

The application config keeps framework Svelte packages out of dependency prebundling so their
source aliases and preprocessing work during development. It also prebundles React's CommonJS
entrypoints used by the dynamically loaded Lexical integration. Additional application exclusions
can be supplied through `optimizeDepsExclude`.

Use `@riducms/ui` for runtime components and variants.
