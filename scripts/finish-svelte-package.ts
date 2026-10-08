// bun ../../scripts/finish-svelte-package.ts [--lexical] [--watch]
//
// Completes svelte-package output in one of Ridu's Svelte packages; run it from the package
// directory after `svelte-package`. svelte-package preprocesses components and rewrites `#lib`
// imports, but it doesn't run Vite plugins and copies stylesheets unchanged. This writes each
// `~icons/<collection>/<name>` import (unplugin-icons) as a component next to the output, using
// unplugin-icons itself so the markup matches what the admin renders, and points the import at it.
// It compiles each stylesheet a module imports to CSS beside it and imports that instead, so an
// application needs no Sass; the `.scss` files stay for packages that `@use` them. With
// `--lexical`, it also rewrites canonical Lexical imports in rune modules, which svelte-package
// doesn't preprocess. Running it again changes nothing. With `--watch`, it finishes the output
// again whenever `svelte-package --watch` writes to it.
import { watch as watchFiles } from "node:fs";
import { mkdir, readdir, readFile, writeFile } from "node:fs/promises";
import { createRequire } from "node:module";
import { dirname, join, relative, sep } from "node:path";
import { pathToFileURL } from "node:url";
import { initAsyncCompiler, type FileImporter } from "sass-embedded";
import Icons from "unplugin-icons";

interface FinishOptions {
	/** The package directory, which holds its package.json. */
	root: string;
	/** Apply lexical-svelte's import rewrite to `.svelte.js` rune modules. */
	lexical: boolean;
}

