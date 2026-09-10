import { existsSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname, join, resolve } from "node:path";
import { normalizePath, type Plugin } from "vite";

import { frameworkAliases } from "./source-alias.js";

/** Scan source packages without prebundling their aliases or Svelte preprocessing. */
export function packageSourceScanPlugin(packages: readonly string[]): Plugin {
	return {
		name: "ridu-package-source-scan",
		apply: "serve",
		config(config) {
			const root = resolve(config.root ?? process.cwd());
			const require = createRequire(resolve(root, "package.json"));
			const entries = config.optimizeDeps?.entries === undefined ? ["index.html"] : [];
			const sourceEntries: string[] = [];
			for (const name of packages) {
				try {
					const sourceRoot = dirname(require.resolve(name));
					const normalized = normalizePath(sourceRoot);
					sourceEntries.push(
						`${normalized}/**/*.{js,ts,svelte}`,
						`!${normalized}/**/*.d.ts`,
						`!${normalized}/**/*.{test,spec}.{js,ts,svelte}`
					);
				} catch (error) {
					// Official plugins are optional in generated applications.
					const installed = require.resolve
						.paths(name)
						?.some((path) => existsSync(join(path, name, "package.json")));
					if ((error as NodeJS.ErrnoException).code !== "MODULE_NOT_FOUND" || installed)
						throw error;
				}
			}
			return {
				optimizeDeps: {
					// Vite merges these additions with any application-provided scan entries.
					entries: [...entries, ...sourceEntries],
					// Each source file is an entry, so aliases must not become dependency bundles.
					exclude: frameworkAliases.map((alias) => alias.slice(0, -1)),
				},
			};
		},
	};
}
