# Documentation capture playground

This directory supplies focused content to clean applications created by the real Ridu scaffold.
It is not itself a generated project and the website never imports it at runtime.

`bun run capture:docs-images` creates starter and blank projects below the ignored
`playground/.ridu/docs-capture/` directory. The matrix covers SQLite and PostgreSQL; captures use
the SQLite projects so database-neutral images remain quick and reproducible. The field content
and seed command in this directory are copied only into the generated blank SQLite application.

The checked `capture-manifest.json` owns every field slug, document route, stable selector, desired
state, output path, and documentation page. Playwright crops the real admin element. ImageMagick
adds consistent breathing room and strips metadata; it never constructs interface pixels.

The existing `playground/ridu` Payload-comparison baseline and `tests/contracts/admin_server`
browser fixture remain independent and are not documentation image sources.