/** svelte-package's output directory, relative to the package. */
const dist = "dist";
/** Where generated icon components live; the admin's dependency inventory recognises `~icons/`. */
const iconDirectory = "~icons";
const iconSpecifier = /(["'])~icons\/([a-z0-9-]+)\/([a-z0-9-]+)\1/g;
const stylesheetImport = /(import\s+)(["'])(\.{1,2}\/[^"']+)\.scss\2/g;

async function finishSveltePackage(options: FinishOptions) {
	const output = join(options.root, dist);
	const require = createRequire(join(options.root, "package.json"));
	const files = await outputFiles(output);
	const icons = new IconWriter(output, require);
	const stylesheets = new StylesheetCompiler();
	let lexicalModules = 0;
	const lexicalTransform = options.lexical ? await loadLexicalTransform(require) : undefined;

	for (const file of files) {
		// Declarations keep components' imports too, stylesheets included.
		if (!/\.(svelte|js|ts)$/.test(file) || file.includes(`${sep}${iconDirectory}${sep}`)) continue;
		const original = await readFile(file, "utf8");
		let contents = stylesheets.rewrite(file, await icons.rewrite(file, original));
		if (lexicalTransform !== undefined && file.endsWith(".svelte.js")) {
			const transformed = await lexicalTransform(contents, file);
			if (transformed !== contents) lexicalModules++;
			contents = transformed;
		}
		if (contents !== original) await writeFile(file, contents);
	}
	await stylesheets.compile(files);
	return { icons: icons.written, lexicalModules, stylesheets: stylesheets.written };
}

function watchSveltePackage(options: FinishOptions) {
	let timer: ReturnType<typeof setTimeout> | undefined;
	let running = Promise.resolve();
	const schedule = () => {
		clearTimeout(timer);
		timer = setTimeout(() => {
			running = running
				.then(() => finishSveltePackage(options))
				.then(() => undefined)
				.catch((error: unknown) => console.error(error));
		}, 100);
	};
	schedule();
	// Watch the package root: svelte-package replaces the output directory when it starts.
	return watchFiles(options.root, { recursive: true }, (_event, filename) => {
		if (filename !== null && filename.split(sep)[0] === dist) schedule();
	});
}

class StylesheetCompiler {
	written = 0;
	readonly #imported = new Set<string>();

	/** Points a module's stylesheet imports at the CSS that compile writes. */
	rewrite(file: string, contents: string) {
		return contents.replace(
			stylesheetImport,
			(_match, keyword: string, quote: string, path: string) => {
				this.#imported.add(join(dirname(file), `${path}.scss`));
				return `${keyword}${quote}${path}.css${quote}`;
			}
		);
	}

	/**
	 * Compiles the imported stylesheets, and every stylesheet compiled before: svelte-package
	 * --watch copies a changed stylesheet without writing its importers again. Unchanged CSS isn't
	 * rewritten, so the watcher doesn't finish the output in a loop.
	 */
	async compile(files: readonly string[]) {
		const present = new Set(files);
		const sources = new Set(this.#imported);
		for (const file of files) {
			const source = file.replace(/\.css$/, ".scss");
			if (source !== file && present.has(source)) sources.add(source);
		}
		if (sources.size === 0) return;
		const compiler = await initAsyncCompiler();
		try {
			for (const source of sources) {
				const css = `${(await compiler.compileAsync(source, { importers: [packageImporter] })).css}\n`;
				const target = source.replace(/\.scss$/, ".css");
				if ((await readFile(target, "utf8").catch(() => undefined)) === css) continue;
				await writeFile(target, css);
				this.written++;
			}
		} finally {
			await compiler.dispose();
		}
	}
}

/** Resolves package paths such as `@riducms/ui/styles.scss` as Node does, from the importing file. */
const packageImporter: FileImporter<"async"> = {
	findFileUrl(url, { containingUrl }) {
		if (containingUrl === null || /^(\.|\/|[a-z]+:)/.test(url)) return null;
		return pathToFileURL(createRequire(containingUrl).resolve(url));
	},
};

class IconWriter {
	written = 0;
	readonly #generated = new Map<string, Promise<string>>();
	readonly #plugin = rawIconPlugin();

	constructor(
		readonly output: string,
		readonly require: NodeRequire
	) {}

	async rewrite(file: string, contents: string) {
		const matches = [...contents.matchAll(iconSpecifier)];
		for (const [specifier, quote, collection, name] of matches) {
			const target = await this.#component(collection!, name!);
			let path = relative(dirname(file), target).split(sep).join("/");
			if (!path.startsWith(".")) path = `./${path}`;
			contents = contents.replace(specifier, `${quote}${path}${quote}`);
		}
		return contents;
	}

	#component(collection: string, name: string) {
		const key = `${collection}/${name}`;
		let pending = this.#generated.get(key);
		if (pending === undefined) {
			pending = this.#write(collection, name);
			this.#generated.set(key, pending);
		}
		return pending;
	}

	async #write(collection: string, name: string) {
		const id = `~icons/${collection}/${name}`;
		const resolved = await call(this.#plugin.resolveId, id);
		const loaded = await call(this.#plugin.load, typeof resolved === "string" ? resolved : id);
		const code = typeof loaded === "string" ? loaded : loaded?.code;
		if (typeof code !== "string") throw new Error(`unplugin-icons could not load ${id}`);
		const target = join(this.output, iconDirectory, collection, `${name}.svelte`);
		await mkdir(dirname(target), { recursive: true });
		await writeFile(target, `${attribution(this.require, collection)}${code}\n`);
		this.written++;
		return target;
	}
}

type Hook = ((...args: unknown[]) => unknown) | { handler: (...args: unknown[]) => unknown };

function rawIconPlugin() {
	const raw = Icons.raw({ compiler: "svelte" }, { framework: "vite" });
	const plugin = (Array.isArray(raw) ? raw[0] : raw) as { resolveId: Hook; load: Hook };
	return plugin;
}

async function call(hook: Hook, ...args: unknown[]) {
	const handler = typeof hook === "function" ? hook : hook.handler;
	return (await handler.call({}, ...args)) as string | { code?: string } | null | undefined;
}

const collectionInfo = new Map<string, string>();

/** An HTML comment crediting the icon set, since its artwork now ships inside the package. */
function attribution(require: NodeRequire, collection: string) {
	let comment = collectionInfo.get(collection);
	if (comment !== undefined) return comment;
	let info: IconSetInfo | undefined;
	for (const path of [
		`@iconify/json/json/${collection}.json`,
		`@iconify-json/${collection}/info.json`,
	]) {
		try {
			const loaded = require(path) as { info?: IconSetInfo } & IconSetInfo;
			info = loaded.info ?? loaded;
			break;
		} catch {
			// Try the next published layout.
		}
	}
	comment =
		info === undefined
			? ""
			: `<!-- ${info.name}${info.author?.name ? ` by ${info.author.name}` : ""}, ${info.license?.title ?? info.license?.spdx ?? "see its licence"}${info.license?.url ? ` (${info.license.url})` : ""} -->\n`;
	collectionInfo.set(collection, comment);
	return comment;
}

interface IconSetInfo {
	name: string;
	author?: { name?: string };
	license?: { title?: string; spdx?: string; url?: string };
}

async function loadLexicalTransform(require: NodeRequire) {
	const module = (await import(
		pathToFileURL(require.resolve("@hvniel/lexical-svelte/preprocess")).href
	)) as { lexicalImports: () => { transform?: Hook } };
	const transform = module.lexicalImports().transform;
	if (transform === undefined) throw new Error("lexicalImports() has no transform hook");
	return async (code: string, file: string) => {
		const result = await call(transform, code, file);
		if (result === null || result === undefined) return code;
		return typeof result === "string" ? result : (result.code ?? code);
	};
}

async function outputFiles(directory: string): Promise<string[]> {
	let entries;
	try {
		entries = await readdir(directory, { withFileTypes: true });
	} catch (error) {
		if ((error as NodeJS.ErrnoException).code === "ENOENT") return [];
		throw error;
	}
	const files = await Promise.all(
		entries.map((entry) => {
			const path = join(directory, entry.name);
			return entry.isDirectory() ? outputFiles(path) : Promise.resolve([path]);
		})
	);
	return files.flat().sort();
}

const flags = new Set(process.argv.slice(2));
const unknown = [...flags].filter((flag) => flag !== "--lexical" && flag !== "--watch");
if (unknown.length > 0) {
	console.error(`finish-svelte-package: unknown option ${unknown.join(", ")}`);
	process.exit(2);
}

const options = { root: process.cwd(), lexical: flags.has("--lexical") };
if (flags.has("--watch")) {
	watchSveltePackage(options);
} else {
	const { icons, lexicalModules, stylesheets } = await finishSveltePackage(options);
	console.log(
		`Finished the package: ${icons} icon component${icons === 1 ? "" : "s"}, ` +
			`${stylesheets} stylesheet${stylesheets === 1 ? "" : "s"}` +
			(options.lexical
				? `, ${lexicalModules} Lexical rune module${lexicalModules === 1 ? "" : "s"}`
				: "") +
			"."
	);
}
