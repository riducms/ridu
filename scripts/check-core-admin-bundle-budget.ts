import { resolve } from "node:path";

interface ManifestChunk {
	file: string;
	css?: string[];
	imports?: string[];
	isEntry?: boolean;
}

const buildRoot = resolve(import.meta.dir, "../.ridu/check/admin-assets");
const manifest = (await Bun.file(resolve(buildRoot, ".vite/manifest.json")).json()) as Record<
	string,
	ManifestChunk
>;
const entryKey = Object.keys(manifest).find((key) => manifest[key]?.isEntry);
if (entryKey === undefined) throw new Error("core admin build manifest has no entry chunk");

function staticClosure(key: string, seen = new Set<string>()) {
	if (seen.has(key)) return seen;
	seen.add(key);
	for (const imported of manifest[key]?.imports ?? []) staticClosure(imported, seen);
	return seen;
}

async function gzipSize(path: string) {
	return Bun.gzipSync(await Bun.file(resolve(buildRoot, path)).bytes()).byteLength;
}

const initialChunks = staticClosure(entryKey);
const initialJS = await Promise.all(
	[...initialChunks].map((key) => gzipSize(manifest[key]?.file ?? key))
);
const initialCSSFiles = new Set([...initialChunks].flatMap((key) => manifest[key]?.css ?? []));
const initialCSS = await Promise.all([...initialCSSFiles].map(gzipSize));
const asyncChunks = Object.entries(manifest).filter(
	([key, chunk]) => chunk.file.endsWith(".js") && !initialChunks.has(key)
);
const asyncSizes = await Promise.all(asyncChunks.map(([, chunk]) => gzipSize(chunk.file)));

