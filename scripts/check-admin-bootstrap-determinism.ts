import { rm } from "node:fs/promises";
import { resolve } from "node:path";
import { createHash } from "node:crypto";
import { build as viteBuild } from "vite";

const repository = resolve(import.meta.dir, "..");
const first = resolve(repository, ".ridu/admin-bootstrap-determinism-first");
const second = resolve(repository, ".ridu/admin-bootstrap-determinism-second");
process.env.RIDU_FRAMEWORK_ADMIN_FIXTURE = "true";
process.chdir(resolve(repository, "admin"));

try {
	// Two clean output roots expose ordering leaks from module discovery, UnoCSS, or
	// metadata serialization that an incremental rebuild could otherwise conceal.
	await rm(first, { force: true, recursive: true });
	await rm(second, { force: true, recursive: true });
	await build(first);
	await build(second);

	// Keep the determinism assertion meaningful: this fixture must exercise every
	// loader-registration path whose metadata participates in the bootstrap identity.
	const metadata = await Bun.file(resolve(first, "ridu-admin-bootstrap.json")).json();
	if (JSON.stringify(metadata.dashboardLoaders) !== JSON.stringify(["editorial-dashboard"]))
		throw new Error("fixture build did not select its registered dashboard loader");
	if (
		JSON.stringify(metadata.routeLoaders) !==
			JSON.stringify({ "editorial-report": "editorial-view", graphql: "graphql-playground" }) ||
		JSON.stringify(metadata.viewLoaders) !==
			JSON.stringify({
				"collectionCreate:loader-records": "editorial-view",
				"collectionEdit:loader-records": "editorial-view",
				"collectionList:loader-records": "editorial-view",
				"global:loader-summary": "editorial-view",
			})
	)
		throw new Error("fixture build did not select its registered route/view loaders");

	// Equal metadata is insufficient if it points at an omitted or renamed asset.
	for (const assets of Object.values(metadata.moduleGroups) as string[][]) {
		for (const asset of assets) {
			if (!(await Bun.file(resolve(first, asset)).exists()))
				throw new Error(`bootstrap references missing asset ${asset}`);
		}
	}

	// Hash every emitted file so the check covers hashed chunks, extracted CSS, and
	// bootstrap metadata rather than only comparing the metadata document itself.
	const [left, right] = await Promise.all([snapshot(first), snapshot(second)]);
	if (JSON.stringify(left) !== JSON.stringify(right)) {
		const paths = new Set([...Object.keys(left), ...Object.keys(right)]);
		const changed = [...paths].filter((path) => left[path] !== right[path]);
		throw new Error(`admin build is nondeterministic: ${changed.join(", ")}`);
	}
	console.log(`adminBootstrap.deterministicFiles: ${Object.keys(left).length}`);
} finally {
	await rm(first, { force: true, recursive: true });
	await rm(second, { force: true, recursive: true });
}

async function build(outDir: string) {
	await viteBuild({
		configFile: resolve(repository, "admin/vite.config.ts"),
		logLevel: "silent",
		build: { outDir, manifest: true },
	});
}

async function snapshot(root: string) {
	const result: Record<string, string> = {};
	for await (const path of new Bun.Glob("**/*").scan({ cwd: root, onlyFiles: true })) {
		result[path] = createHash("sha256")
			.update(await Bun.file(resolve(root, path)).bytes())
			.digest("hex");
	}
	return Object.fromEntries(
		Object.entries(result).sort(([left], [right]) => left.localeCompare(right))
	);
}
