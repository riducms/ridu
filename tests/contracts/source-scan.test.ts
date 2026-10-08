import { expect, test } from "bun:test";
import { mkdir, mkdtemp, realpath, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, resolve } from "node:path";
import { createServer, normalizePath, resolveConfig } from "vite";

import { packageSourceAliasPlugin } from "../../packages/build/src/vite/source-alias";
import { adminSourceScanPlugin } from "../../packages/build/src/vite/source-scan";

test("cold scan discovers the admin source's dependencies before browser requests", async () => {
	const root = await realpath(await mkdtemp(resolve(tmpdir(), "ridu-source-scan-")));
	async function file(path: string, content: string) {
		const target = resolve(root, path);
		await mkdir(dirname(target), { recursive: true });
		await writeFile(target, content);
	}
	async function pkg(name: string, entry: string, source: string) {
		await file(
			`node_modules/${name}/package.json`,
			JSON.stringify({ name, type: "module", exports: `./${entry}` })
		);
		await file(`node_modules/${name}/${entry}`, source);
	}
	try {
		await file("package.json", JSON.stringify({ private: true, type: "module" }));
		await file("index.html", '<script type="module" src="/main.ts"></script>');
		await file("main.ts", 'import "@riducms/admin";');
		await pkg(
			"@riducms/admin",
			"src/index.ts",
			'import "@admin/page.svelte"; import "@riducms/ui"; void import("@admin/editor");'
		);
		await file(
			"node_modules/@riducms/admin/src/page.svelte",
			'<script>import "fixture-router";</script><p>Fixture</p>'
		);
		await file(
			"node_modules/@riducms/admin/src/ignored.test.ts",
			'import "missing-test-only-package";'
		);
		await file(
			"node_modules/@riducms/admin/src/ignored.d.ts",
			'import "missing-declaration-only-package";'
		);
		await file("node_modules/@riducms/admin/src/editor.ts", 'import "fixture-editor";');
		await pkg("@riducms/ui", "src/index.ts", 'import "fixture-theme";');
		for (const name of ["fixture-router", "fixture-theme", "fixture-editor"]) {
			await pkg(name, "index.js", 'export const value = "dependency";');
		}
		const server = await createServer({
			configFile: false,
			root,
			cacheDir: resolve(root, ".cache"),
			logLevel: "silent",
			plugins: [packageSourceAliasPlugin(), adminSourceScanPlugin()],
			optimizeDeps: { exclude: ["@riducms/admin"] },
			server: { middlewareMode: true },
		});
		try {
			expect(server.config.optimizeDeps.exclude).toEqual(["@riducms/admin", "@admin"]);
			const source = normalizePath(resolve(root, "node_modules/@riducms/admin/src"));
			expect(server.config.optimizeDeps.entries).toEqual([
				"index.html",
				`${source}/**/*.{js,ts,svelte}`,
				`!${source}/**/*.d.ts`,
				`!${source}/**/*.{test,spec}.{js,ts,svelte}`,
			]);
			const optimizer = server.environments.client?.depsOptimizer;
			if (!optimizer) throw new Error("Missing client dependency optimizer");
			await optimizer.init();
			await optimizer.scanProcessing;
			const discovered = Object.keys({
				...optimizer.metadata.optimized,
				...optimizer.metadata.discovered,
			});
			// Packaged Ridu libraries are ordinary dependencies, prebundled with their own imports.
			expect(discovered.sort()).toEqual(["@riducms/ui", "fixture-editor", "fixture-router"]);
		} finally {
			await server.close();
		}
	} finally {
		await rm(root, { recursive: true, force: true });
	}
}, 15_000);

test("source scan preserves custom entries and reports an installed admin with a missing entry", async () => {
	const root = await realpath(await mkdtemp(resolve(tmpdir(), "ridu-source-scan-config-")));
	try {
		for (const entries of ["custom.html", ["custom.html", "extra.ts"]]) {
			const config = await resolveConfig(
				{
					configFile: false,
					root,
					plugins: [adminSourceScanPlugin()],
					optimizeDeps: { entries },
				},
				"serve"
			);
			expect(config.optimizeDeps.entries).toEqual(
				typeof entries === "string" ? [entries] : entries
			);
		}
		const directory = resolve(root, "node_modules/@riducms/admin");
		await mkdir(directory, { recursive: true });
		await writeFile(
			resolve(directory, "package.json"),
			JSON.stringify({
				name: "@riducms/admin",
				exports: "./missing.ts",
			})
		);
		await expect(
			resolveConfig(
				{
					configFile: false,
					root,
					plugins: [adminSourceScanPlugin()],
				},
				"serve"
			)
		).rejects.toThrow("@riducms/admin");
	} finally {
		await rm(root, { recursive: true, force: true });
	}
});
