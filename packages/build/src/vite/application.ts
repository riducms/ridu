import { svelte, vitePreprocess } from "@sveltejs/vite-plugin-svelte";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname } from "node:path";
import type { PreprocessorGroup } from "svelte/compiler";
import UnoCSS from "unocss/vite";
import Icons from "unplugin-icons/vite";
import { defineConfig, type Plugin, type PluginOption } from "vite";

import { createAdminUnoConfig } from "../uno/index.ts";
import { contentCSSHash } from "./compiler-options.ts";
import { riduSchemaReloadPlugin } from "./schema-reload.ts";
import { packageSourceAliasPlugin } from "./source-alias.ts";

type SvelteOptions = NonNullable<Parameters<typeof svelte>[0]>;

export interface AdminApplicationConfigOptions {
	outDir: string;
	schemaReloadSignal: string;
	base?: string;
	cacheDir?: string;
	dedupe?: readonly string[];
	dependencyInventory?: string;
	dependencyInventoryIconSourcePackage?: string;
	optimizeDepsExclude?: readonly string[];
	pluginsBeforeSvelte?: readonly PluginOption[];
	proxyTarget?: string;
	root?: string;
	svelte?: SvelteOptions;
}

export function createAdminApplicationConfig(options: AdminApplicationConfigOptions) {
	const svelteOptions = options.svelte ?? {};
	const base = options.base ?? "/admin/";
	return defineConfig(({ command }) => ({
		...(options.root === undefined ? {} : { root: options.root }),
		base,
		clearScreen: false,
		...(options.cacheDir === undefined ? {} : { cacheDir: options.cacheDir }),
		plugins: [
			canonicalBaseRedirectPlugin(base),
			riduSchemaReloadPlugin(options.schemaReloadSignal),
			packageSourceAliasPlugin(),
			...(options.pluginsBeforeSvelte ?? []),
			UnoCSS(createAdminUnoConfig()),
			Icons({ compiler: "svelte" }),
			svelte({
				...svelteOptions,
				configFile: false,
				preprocess: [...preprocessors(svelteOptions.preprocess), vitePreprocess()],
				compilerOptions: {
					runes: true,
					...(command === "build" ? { cssHash: contentCSSHash } : {}),
					...svelteOptions.compilerOptions,
				},
			}),
			...(options.dependencyInventory === undefined
				? []
				: [
						dependencyInventoryPlugin(
							options.dependencyInventory,
							options.dependencyInventoryIconSourcePackage
						),
					]),
		],
		resolve: {
			dedupe: unique(["bits-ui", "svelte", ...(options.dedupe ?? [])]),
		},
		...(options.optimizeDepsExclude === undefined
			? {}
			: { optimizeDeps: { exclude: [...options.optimizeDepsExclude] } }),
		build: {
			outDir: options.outDir,
			emptyOutDir: true,
		},
		...(options.proxyTarget === undefined
			? {}
			: { server: { proxy: { "/api": options.proxyTarget } } }),
	}));
}

interface BundledDependency {
	kind: "package" | "generated-icon-collection";
	name: string;
	version: string;
	license?: string;
	noticeSection?: string;
	root: string;
	sourcePackage?: string;
	sourceURL?: string;
}

function dependencyInventoryPlugin(
	outputPath: string,
	iconSourcePackage: string | undefined
): Plugin {
	return {
		name: "ridu-dependency-inventory",
		generateBundle(_outputOptions, bundle) {
			const dependencies = new Map<string, BundledDependency>();
			const iconCollections = new Set<string>();
			for (const output of Object.values(bundle)) {
				if (output.type !== "chunk") continue;
				for (const moduleID of Object.keys(output.modules)) {
					const packageRoot = packageRootFromModuleID(moduleID);
					if (packageRoot !== undefined) recordDependency(packageRoot, dependencies);
					const iconCollection = iconCollectionFromModuleID(moduleID);
					if (iconCollection !== undefined) iconCollections.add(iconCollection);
				}
			}

			if (iconCollections.size > 0 && iconSourcePackage === undefined) {
				throw new Error("rendered icon collections require a dependency inventory source package");
			}
			if (iconSourcePackage !== undefined) {
				if (iconCollections.size === 0) {
					throw new Error(
						"configured icon dependency inventory found no rendered icon collections"
					);
				}
				const require = createRequire(`${process.cwd()}/package.json`);
				const manifestPath = require.resolve(`${iconSourcePackage}/package.json`);
				const packageRoot = dirname(manifestPath);
				for (const collection of iconCollections) {
					recordIconCollection(packageRoot, collection, dependencies);
				}
			}

			mkdirSync(dirname(outputPath), { recursive: true });
			writeFileSync(
				outputPath,
				`${JSON.stringify(
					[...dependencies.values()].sort((left, right) =>
						`${left.name}@${left.version}`.localeCompare(`${right.name}@${right.version}`)
					),
					null,
					2
				)}\n`
			);
		},
	};
}

