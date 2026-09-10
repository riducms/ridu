import { existsSync } from "node:fs";
import { dirname, join, parse, resolve } from "node:path";
import type { Plugin } from "vite";

export const frameworkAliases = ["@admin/", "@ui/", "@plugin-richtext/", "@plugin-seo/"] as const;

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

			const alias = frameworkAliases.find((candidate) => source.startsWith(candidate));
			if (alias === undefined || importer === undefined) return;
			const sourceRoot = findPackageSourceRoot(cleanID(importer), sourceRoots);
			if (sourceRoot === undefined) return;
			return this.resolve(resolve(sourceRoot, source.slice(alias.length)), importer, {
				skipSelf: true,
			});
		},
	};
}

function cleanID(id: string) {
	return id.replace(/^\0/, "").split("?", 1)[0] ?? id;
}

function findPackageSourceRoot(id: string, cache: Map<string, string>) {
	if (!id.startsWith("/")) return;

	let directory = dirname(id);
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
