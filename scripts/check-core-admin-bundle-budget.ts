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
const budgets = { initialJS: 300 * 1024, initialCSS: 20 * 1024, largestAsyncJS: 150 * 1024 };
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
