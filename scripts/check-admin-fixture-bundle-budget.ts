import { readdir } from "node:fs/promises";
import { createHash } from "node:crypto";
import { resolve } from "node:path";

const assetsRoot = resolve(import.meta.dir, "../.ridu/admin-fixture-build/assets");
const files = await readdir(assetsRoot);
const bootstrapRoot = resolve(assetsRoot, "..");

interface ManifestChunk {
	file: string;
	css?: string[];
	imports?: string[];
}
const manifest = (await Bun.file(resolve(bootstrapRoot, ".vite/manifest.json")).json()) as Record<
	string,
	ManifestChunk
>;
const chunkByFile = new Map(Object.values(manifest).map((chunk) => [chunk.file, chunk]));
const bootstrap = (await Bun.file(resolve(bootstrapRoot, "ridu-admin-bootstrap.json")).json()) as {
	protocolVersion: number;
	buildId: string;
	documentViewRoutes: string[];
	extensionRoutes: string[];
	listCellFields: Record<string, string[]>;
	moduleGroups: Record<string, string[]>;
	replacedCoreViews: string[];
	seedableCoreSurfaces: string[];
	dashboardLoaders: string[];
	routeLoaders: Record<string, string>;
	viewLoaders: Record<string, string>;
};

// These assertions keep the fixture representative of application replacements,
// additive UI, and loaders; otherwise the size and closure checks could pass vacuously.
if (JSON.stringify(bootstrap.documentViewRoutes) !== JSON.stringify(["posts:insights"]))
	throw new Error("admin bootstrap metadata did not evaluate the fixture document view");
if (
	JSON.stringify(bootstrap.extensionRoutes) !==
	JSON.stringify(["editorial-report", "graphql", "plugin-contract"])
)
	throw new Error("admin bootstrap metadata did not evaluate the fixture extension routes");
const expectedReplacements = [
	"account:*",
	"collectionCreate:loader-records",
	"collectionCreate:payload-only-capabilities",
	"collectionEdit:loader-records",
	"collectionEdit:payload-only-capabilities",
	"collectionList:loader-records",
	"collectionList:payload-only-capabilities",
	"global:loader-summary",
	"global:site-settings",
	"login:*",
	"notFound:*",
];
if (JSON.stringify(bootstrap.replacedCoreViews) !== JSON.stringify(expectedReplacements))
	throw new Error("admin bootstrap metadata did not match the fixture's replaced core views");
if (JSON.stringify(bootstrap.listCellFields) !== JSON.stringify({ posts: ["readingMinutes"] }))
	throw new Error("admin bootstrap metadata did not evaluate the fixture's additive list cells");
if (bootstrap.seedableCoreSurfaces.includes("login"))
	throw new Error("admin bootstrap metadata kept the replaced login surface seedable");
if (!bootstrap.seedableCoreSurfaces.includes("setup"))
	throw new Error("admin bootstrap metadata omitted the first-user setup surface");

const groupRoots: Record<string, RegExp> = {
	entry: /^assets\/index-.+\.js$/,
	document: /^assets\/document-route-.+\.js$/,
	versions: /^assets\/version-history-route-.+\.js$/,
	date: /^assets\/date-value-control-.+\.js$/,
	"upload-preview": /^assets\/upload-control-.+\.js$/,
	"bulk-upload": /^assets\/bulk-upload-route-.+\.js$/,
	"document-api": /^assets\/document-api-view-.+\.js$/,
};

for (const group of Object.keys(groupRoots)) {
	if (!bootstrap.moduleGroups[group]?.length)
		throw new Error(`admin bootstrap omitted required module group ${group}`);
}