const measurements = {
	initialJS: initialJS.reduce((total, size) => total + size, 0),
	initialCSS: initialCSS.reduce((total, size) => total + size, 0),
	largestAsyncJS: Math.max(0, ...asyncSizes),
};
// The English interface catalog and data-router navigation blocking are part of the first usable
// screen; optional project languages remain application-owned imports. Keep the explicit allowance
// tight enough to catch unrelated growth. Host-owned scalar-editor lifetimes and registration
// validation add a reviewed 1 KiB allowance (295,106 measured bytes against the former 294,912 cap).
// Application contribution validation and app translations add a further 1 KiB allowance
// (296,389 measured bytes against the former 295,936 cap), with no new runtime dependencies.
// Paired plugin identity/config checks and advanced occurrence bindings add 3 KiB
// (299,770 measured bytes), reusing the form controller with no new runtime dependencies.
// Repeating-row mount lifetimes reuse schema identity correlation and add a 1 KiB allowance
// (300,037 measured bytes), with no new production dependencies.
// Opt-in live-validation scheduling, cancellation, input leases, feedback and SDK transport
// measure 303,028 bytes. Extend the allowance by 2 KiB; no new runtime dependencies are added.
// Primitive list controls, typed bindings, and positional-feedback invalidation add 2 KiB of
// budget (302,502 measured bytes, up from 300,311 at the preceding checkpoint). They reuse the
// existing form controller and controls without new runtime dependencies.
// The combined live-validation and primitive-list build measures 305,288 bytes. Both branches
// independently consumed the same 296 KiB cap; allow 3 KiB for their combined controls and typed
// input availability checks, preserving the CSS/async caps and adding no runtime dependencies.
// Compact block registries and lazy placement/extension views add 1 KiB of allowance
// (307,052 measured bytes), with no new dependencies or changes to authorization.
// Optional block-name headers, guarded embedded callbacks and shared occurrence indexes measure
// 308,900 bytes (+1,848). Allow 2 KiB for these controls; nested editors remain lazy and this adds
// no runtime dependencies. Preserve the CSS and async caps.
// Router 0.1 and Svelte 5.57, including native route errors and primary-viewport scroll
// restoration, measure 312,009 bytes. Allow 3 KiB for this dependency/API migration
// and its admin integration; keep the CSS and async caps unchanged.
// Request/session ownership, schema-access refresh and resource disposal measure 312,735 bytes
// (+600 over the preceding admin build). Allow 1 KiB for these reproduced lifetime fixes;
// keep CSS/async caps unchanged and add no runtime dependency or generic request framework.
// Atomic collection-page capabilities, independent status-count lifetimes and populated list
// labels measure 313,749 bytes. Allow 1 KiB for the new SDK decoder and controller behavior;
// the formatter removes browser fan-out and adds no runtime dependency.
// Atomic route snapshots, exact seed brokering, startup gating and retained-navigation progress
// measure 321,119 bytes. Allow 8 KiB for this route-wide contract; route-specific editors remain
// async and production still adds no JavaScript server runtime.
// Auth SCSS, public control styles and explicit cascade layers measure about 21,107 gzip bytes of CSS
// during the UnoCSS migration. Allow 1 KiB beyond the former 20 KiB cap. System typography
// removes the bundled font files; the JS/async budgets remain unchanged.
// The sidebar/dashboard migration measures 22,551 CSS bytes (+1,444 over the auth baseline).
// Semantic shell styles replace its utilities and legacy sidebar tokens; unmigrated families
// still share the UnoCSS sheet. Allow 2 KiB for this slice; keep both JavaScript caps unchanged.
// The list, Select and Checkbox SCSS migration measures 24,207 CSS bytes (+1,656), while
// initial JS falls to 316,573 bytes. Allow 1 KiB more CSS; no dependency or JS-cap increase.
// Reference drawer/join presentation and shared list controls measure 24,851 initial
// CSS bytes. Allow 0.5 KiB beyond the list baseline; drawer-only CSS remains lazy,
// and neither JavaScript budget changes.
// Document field controls, retained editor lifetimes and semantic popup/tab styles measure
// 324,329 initial JS bytes and 25,764 initial CSS bytes. Allow 2 KiB JS and 1 KiB CSS for
// this document-editor slice. CodeMirror and syntax support remain interaction-loaded;
// the largest async chunk is 92,196 bytes, within the unchanged 150 KiB cap.
// Isolating relationship surfaces from dnd-kit's popover reset measures 324,615 JS bytes
// (+18 over the preceding build). Allow 0.25 KiB for the drag container; CSS and async caps stay fixed.
// Collection API reference adds the drawer shell and translated documentation strings.
// Measured initial JS is 329,366 bytes and CSS 26,392; allow 5 KiB JS and 0.5 KiB CSS.
// Schema projection, examples and the reference body load only on interaction; no new dependency.
// Version history/comparison now loads in its own prepared route group. Initial JS falls to
// 328,202 bytes; shared table/control CSS chunking measures 26,723 bytes. Allow 0.25 KiB CSS
// beyond the previous cap; keep JavaScript budgets unchanged and the comparison styles lazy.
// Completing shared controls and built-in fields in semantic SCSS measures 27,884 initial CSS
// bytes. Allow 1 KiB for the component-owned rules; calendar styles remain interaction-loaded.
// Removing tailwind-variants reduces initial JS to 313,266 bytes. Keep both JS caps unchanged.
// Bulk workflows move both editors behind lazy boundaries, reducing initial JS to 312,082 bytes.
// The collection loading/error shell and changed shared CSS chunk boundaries measure 28,441
// initial CSS bytes (+557). Allow 0.75 KiB; workspace/picker CSS remains lazy and JS caps stay fixed.
const budgets = { initialJS: 322.25 * 1024, initialCSS: 28 * 1024, largestAsyncJS: 150 * 1024 };
const exceeded = Object.entries(budgets).filter(
	([name, budget]) => measurements[name as keyof typeof measurements] > budget
);

for (const [name, size] of Object.entries(measurements)) {
	console.log(
		`coreAdmin.${name}: ${size.toLocaleString()} gzip bytes (budget ${budgets[name as keyof typeof budgets].toLocaleString()})`
	);
}
if (exceeded.length > 0) {
	for (const [name, budget] of exceeded) {
		console.error(
			`Core admin ${name} bundle is ${measurements[name as keyof typeof measurements].toLocaleString()} gzip bytes; budget is ${budget.toLocaleString()}.`
		);
	}
	process.exit(1);
}
