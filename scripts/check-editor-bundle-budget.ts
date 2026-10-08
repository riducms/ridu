import { resolve } from "node:path";

// What @riducms/plugin-richtext/editor costs an application: the editor component's chunk and
// everything it imports, beyond SvelteKit's own entry chunks. Measured from the editor fixture's
// production build (tests/contracts/editor_app), so it reflects what an app's bundler produces.
const clientRoot = resolve(
	import.meta.dir,
	"../tests/contracts/editor_app/.svelte-kit/output/client"
);

interface ManifestChunk {
	file: string;
	css?: string[];
	imports?: string[];
}
const manifest = (await Bun.file(resolve(clientRoot, ".vite/manifest.json")).json()) as Record<
	string,
	ManifestChunk
>;

function closure(keys: string[]) {
	const files = new Set<string>();
	const visit = (key: string) => {
		const chunk = manifest[key];
		if (chunk === undefined) throw new Error(`the editor fixture's manifest has no chunk ${key}`);
		if (files.has(chunk.file)) return;
		files.add(chunk.file);
		for (const css of chunk.css ?? []) files.add(css);
		for (const imported of chunk.imports ?? []) visit(imported);
	};
	keys.forEach(visit);
	return files;
}

const editor = Object.keys(manifest).filter((key) =>
	key.endsWith("/plugin-richtext/dist/editor/rich-text-app-editor.svelte")
);
const kit = Object.keys(manifest).filter((key) =>
	/\/runtime\/client\/entry\.js$|\/client-optimized\/app\.js$/.test(key)
);
if (editor.length !== 1 || kit.length !== 2)
	throw new Error("the editor fixture's build didn't split the editor and SvelteKit as expected");

const loadedByKit = closure(kit);
const editorFiles = [...closure(editor)].filter((file) => !loadedByKit.has(file));

async function gzipSize(file: string) {
	return Bun.gzipSync(await Bun.file(resolve(clientRoot, file)).bytes()).byteLength;
}
const measurements = { js: 0, css: 0 };
for (const file of editorFiles)
	measurements[file.endsWith(".css") ? "css" : "js"] += await gzipSize(file);

// With Lexical, Prism and the toolbars, slash menu, link editor and block handle, the editor
// measures 210,428 gzip bytes of JavaScript and 6,919 of CSS. Phones on slow networks pay this
// before the editor appears, so the budgets sit just above it, like the admin's: growth is a
// reviewed decision recorded here.
const budgets = { js: 206 * 1024, css: 7 * 1024 };

for (const [name, size] of Object.entries(measurements)) {
	const budget = budgets[name as keyof typeof budgets];
	console.log(
		`editor.${name}: ${size.toLocaleString()} gzip bytes (budget ${budget.toLocaleString()})`
	);
	if (size > budget) {
		console.error(`The rich-text editor's ${name} exceeds its bundle budget.`);
		process.exitCode = 1;
	}
}