for (const [group, assets] of Object.entries(bootstrap.moduleGroups)) {
	if (!assets.some((asset) => groupRoots[group]?.test(asset)))
		throw new Error(`admin bootstrap group ${group} omitted its logical root`);
	for (const asset of assets) {
		if (!/^assets\/.+-[A-Za-z0-9_-]{8}\.(?:css|js)$/.test(asset))
			throw new Error(`admin bootstrap group ${group} references unhashed asset ${asset}`);
		if (!(await Bun.file(resolve(bootstrapRoot, asset)).exists()))
			throw new Error(`admin bootstrap group ${group} references missing asset ${asset}`);
		if (/reference-browser|slider-content|image-editor|api-reference-content/.test(asset))
			throw new Error(`interaction-only chunk ${asset} leaked into bootstrap group ${group}`);
	}

	// Reconstruct each group's static JS and CSS closure from Vite's manifest. The
	// bootstrap must be complete enough for Go to preload it, but must not absorb
	// dynamic interaction-only chunks into the initial route group.
	const expected = new Set<string>();
	const visit = (file: string) => {
		if (expected.has(file)) return;
		expected.add(file);
		const chunk = chunkByFile.get(file);
		if (chunk === undefined) return;
		for (const imported of chunk.imports ?? []) visit(manifest[imported]!.file);
		for (const css of chunk.css ?? []) expected.add(css);
	};
	for (const asset of assets) if (asset.endsWith(".js")) visit(asset);
	for (const asset of expected)
		if (!assets.includes(asset))
			throw new Error(`admin bootstrap group ${group} omitted transitive asset ${asset}`);
}

// Mirror the producer's canonical identity input. This detects timestamps, output
// paths, or unstable iteration order accidentally entering the runtime cache key.
const bootstrapIdentity = JSON.stringify({
	dashboardLoaders: bootstrap.dashboardLoaders,
	routeLoaders: bootstrap.routeLoaders,
	viewLoaders: bootstrap.viewLoaders,
	protocolVersion: bootstrap.protocolVersion,
	documentViewRoutes: bootstrap.documentViewRoutes,
	extensionRoutes: bootstrap.extensionRoutes,
	listCellFields: bootstrap.listCellFields,
	moduleGroups: bootstrap.moduleGroups,
	replacedCoreViews: bootstrap.replacedCoreViews,
	seedableCoreSurfaces: bootstrap.seedableCoreSurfaces,
});
const expectedBuildID = createHash("sha256").update(bootstrapIdentity).digest("hex").slice(0, 24);
if (bootstrap.buildId !== expectedBuildID)
	throw new Error(`admin bootstrap build ID ${bootstrap.buildId} is not deterministic metadata`);

async function gzipSize(path: string) {
	return Bun.gzipSync(await Bun.file(resolve(assetsRoot, path)).bytes()).byteLength;
}

