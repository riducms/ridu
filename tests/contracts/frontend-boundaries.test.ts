import { describe, expect, it } from "bun:test";
import { access, readdir, readFile } from "node:fs/promises";
import { extname, join, relative, resolve } from "node:path";
import ts from "typescript";

const repositoryRoot = resolve(import.meta.dir, "../..");
const sourceExtensions = new Set([".ts", ".svelte"]);

describe("frontend package boundaries", () => {
	it("distinguishes module imports from generated application source", () => {
		expect(
			importSpecifiers(`import {build} from 'vite';
export {type Plugin} from 'vite';
const generatedEntry = \`import {resolveAdminConfig, validateAdminManifest} from '@riducms/plugin/admin';\`;
const lazy = () => import('@riducms/admin');`)
		).toEqual(["vite", "vite", "@riducms/admin"]);
		expect(
			importSpecifiers(`<script lang="ts">
import Field from '@riducms/plugin/editor/field';
const lazy = () => import('@riducms/ui');
</script><Field />`)
		).toEqual(["@riducms/plugin/editor/field", "@riducms/ui"]);
	});

	it("keeps @riducms/build out of runtime package graphs", async () => {
		const violations = await forbiddenImports(
			"packages/build/src",
			(specifier) => specifier.startsWith("@/") || specifier.startsWith("@riducms/")
		);

		expect(violations).toEqual([]);
	});

	it("keeps @riducms/plugin independent from the admin application", async () => {
		const violations = await forbiddenImports(
			"packages/plugin/src",
			(specifier) =>
				specifier.startsWith("@/") ||
				specifier === "@riducms/admin" ||
				specifier.startsWith("@riducms/admin/")
		);

		expect(violations).toEqual([]);
	});

	it("keeps @riducms/ui independent from application contracts", async () => {
		const violations = await forbiddenImports("packages/ui/src", (specifier) =>
			[
				"@riducms/admin",
				"@riducms/build",
				"@riducms/plugin",
				"@riducms/protocol",
				"@riducms/sdk",
			].some((prefix) => specifier === prefix || specifier.startsWith(`${prefix}/`))
		);

		expect(violations).toEqual([]);
	});

	it("keeps runtime variant composition behind @riducms/ui", async () => {
		const violations = await forbiddenImports(
			"admin/src",
			(specifier) => specifier === "tailwind-variants"
		);

		expect(violations).toEqual([]);
	});

	it("keeps Bits UI behavior in primitive owners and the audited async picker", async () => {
		const violations = (await forbiddenImports("admin/src", (specifier) => specifier === "bits-ui"))
			.filter((violation) => !violation.startsWith("admin/src/components/ui/"))
			.filter(
				(violation) =>
					!violation.startsWith(
						"admin/src/fields/relationship/relationship-quick-picker.svelte imports "
					)
			);

		expect(violations).toEqual([]);
	});

	it("keeps admin plugins on public extension contracts", async () => {
		const pluginRoots = [
			"packages/plugin-richtext/src",
			"packages/plugin-seo/src",
			"packages/plugin-form-builder/src",
		];
		await Promise.all(pluginRoots.map((root) => access(join(repositoryRoot, root))));
		const violations = (
			await Promise.all(
				pluginRoots.map((root) =>
					forbiddenImports(
						root,
						(specifier) =>
							specifier === "@riducms/admin" ||
							specifier.startsWith("@riducms/admin/") ||
							specifier === "@riducms/build" ||
							specifier.startsWith("@riducms/build/") ||
							specifier === "tailwind-variants" ||
							specifier === "bits-ui" ||
							specifier.includes("/admin/src/")
					)
				)
			)
		).flat();

		expect(violations).toEqual([]);
	});

	it("keeps behavior-heavy composite roles behind audited primitives", async () => {
		const roots = [
			"admin/src",
			"packages/plugin-richtext/src",
			"packages/plugin-seo/src",
			"packages/plugin-form-builder/src",
		];
		const manualCompositeRole =
			/\brole\s*=\s*(?:["'](?:tree|treeitem|radio|toolbar)["']|\{[^}\n]*["'](?:tree|treeitem|radio|toolbar)["'][^}\n]*\})/gm;
		const violations = (
			await Promise.all(roots.map((root) => forbiddenSource(root, manualCompositeRole)))
		).flat();

		expect(violations).toEqual([]);
	});

	it("uses collision-free package-root aliases inside framework source", async () => {
		const roots = {
			"admin/src": "@admin/",
			"packages/ui/src": "@ui/",
			"packages/plugin-richtext/src": "@plugin-richtext/",
			"packages/plugin-seo/src": "@plugin-seo/",
		};
		const violations = (
			await Promise.all(
				Object.entries(roots).map(([root, ownedAlias]) =>
					forbiddenImports(
						root,
						(specifier) =>
							specifier.startsWith("./") ||
							specifier.startsWith("../") ||
							specifier.startsWith("@/") ||
							(frameworkAlias(specifier) && !specifier.startsWith(ownedAlias))
					)
				)
			)
		).flat();

		expect(violations).toEqual([]);
		const generatedConfig = JSON.parse(
			await readFile(
				join(repositoryRoot, "internal/scaffold/templates/admin-tsconfig.json.tmpl"),
				"utf8"
			)
		) as { compilerOptions: { paths: Record<string, string[]> } };
		for (const [alias, packageName] of [
			["@admin/*", "admin"],
			["@ui/*", "ui"],
			["@plugin-richtext/*", "plugin-richtext"],
			["@plugin-seo/*", "plugin-seo"],
		] as const) {
			expect(generatedConfig.compilerOptions.paths[alias]).toEqual([
				`./node_modules/@riducms/${packageName}/src/*`,
				`../node_modules/@riducms/${packageName}/src/*`,
			]);
		}
	});

	it("keeps browser modal APIs out of production frontend workflows", async () => {
		const roots = [
			"admin/src",
			"packages/ui/src",
			"packages/plugin-richtext/src",
			"packages/plugin-seo/src",
			"packages/plugin-form-builder/src",
		];
		const browserModal =
			/(?:\b(?:window|globalThis)\s*(?:(?:\?\.|\.)\s*(?:alert|confirm|prompt)|\[\s*["'](?:alert|confirm|prompt)["']\s*\])\s*(?:\?\.)?\s*\(|(?:^|[^\w.])(?:alert|confirm|prompt)\s*\()/gm;
		const violations = (
			await Promise.all(roots.map((root) => forbiddenSource(root, browserModal)))
		).flat();

		expect(violations).toEqual([]);
	});

	it("keeps collection list presentation outside router and client ownership", async () => {
		const files = [
			"collection-list-filter-popover.svelte",
			"collection-list-results.svelte",
			"collection-list-bulk-actions.svelte",
		];
		const violations = (
			await Promise.all(
				files.map(async (file) => {
					const source = await readFile(
						join(repositoryRoot, "admin/src/features/collections", file),
						"utf8"
					);
					return importSpecifiers(source)
						.filter(
							(specifier) =>
								specifier === "@hvniel/svelte-router" ||
								specifier === "@riducms/sdk" ||
								specifier.includes("/core/api/") ||
								specifier.includes("/core/runtime/")
						)
						.map((specifier) => `${file} imports ${specifier}`);
				})
			)
		).flat();

		expect(violations).toEqual([]);
	});

	it("keeps collection list router ownership in the route composition", async () => {
		const violations = (
			await forbiddenImports(
				"admin/src/features/collections",
				(specifier) => specifier === "@hvniel/svelte-router"
			)
		)
			.filter((violation) => violation.includes("/collection-list-"))
			.filter(
				(violation) =>
					!violation.startsWith(
						"admin/src/features/collections/collection-list-route.svelte imports "
					)
			);

		expect(violations).toEqual([]);
	});
});

async function forbiddenImports(relativeRoot: string, isForbidden: (specifier: string) => boolean) {
	const absoluteRoot = join(repositoryRoot, relativeRoot);
	const files = await sourceFiles(absoluteRoot);
	const violations: string[] = [];
	for (const file of files) {
		const source = await readFile(file, "utf8");
		for (const specifier of importSpecifiers(source)) {
			if (isForbidden(specifier)) {
				violations.push(`${relative(repositoryRoot, file)} imports ${specifier}`);
			}
		}
	}
	return violations.sort();
}

async function sourceFiles(directory: string): Promise<string[]> {
	const entries = await readdir(directory, { withFileTypes: true });
	const files = await Promise.all(
		entries.map((entry) => {
			const path = join(directory, entry.name);
			return entry.isDirectory()
				? sourceFiles(path)
				: Promise.resolve(sourceExtensions.has(extname(entry.name)) ? [path] : []);
		})
	);
	return files.flat();
}

function importSpecifiers(source: string) {
	return ts.preProcessFile(source, true, true).importedFiles.map((file) => file.fileName);
}

function frameworkAlias(specifier: string) {
	return ["@admin/", "@ui/", "@plugin-richtext/", "@plugin-seo/"].some((alias) =>
		specifier.startsWith(alias)
	);
}

async function forbiddenSource(relativeRoot: string, pattern: RegExp) {
	const files = await sourceFiles(join(repositoryRoot, relativeRoot));
	const violations: string[] = [];
	for (const file of files) {
		const source = await readFile(file, "utf8");
		for (const match of source.matchAll(pattern)) {
			const line = source.slice(0, match.index).split("\n").length;
			violations.push(`${relative(repositoryRoot, file)}:${line} uses ${match[0]}`);
		}
	}
	return violations.sort();
}
