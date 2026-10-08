import { existsSync } from "node:fs";
import { dirname, isAbsolute, join, parse, resolve } from "node:path";
import type { Plugin } from "vite";

// The admin ships as source and imports itself through @admin/. Ridu's Svelte packages ship
// svelte-package output with relative imports, so they need no aliases.
export const adminAlias = "@admin/";

/** Resolve the application's @/ and the admin's @admin/ to their own src directories. */
export function packageSourceAliasPlugin(): Plugin {
	const sourceRoots = new Map<string, string>();
	let applicationSourceRoot = "";

	return {
		name: "ridu-package-source-alias",
		enforce: "pre",
		configResolved(config) {
			applicationSourceRoot = resolve(config.root, "src");
		},
		async resolveId(source, importer) {
			if (source.startsWith("@/")) {
				return this.resolve(resolve(applicationSourceRoot, source.slice(2)), importer, {
					skipSelf: true,
				});
			}

			if (!source.startsWith(adminAlias) || importer === undefined) return;
			// The admin's src, wherever the package that imports @admin/ is installed.
			const sourceRoot = findPackageSourceRoot(importer, sourceRoots);
			if (sourceRoot === undefined) return;
			return this.resolve(resolve(sourceRoot, source.slice(adminAlias.length)), importer, {
				skipSelf: true,
			});
		},
	};
}

/**
 * The file a Vite module ID names, without its \0 prefix or query, or undefined for a virtual
 * module. Windows IDs start with a drive (C:/…) rather than /.
 */
export function moduleFilePath(id: string, isAbsolutePath = isAbsolute) {
	const path = id.replace(/^\0/, "").split("?", 1)[0] ?? id;
	return isAbsolutePath(path) ? path : undefined;
}

export function findPackageSourceRoot(id: string, cache: Map<string, string>) {
	const path = moduleFilePath(id);
	if (path === undefined) return;

	let directory = dirname(path);
	const cached = cache.get(directory);
	if (cached) return cached;

	const visited: string[] = [];
	const filesystemRoot = parse(directory).root;
	while (directory !== filesystemRoot) {
		visited.push(directory);
		if (existsSync(join(directory, "package.json"))) {
			const sourceRoot = join(directory, "src");
			for (const visitedDirectory of visited) cache.set(visitedDirectory, sourceRoot);
			return sourceRoot;
		}
		directory = dirname(directory);
	}
}