function iconCollectionFromModuleID(moduleID: string) {
	const match = moduleID.replaceAll("\\", "/").match(/(?:~icons|virtual:icons)\/([^/?]+)/);
	return match?.[1];
}

function packageRootFromModuleID(moduleID: string) {
	const withQuery = moduleID.replaceAll("\\", "/");
	const queryIndex = withQuery.indexOf("?");
	const normalized = queryIndex < 0 ? withQuery : withQuery.slice(0, queryIndex);
	const marker = "/node_modules/";
	const markerIndex = normalized.lastIndexOf(marker);
	if (markerIndex < 0) return undefined;
	const packageSegments = normalized.slice(markerIndex + marker.length).split("/");
	const segmentCount = packageSegments[0]?.startsWith("@") ? 2 : 1;
	if (packageSegments.length < segmentCount) return undefined;
	return `${normalized.slice(0, markerIndex + marker.length)}${packageSegments
		.slice(0, segmentCount)
		.join("/")}`;
}

function recordDependency(packageRoot: string, dependencies: Map<string, BundledDependency>) {
	const manifest = JSON.parse(readFileSync(`${packageRoot}/package.json`, "utf8")) as {
		name?: string;
		version?: string;
		license?: string;
	};
	if (manifest.name === undefined || manifest.version === undefined) {
		throw new Error(`bundled dependency at ${packageRoot} has no package identity`);
	}
	const dependency = {
		kind: "package" as const,
		name: manifest.name,
		version: manifest.version,
		...(typeof manifest.license === "string" ? { license: manifest.license } : {}),
		root: packageRoot,
	};
	dependencies.set(`${dependency.name}@${dependency.version}`, dependency);
}

function recordIconCollection(
	packageRoot: string,
	collection: string,
	dependencies: Map<string, BundledDependency>
) {
	const manifest = JSON.parse(readFileSync(`${packageRoot}/package.json`, "utf8")) as {
		name?: string;
		version?: string;
	};
	if (manifest.name === undefined || manifest.version === undefined) {
		throw new Error(`icon source package at ${packageRoot} has no package identity`);
	}
	const collectionMetadata = JSON.parse(
		readFileSync(`${packageRoot}/json/${collection}.json`, "utf8")
	) as {
		info?: {
			name?: string;
			author?: { url?: string };
			license?: { spdx?: string; url?: string };
		};
	};
	const name = collectionMetadata.info?.name;
	const license = collectionMetadata.info?.license?.spdx;
	if (name === undefined || license === undefined) {
		throw new Error(`${manifest.name} icon collection ${collection} lacks name or SPDX metadata`);
	}
	const dependency = {
		kind: "generated-icon-collection" as const,
		name: `${name} icons`,
		version: manifest.version,
		license: license === "ISC" ? "ISC AND MIT" : license,
		noticeSection: `${name} icons`,
		root: packageRoot,
		sourcePackage: manifest.name,
		...(collectionMetadata.info?.author?.url === undefined
			? {}
			: { sourceURL: collectionMetadata.info.author.url }),
	};
	dependencies.set(`${dependency.name}@${dependency.version}`, dependency);
}

function canonicalBaseRedirectPlugin(base: string): Plugin {
	return {
		name: "ridu-canonical-admin-base",
		configureServer(server) {
			if (!base.startsWith("/") || base === "/" || !base.endsWith("/")) return;
			const baseWithoutSlash = base.slice(0, -1);
			server.middlewares.use((request, response, next) => {
				const requestURL = request.url ?? "";
				const queryIndex = requestURL.indexOf("?");
				const pathname = queryIndex < 0 ? requestURL : requestURL.slice(0, queryIndex);
				if (pathname !== baseWithoutSlash) {
					next();
					return;
				}
				const query = queryIndex < 0 ? "" : requestURL.slice(queryIndex);
				response.statusCode = 308;
				response.setHeader("Location", `${base}${query}`);
				response.end();
			});
		},
	};
}

function preprocessors(preprocess: PreprocessorGroup | PreprocessorGroup[] | undefined) {
	if (!preprocess) return [];
	return Array.isArray(preprocess) ? preprocess : [preprocess];
}

function unique(values: readonly string[]) {
	return [...new Set(values)];
}
