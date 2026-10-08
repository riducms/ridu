import { describe, expect, it } from "bun:test";
import { access, readdir, readFile } from "node:fs/promises";
import { extname, join, relative, resolve } from "node:path";
import ts from "typescript";

const repositoryRoot = resolve(import.meta.dir, "../..");
const sourceExtensions = new Set([".ts", ".svelte"]);
const packagedExtensions = new Set([".js", ".ts", ".svelte"]);

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

	it("keeps build-time and published runtime package graphs separated", async () => {
		const buildViolations = await forbiddenImports(
			"packages/build/src",
			(specifier) => specifier.startsWith("@/") || specifier.startsWith("@riducms/")
		);
		expect(buildViolations).toEqual([]);

		const runtimeRoots = ["packages/sdk/src", "packages/protocol/src", "packages/translations/src"];
		const runtimeViolations = (
			await Promise.all(
				runtimeRoots.map((root) =>
					forbiddenImports(
						root,
						(specifier) =>
							specifier === "@riducms/build" || specifier.startsWith("@riducms/build/"),
						(file) => !/\.(?:test|spec)\.[cm]?[jt]sx?$/u.test(file)
					)
				)
			)
		).flat();
		expect(runtimeViolations).toEqual([]);
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

	it("keeps @riducms/sveltekit on the SDK's public client contract", async () => {
		// The integration binds an application's generated client; it never reaches into admin,
		// plugin, protocol, or build internals, and it adds no second transport.
		const allowed = (specifier: string) =>
			specifier.startsWith("#lib/") ||
			specifier === "@riducms/sdk" ||
			specifier === "svelte" ||
			specifier.startsWith("svelte/") ||
			specifier === "@sveltejs/kit" ||
			specifier === "esm-env" ||
			specifier === "$app/navigation";
		const violations = await forbiddenImports(
			"packages/sveltekit/src",
			(specifier) => !allowed(specifier)
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

	it("keeps migrated controls independent of runtime utility merging", async () => {
		const violations = (
			await Promise.all(
				["admin/src", "packages/ui/src"].map((directory) =>
					forbiddenImports(directory, (specifier) => specifier === "tailwind-variants")
				)
			)
		).flat();

		expect(violations).toEqual([]);
	});

	it("keeps Bits UI behavior in primitive owners and audited field/drawer compositions", async () => {
		// These owners need direct composition: schema-backed single/multiple selection,
		// asynchronous relationship results, and reference/image/API/version/bulk drawer sessions.
		// Their state belongs to the field, reference workflow, or upload draft rather
		// than a second generic controller; Bits retains focus, nesting, and dismissal.
		const compositions = [
			"admin/src/fields/relationship/relationship-quick-picker.svelte",
			"admin/src/fields/select/select-field.svelte",
			"admin/src/fields/nested/block-picker.svelte",
			"admin/src/features/documents/document-schedule.svelte",
			"admin/src/features/collections/bulk/bulk-editor.svelte",
			"admin/src/features/collections/bulk/bulk-editor-loader.svelte",
			"admin/src/features/uploads/upload-control.svelte",
			"admin/src/features/uploads/bulk-upload-route.svelte",
			"admin/src/features/uploads/bulk-upload-editor.svelte",
			"admin/src/features/uploads/bulk-upload-edit-all.svelte",
			"admin/src/features/reference-browser/reference-browser.svelte",
			"admin/src/features/api-reference/api-reference.svelte",
			"admin/src/features/versions/version-comparison.svelte",
		];
		const violations = (await forbiddenImports("admin/src", (specifier) => specifier === "bits-ui"))
			.filter((violation) => !violation.startsWith("admin/src/components/ui/"))
			.filter(
				(violation) => !compositions.some((owner) => violation.startsWith(`${owner} imports `))
			);

		expect(violations).toEqual([]);
	});

	it("keeps admin plugins on public extension contracts", async () => {
		const pluginRoots = [
			"packages/plugin-richtext/src",
			"packages/plugin-seo/src",
			"packages/plugin-form-builder/src",
			"packages/plugin-graphql/src",
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
			"packages/plugin-graphql/src",
		];
		const manualCompositeRole =
			/\brole\s*=\s*(?:["'](?:tree|treeitem|radio|toolbar)["']|\{[^}\n]*["'](?:tree|treeitem|radio|toolbar)["'][^}\n]*\})/gm;
		const violations = (
			await Promise.all(roots.map((root) => forbiddenSource(root, manualCompositeRole)))
		).flat();

		expect(violations).toEqual([]);
	});

	it("imports framework source through each package's own root", async () => {
		// The admin ships as source and imports itself through @admin/. The Svelte packages follow
		// the sv library template: #lib/ imports, which svelte-package turns into relative paths.
		const roots = {
			"admin/src": "@admin/",
			...Object.fromEntries(sveltePackages.map((name) => [`packages/${name}/src/lib`, "#lib/"])),
		};
		const violations = (
			await Promise.all(
				Object.entries(roots).map(([root, ownedPrefix]) =>
					forbiddenImports(
						root,
						(specifier) =>
							specifier.startsWith("./") ||
							specifier.startsWith("../") ||
							specifier.startsWith("@/") ||
							(frameworkAlias(specifier) && !specifier.startsWith(ownedPrefix))
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
		expect(generatedConfig.compilerOptions.paths["@admin/*"]).toEqual([
			"./node_modules/@riducms/admin/src/*",
			"../node_modules/@riducms/admin/src/*",
		]);
		for (const retired of ["@ui/*", "@plugin-richtext/*", "@plugin-seo/*"]) {
			expect(Object.keys(generatedConfig.compilerOptions.paths)).not.toContain(retired);
		}
	});

	it("publishes the Svelte packages as svelte-package output", async () => {
		for (const name of sveltePackages) {
			const manifest = JSON.parse(
				await readFile(join(repositoryRoot, `packages/${name}/package.json`), "utf8")
			) as {
				imports?: unknown;
				exports: Record<string, unknown>;
				files: string[];
				scripts: Record<string, string>;
			};
			expect(manifest.imports).toEqual({ "#lib": "./src/lib/index.js", "#lib/*": "./src/lib/*" });
			expect(JSON.stringify(manifest.exports)).not.toContain("./src/");
			expect(manifest.files).toContain("dist");
			expect(manifest.scripts.package).toStartWith("svelte-kit sync && svelte-package");
			expect(manifest.scripts.package).toEndWith("&& publint");
		}
	});

	it("leaves no source-only imports in packaged output", async () => {
		// build:runtime-packages packages these before the workspace tests run.
		// A stylesheet import would make every application install Sass.
		const sourceOnly = (specifier: string) =>
			specifier.startsWith("#") ||
			specifier.startsWith("~icons/") ||
			specifier.endsWith(".scss") ||
			frameworkAlias(specifier);
		const violations: string[] = [];
		for (const name of sveltePackages) {
			const dist = join(repositoryRoot, "packages", name, "dist");
			for (const file of await sourceFiles(dist, packagedExtensions)) {
				for (const specifier of importSpecifiers(await readFile(file, "utf8"))) {
					if (sourceOnly(specifier)) {
						violations.push(`${relative(repositoryRoot, file)} imports ${specifier}`);
					}
				}
			}
		}

		expect(violations).toEqual([]);
	});

	it("keeps browser modal APIs out of production frontend workflows", async () => {
		const roots = [
			"admin/src",
			"packages/ui/src",
			"packages/plugin-richtext/src",
			"packages/plugin-seo/src",
			"packages/plugin-form-builder/src",
			"packages/plugin-graphql/src",
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
			"controls/filter-builder.svelte",
			"controls/filter-row.svelte",
			"controls/column-picker.svelte",
			"controls/list-toolbar.svelte",
			"controls/list-pagination.svelte",
			"controls/sort-heading.svelte",
			"collection-list-results.svelte",
			"bulk/bulk-actions.svelte",
		];
		const violations = (
			await Promise.all(
				files.map(async (file) => {
					const source = await readFile(
						join(repositoryRoot, "admin/src/features/collections", file),
						"utf8"
					);
					// Rendering a destination supplied by the route is presentation, not router ownership.
					const withoutLink = source.replace(
						/import\s*\{\s*Link\s*\}\s*from\s*["']@hvniel\/svelte-router["'];?/g,
						""
					);
					return importSpecifiers(withoutLink)
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
		const root = join(repositoryRoot, "admin/src/features/collections");
		const violations = (
			await Promise.all(
				(await sourceFiles(root)).map(async (file) => {
					if (file.endsWith("/collection-list-route.svelte")) return [];
					// Link renders an owned destination; hooks and navigation engines belong to the route.
					const source = (await readFile(file, "utf8")).replace(
						/import\s*\{\s*Link\s*\}\s*from\s*["']@hvniel\/svelte-router["'];?/g,
						""
					);
					return importSpecifiers(source)
						.filter((specifier) => specifier === "@hvniel/svelte-router")
						.map((specifier) => `${relative(repositoryRoot, file)} imports ${specifier}`);
				})
			)
		).flat();

		expect(violations).toEqual([]);
	});
});

async function forbiddenImports(
	relativeRoot: string,
	isForbidden: (specifier: string) => boolean,
	includeFile: (file: string) => boolean = () => true
) {
	const absoluteRoot = join(repositoryRoot, relativeRoot);
	const files = await sourceFiles(absoluteRoot);
	const violations: string[] = [];
	for (const file of files) {
		if (!includeFile(file)) continue;
		const source = await readFile(file, "utf8");
		for (const specifier of importSpecifiers(source)) {
			if (isForbidden(specifier)) {
				violations.push(`${relative(repositoryRoot, file)} imports ${specifier}`);
			}
		}
	}
	return violations.sort();
}

async function sourceFiles(
	directory: string,
	extensions: ReadonlySet<string> = sourceExtensions
): Promise<string[]> {
	const entries = await readdir(directory, { withFileTypes: true });
	const files = await Promise.all(
		entries.map((entry) => {
			const path = join(directory, entry.name);
			return entry.isDirectory()
				? sourceFiles(path, extensions)
				: Promise.resolve(extensions.has(extname(entry.name)) ? [path] : []);
		})
	);
	return files.flat();
}

function importSpecifiers(source: string) {
	return ts.preProcessFile(source, true, true).importedFiles.map((file) => file.fileName);
}

const sveltePackages = [
	"ui",
	"plugin",
	"plugin-richtext",
	"plugin-seo",
	"plugin-graphql",
	"plugin-form-builder",
	"sveltekit",
] as const;

function frameworkAlias(specifier: string) {
	return ["@admin/", "@ui/", "@plugin-richtext/", "@plugin-seo/", "#"].some((alias) =>
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
