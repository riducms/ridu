import { readdir } from "node:fs/promises";
import { resolve } from "node:path";

const repositoryRoot = resolve(import.meta.dir, "..");
const expectedRoot = resolve(repositoryRoot, "internal/adminassets/dist");
const actualRoot = resolve(repositoryRoot, ".ridu/check/admin-assets");
const assetHash = /-[A-Za-z0-9_-]{8}(?=\.(?:css|js)(?:["'`]|$))/g;

async function files(root: string, directory = root): Promise<string[]> {
	const entries = await readdir(directory, { withFileTypes: true });
	const result: string[] = [];
	for (const entry of entries) {
		const absolute = resolve(directory, entry.name);
		if (entry.isDirectory()) result.push(...(await files(root, absolute)));
		else if (entry.isFile()) result.push(absolute.slice(root.length + 1));
	}
	return result.sort();
}

const [generatedFiles, builtFiles] = await Promise.all([files(expectedRoot), files(actualRoot)]);
const expectedFiles = generatedFiles.filter((path) => path !== ".vite/manifest.json");
const actualFiles = builtFiles.filter((path) => path !== ".vite/manifest.json");
const expectedByCanonicalPath = canonicalFiles(expectedFiles);
const actualByCanonicalPath = canonicalFiles(actualFiles);
const expected = new Set(expectedByCanonicalPath.keys());
const actual = new Set(actualByCanonicalPath.keys());
const missing = [...expected].filter((path) => !actual.has(path));
const unexpected = [...actual].filter((path) => !expected.has(path));
const changed: string[] = [];

for (const path of [...expected].filter((candidate) => actual.has(candidate))) {
	const expectedPaths = expectedByCanonicalPath.get(path);
	const actualPaths = actualByCanonicalPath.get(path);
	if (expectedPaths === undefined || actualPaths === undefined) continue;
	const [expectedContents, actualContents] = await Promise.all([
		normalizedContents(expectedRoot, path, expectedPaths),
		normalizedContents(actualRoot, path, actualPaths),
	]);
	if (
		expectedContents.length !== actualContents.length ||
		expectedContents.some((content, index) => !content.equals(actualContents[index]!))
	) {
		changed.push(path);
	}
}

if (missing.length > 0 || unexpected.length > 0 || changed.length > 0) {
	console.error("Embedded admin assets are stale. Run `bun run generate:admin-assets`.");
	for (const path of missing) console.error(`  missing: ${path}`);
	for (const path of unexpected) console.error(`  unexpected: ${path}`);
	for (const path of changed) console.error(`  changed: ${path}`);
	process.exit(1);
}

console.log(`Embedded admin assets match the production build (${expectedFiles.length} files).`);

function canonicalFiles(paths: string[]) {
	const result = new Map<string, string[]>();
	for (const path of paths) {
		const canonical = path.replace(assetHash, "-HASH");
		const matches = result.get(canonical);
		if (matches === undefined) result.set(canonical, [path]);
		else matches.push(path);
	}
	return result;
}

async function normalizedContents(root: string, canonicalPath: string, paths: string[]) {
	const contents = await Promise.all(
		paths.map(async (path) => {
			const bytes = await Bun.file(resolve(root, path)).bytes();
			return Buffer.from(normalize(canonicalPath, bytes));
		})
	);
	return contents.sort(Buffer.compare);
}

function normalize(path: string, bytes: Uint8Array) {
	if (!/\.(?:css|html|js|json)$/.test(path)) return bytes;
	let text = new TextDecoder().decode(bytes).replace(assetHash, "-HASH");
	if (path.endsWith(".css")) {
		text = text.replace(/:root,:host\{([^{}]*)\}/, (_, declarations: string) => {
			const sorted = declarations.split(";").filter(Boolean).sort().join(";");
			return `:root,:host{${sorted}}`;
		});
	}
	return new TextEncoder().encode(text);
}