const entry = files.find((path) => /^index-.+\.js$/.test(path));
if (entry === undefined) throw new Error("admin fixture build has no index JavaScript entry");
const asyncFiles = files.filter((path) => path.endsWith(".js") && path !== entry);
const cssFiles = files.filter((path) => path.endsWith(".css"));
const measurements = {
	entryJS: await gzipSize(entry),
	largestAsyncJS: Math.max(0, ...(await Promise.all(asyncFiles.map(gzipSize)))),
	totalCSS: (await Promise.all(cssFiles.map(gzipSize))).reduce((total, size) => total + size, 0),
};
// The auth SCSS/layer migration measures about 24,082 gzip bytes of fixture CSS; mirror
// the core's 1 KiB transitional CSS allowance without changing either JavaScript cap.
// Sidebar/dashboard SCSS brings fixture CSS to 25,611 bytes (+1,529); mirror the core's
// 2 KiB slice allowance. The entry and async JavaScript budgets are unchanged.
// The list and shared-control migration measures 27,331 CSS bytes (+1,720). Mirror the
// core's 1 KiB CSS allowance and keep both JavaScript caps unchanged.
// Media documents, crop/rendition drawers and shared field SCSS used 29,608 bytes.
// Repeated-field error badges and validation-toast parity add 310 bytes (29,918 total).
// Reference document/list drawers and condensed join tables bring CSS to 31,747 bytes
// (+1,829). Shared list controls remain a single CSS chunk; drawer-only CSS stays lazy.
// Allow 31.25 KiB; keep both JS caps unchanged and forbid interaction-only chunks
// from leaking into any initial route closure.
// Complete document fields, sidebar/tabs, calendar, block/schedule drawers and rich-text
// controls measure 35,149 CSS bytes (+3,402 over the reference-browser baseline).
// Allow 3.5 KiB for these semantic styles; both JavaScript caps remain unchanged.
// The document API inspector and JSON tree bring fixture CSS to 36,543 bytes.
// Allow 1 KiB for their semantic styles; both JavaScript caps remain unchanged.
// Collection API reference brings total CSS to 38,471 bytes. Allow 2 KiB for its drawer,
// responsive operation rail and documentation tables; keep JavaScript caps unchanged.
// Website syntax styling and Martian Mono bring CSS to 42,418 gzip bytes, including
// Vite's inlined small Cyrillic font subset. Allow 4 KiB in lazy CSS; initial CSS and
// both JavaScript caps stay unchanged. Font files and grammars load with the drawer.
// Version table, comparison, nested diffs and picker add 2,632 CSS bytes (45,050 total).
// Allow 2.5 KiB beyond the previous cap. Versions is a lazy prepared route group;
// keep both JavaScript caps unchanged.
// Shared controls, nested fields, document fallbacks and accessible hidden states now own their
// semantic SCSS. Allow 2.5 KiB for these rules beyond the version-view baseline.
// Calendar CSS remains lazy, and both JS caps stay fixed.
// Bulk upload workspace and the shared multi-field drawer bring total CSS to 51,219 bytes.
// Their styles are lazy, with one shared picker/drawer chunk. Allow 3.5 KiB for the new
// workspace and CSS chunk boundaries; both JS caps remain unchanged.
// Rich-text link editing, Markdown/checklist behavior and Payload control SVGs bring
// the lazy editor to about 132,000 gzip bytes. Plugin SCSS brings total CSS to about
// 53,300 bytes. Allow 5 KiB async JS and 2.25 KiB CSS for this reviewed feature slice;
// keep the entry budget fixed and preserve interaction-only chunk closure checks.
// The GraphQL playground route adds 1,329 bytes of lazy CSS (54,578 total); its editor and
// language tools load in one route chunk. Allow 1.5 KiB CSS; both JS caps remain unchanged.
// The fixed rich-text toolbar, its shared selection state and the editor layout options add
// 832 gzip bytes to the lazy editor (133,284 total). Allow 1 KiB async JS; the entry and CSS
// caps remain unchanged.
// Payload block headers, retained disclosure animation and inset group styling measure
// 55,663 CSS bytes, 367 bytes above the previous cap. Allow a bounded 512-byte CSS slice;
// preserve both JavaScript caps and the initial-route closure checks above.
// The list filter's hierarchical field picker (a nested-trail trigger plus a lazily loaded
// panel with search and level navigation) measures 56,467 CSS bytes, 659 above the previous
// cap. Allow a bounded 768-byte CSS slice; both JavaScript caps stay unchanged.
// The standalone editor entry splits the core editor's features and extensions into props and
// opens only the nodes it registers. With its translations and tooltip provider kept in the
// app-only wrapper, the lazy editor measures 134,153 gzip bytes, 47 more than before. Allow
// 256 bytes async JS; the entry and CSS caps remain unchanged.
// The editor's --ridu-richtext-* tokens, and the selectors that give its server-rendered copy the
// editor's own rules, bring CSS to 57,462 bytes, 886 above the previous cap. Allow a bounded
// 1.25 KiB CSS slice; both JavaScript caps stay unchanged.
const budgets = { entryJS: 215 * 1024, largestAsyncJS: 131.25 * 1024, totalCSS: 56.5 * 1024 };

for (const [name, size] of Object.entries(measurements)) {
	console.log(
		`adminFixture.${name}: ${size.toLocaleString()} gzip bytes (budget ${budgets[name as keyof typeof budgets].toLocaleString()})`
	);
	if (size > budgets[name as keyof typeof budgets]) {
		console.error(`Admin fixture ${name} exceeds its bundle budget.`);
		process.exitCode = 1;
	}
}
