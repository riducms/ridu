import { existsSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname, join, resolve } from "node:path";
import { normalizePath, type Plugin } from "vite";

import { adminAlias } from "./source-alias.js";

/** The admin ships as source; every other Ridu package ships svelte-package output. */
export const adminPackage = "@riducms/admin";

/** Scan the admin's source without prebundling its @admin/ imports or Svelte preprocessing. */
export function adminSourceScanPlugin(): Plugin {
	return {
		name: "ridu-admin-source-scan",
		apply: "serve",
		config(config) {
			const root = resolve(config.root ?? process.cwd());
			const require = createRequire(resolve(root, "package.json"));
			const entries = config.optimizeDeps?.entries === undefined ? ["index.html"] : [];
			const sourceEntries: string[] = [];
			try {
				const sourceRoot = normalizePath(dirname(require.resolve(adminPackage)));
				sourceEntries.push(
					`${sourceRoot}/**/*.{js,ts,svelte}`,
					`!${sourceRoot}/**/*.d.ts`,
					`!${sourceRoot}/**/*.{test,spec}.{js,ts,svelte}`
				);
			} catch (error) {
				// An application without the admin has no admin source to scan; a broken install fails.
				const installed = require.resolve
					.paths(adminPackage)
					?.some((path) => existsSync(join(path, adminPackage, "package.json")));
				if ((error as NodeJS.ErrnoException).code !== "MODULE_NOT_FOUND" || installed) throw error;
			}
			return {
				optimizeDeps: {
					// Vite merges these additions with any application-provided scan entries.
					entries: [...entries, ...sourceEntries],
					// Each source file is an entry, so the alias must not become a dependency bundle.
					exclude: [adminAlias.slice(0, -1)],
				},
			};
		},
	};
}
