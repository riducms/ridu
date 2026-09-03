# Documentation image sources

The `ridu-admin-*.png` and `fields/*.png` files are captured from clean applications created by
the real release-shaped scaffold. The maintained capture definition lives in
`playground/documentation/`; screenshots are product output, not reconstructed interface mocks.

From the repository root, run:

```sh
bun run capture:docs-images
```

The default workflow generates the starter/blank × SQLite/PostgreSQL matrix below the ignored
`playground/.ridu/docs-capture/` directory. It exercises SQLite for the committed database-neutral
screenshots and validates generated artifacts for every project. Use `--sqlite-only` for a faster
local recapture while editing image composition.

The field capture manifest owns all 24 field routes and stable selectors. Playwright fixes the dark
theme, locale, reduced-motion preference, and viewport before cropping real controls. ImageMagick
adds consistent padding, strips metadata, and optimizes the PNGs; it does not draw or reconstruct UI.

Keep screenshots free of local filesystem paths, access tokens, database URLs, and real user data.
Update a page's alt text and caption when a captured state changes materially.
