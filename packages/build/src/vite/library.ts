import { svelte, vitePreprocess } from "@sveltejs/vite-plugin-svelte";
import { resolve } from "node:path";
import type { PreprocessorGroup } from "svelte/compiler";
import UnoCSS from "unocss/vite";
import { defineConfig, type Plugin, type PluginOption } from "vite";

import { createAdminUnoConfig } from "../uno/index.js";
import { contentCSSHash } from "./compiler-options.js";
import { packageSourceAliasPlugin } from "./source-alias.js";

type SvelteOptions = NonNullable<Parameters<typeof svelte>[0]>;

export interface AdminLibraryConfigOptions {
	dedupe?: readonly string[];
	entry?: string;
	pluginsBeforeSvelte?: readonly PluginOption[];
	svelte?: SvelteOptions;
	uno?: boolean;
}

export function createAdminLibraryConfig(options: AdminLibraryConfigOptions = {}) {
	const svelteOptions = options.svelte ?? {};
	const entry = options.entry ?? "src/index.ts";
	return defineConfig({
		plugins: [
			packageSourceAliasPlugin(),
			...(options.pluginsBeforeSvelte ?? []),
			...(options.uno === false
				? []
				: [libraryUnoEntryPlugin(entry), UnoCSS(createAdminUnoConfig())]),
			svelte({
				...svelteOptions,
				configFile: false,
				preprocess: [...preprocessors(svelteOptions.preprocess), vitePreprocess()],
				compilerOptions: {
					runes: true,
					cssHash: contentCSSHash,
					...svelteOptions.compilerOptions,
				},
			}),
		],
		resolve: {
			dedupe: unique(["bits-ui", "svelte", ...(options.dedupe ?? [])]),
		},
		build: {
			lib: { entry, formats: ["es"], fileName: "index" },
			rollupOptions: {
				external: (id) =>
					id !== "uno.css" &&
					id !== "virtual:uno.css" &&
					!id.startsWith("@/") &&
					!id.startsWith("@admin/") &&
					!id.startsWith("@ui/") &&
					!id.startsWith("@plugin-richtext/") &&
					!id.startsWith("@plugin-seo/") &&
					!id.startsWith(".") &&
					!id.startsWith("/"),
			},
		},
	});
}

function preprocessors(preprocess: PreprocessorGroup | PreprocessorGroup[] | undefined) {
	if (!preprocess) return [];
	return Array.isArray(preprocess) ? preprocess : [preprocess];
}

function libraryUnoEntryPlugin(entry: string): Plugin {
	let entryPath = "";
	return {
		name: "ridu-library-uno-entry",
		configResolved(config) {
			entryPath = resolve(config.root, entry);
		},
		transform(code, id) {
			if (id !== entryPath) return;
			return `import "uno.css";\n${code}`;
		},
	};
}

function unique(values: readonly string[]) {
	return [...new Set(values)];
}
