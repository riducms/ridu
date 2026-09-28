import { svelte, vitePreprocess } from "@sveltejs/vite-plugin-svelte";
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { createHash } from "node:crypto";
import { createRequire } from "node:module";
import { dirname, resolve } from "node:path";
import { Worker } from "node:worker_threads";
import type { PreprocessorGroup } from "svelte/compiler";
import UnoCSS from "unocss/vite";
import Icons from "unplugin-icons/vite";
import {
	build as viteBuild,
	defineConfig,
	type Plugin,
	type PluginOption,
	type ResolvedConfig,
} from "vite";

import { createAdminUnoConfig } from "../uno/index.js";
import { ADMIN_PREPARED_ROUTE_STATE_VERSION } from "./admin-route-state-version.generated.js";
import { contentCSSHash } from "./compiler-options.js";
import { riduSchemaReloadPlugin } from "./schema-reload.js";
import { packageSourceAliasPlugin } from "./source-alias.js";
import { sharedDependencies } from "./shared-dependencies.js";
import { packageSourceScanPlugin } from "./source-scan.js";
import { riduAdminCheckPlugins } from "../admin-check.js";

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
	const sourcePackages = [
		"@riducms/admin",
		"@riducms/ui",
		"@riducms/plugin-richtext",
		"@riducms/plugin-seo",
		"@riducms/plugin-graphql",
	];
	return defineConfig(({ command }) => ({
		...(options.root === undefined ? {} : { root: options.root }),
		base,
		clearScreen: false,
		...(options.cacheDir === undefined ? {} : { cacheDir: options.cacheDir }),
		plugins: [
			...riduAdminBuildOutputPlugins(),
			canonicalBaseRedirectPlugin(base),
			riduSchemaReloadPlugin(options.schemaReloadSignal),
			packageSourceAliasPlugin(),
			packageSourceScanPlugin(sourcePackages),
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
			dedupe: unique([...sharedDependencies, ...(options.dedupe ?? [])]),
		},
		optimizeDeps: {
			// Preserve package-relative aliases and Svelte preprocessing in framework source.
			exclude: unique([...sourcePackages, ...(options.optimizeDepsExclude ?? [])]),
			include: [
				// Plugin source can be excluded from scanning while its editor is prebundled.
				// Optimize the plugin-owned core too so custom nodes share the editor's classes.
				"@riducms/plugin-richtext > lexical",
				// Lexical loads its React devtools dynamically, beyond the initial dependency scan.
				"react",
				"react-dom",
				"react/jsx-runtime",
				"react/jsx-dev-runtime",
				"react-dom/client",
			],
		},
		build: {
			outDir: options.outDir,
			emptyOutDir: true,
		},
		...(options.proxyTarget === undefined
			? {}
			: { server: { proxy: { "/api": options.proxyTarget } } }),
	}));
}

function riduAdminBuildOutputPlugins(): Plugin[] {
	const checks = riduAdminCheckPlugins();
	// `ridu check` deliberately replaces the production output with an in-memory
	// registry build, so it must not also try to emit bootstrap metadata.
	return checks.length === 0 ? [riduAdminBootstrapMetadataPlugin()] : checks;
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
	let writesArtifacts = true;
	return {
		name: "ridu-dependency-inventory",
		configResolved(config) {
			writesArtifacts = config.build.write;
		},
		generateBundle(_outputOptions, bundle) {
			if (!writesArtifacts) return;
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

function riduAdminBootstrapMetadataPlugin(): Plugin {
	let config: ResolvedConfig | undefined;
	let coreMetadata: AdminCoreViewMetadata = emptyAdminCoreViewMetadata();

	return {
		name: "ridu-admin-bootstrap-metadata",
		// Embedded-bundle metadata is unused by Vite's development module graph.
		apply: "build",
		configResolved(resolved) {
			config = resolved;
		},
		async buildStart() {
			if (config === undefined) throw new Error("admin bootstrap metadata has no resolved config");
			coreMetadata = await resolveAdminCoreViewMetadata(config, (source, importer) =>
				this.resolve(source, importer, { skipSelf: true })
			);
		},
		generateBundle: {
			// Vite removes shared CSS-only JS chunks and rewrites their imports first.
			// Snapshot the final graph so preload metadata never names those removed files.
			order: "post",
			handler(_options, bundle) {
				const chunks = Object.values(bundle).filter(
					(output): output is Extract<(typeof bundle)[string], { type: "chunk" }> =>
						output.type === "chunk"
				);
				const byFile = new Map(chunks.map((chunk) => [chunk.fileName, chunk]));
				const groupRoots = new Map<string, string[]>();

				const add = (group: string, fileName: string) => {
					groupRoots.set(group, [...(groupRoots.get(group) ?? []), fileName]);
				};

				// Stable source-module identities locate each preload boundary; emitted file
				// names are content-hashed and therefore cannot serve as logical group names.
				for (const chunk of chunks) {
					if (chunk.isEntry) add("entry", chunk.fileName);
					const modules = Object.keys(chunk.modules).map((id) => id.replaceAll("\\", "/"));
					if (modules.some((id) => id.endsWith("/features/documents/document-route.svelte")))
						add("document", chunk.fileName);
					if (modules.some((id) => id.endsWith("/versions/version-history-route.svelte")))
						add("versions", chunk.fileName);
					if (modules.some((id) => id.endsWith("/uploads/bulk-upload-route.svelte")))
						add("bulk-upload", chunk.fileName);
					if (modules.some((id) => id.endsWith("/date-value-control/date-value-control.svelte")))
						add("date", chunk.fileName);
					if (modules.some((id) => id.endsWith("/uploads/upload-control.svelte")))
						add("upload-preview", chunk.fileName);
					if (modules.some((id) => id.endsWith("/documents/document-api-view.svelte")))
						add("document-api", chunk.fileName);
				}

				// Go serves the embedded bundle without Vite's module graph. Give the runtime
				// each root's complete static-import closure, including CSS attached anywhere
				// in that closure, while leaving interaction-only dynamic chunks lazy.
				const collect = (roots: readonly string[]) => {
					const files = new Set<string>();
					const visit = (fileName: string) => {
						if (files.has(fileName)) return;
						files.add(fileName);
						const chunk = byFile.get(fileName);
						if (chunk === undefined) return;
						for (const dependency of chunk.imports) visit(dependency);
						const metadata = (
							chunk as typeof chunk & {
								viteMetadata?: { importedCss?: Set<string> };
							}
						).viteMetadata;
						for (const css of metadata?.importedCss ?? []) files.add(css);
					};
					for (const root of roots) visit(root);
					return [...files].sort();
				};

				// Sorting groups, roots, and their closure makes the metadata independent of
				// bundler traversal order and gives the build ID a canonical input.
				const moduleGroups = Object.fromEntries(
					[...groupRoots.entries()]
						.sort(([left], [right]) => left.localeCompare(right))
						.map(([group, roots]) => [group, collect([...new Set(roots)].sort())])
				);
				const {
					dashboardLoaders,
					routeLoaders,
					viewLoaders,
					documentViewRoutes,
					extensionRoutes,
					listCellFields,
					seedableCoreSurfaces,
					replacedCoreViews,
				} = coreMetadata;

				// The ID changes only when runtime-visible bootstrap metadata changes, including
				// route-relevant content hashes. Clocks and absolute output directories stay out.
				const identity = JSON.stringify({
					dashboardLoaders,
					routeLoaders,
					viewLoaders,
					protocolVersion: ADMIN_PREPARED_ROUTE_STATE_VERSION,
					documentViewRoutes,
					extensionRoutes,
					listCellFields,
					moduleGroups,
					replacedCoreViews,
					seedableCoreSurfaces,
				});
				const buildId = createHash("sha256").update(identity).digest("hex").slice(0, 24);
				this.emitFile({
					type: "asset",
					fileName: "ridu-admin-bootstrap.json",
					source: `${JSON.stringify(
						{
							dashboardLoaders,
							routeLoaders,
							viewLoaders,
							protocolVersion: ADMIN_PREPARED_ROUTE_STATE_VERSION,
							buildId,
							documentViewRoutes,
							extensionRoutes,
							listCellFields,
							moduleGroups,
							replacedCoreViews,
							seedableCoreSurfaces,
						},
						null,
						2
					)}\n`,
				});
			},
		},
	};
}

interface AdminCoreViewMetadata {
	dashboardLoaders: string[];
	routeLoaders: Record<string, string>;
	viewLoaders: Record<string, string>;
	documentViewRoutes: string[];
	extensionRoutes: string[];
	listCellFields: Record<string, string[]>;
	replacedCoreViews: string[];
	seedableCoreSurfaces: string[];
}

function emptyAdminCoreViewMetadata(): AdminCoreViewMetadata {
	return {
		dashboardLoaders: [],
		routeLoaders: {},
		viewLoaders: {},
		documentViewRoutes: [],
		extensionRoutes: [],
		listCellFields: {},
		replacedCoreViews: [],
		seedableCoreSurfaces: [
			"account",
			"collectionCreate",
			"collectionEdit",
			"collectionList",
			"dashboard",
			"global",
			"login",
			"notFound",
			"setup",
		],
	};
}

async function resolveAdminCoreViewMetadata(
	config: ResolvedConfig,
	resolveApplicationImport: (
		source: string,
		importer: string | undefined
	) => Promise<{ id: string } | null>
): Promise<AdminCoreViewMetadata> {
	const configPath = [
		resolve(config.root, "src/admin.config.ts"),
		resolve(config.root, "admin.config.ts"),
	].find(existsSync);
	if (configPath === undefined) return emptyAdminCoreViewMetadata();

	const entry = "\0ridu-admin-bootstrap-config";
	const componentPrefix = "\0ridu-admin-bootstrap-component:";
	// Admin config is executable TypeScript, so use the application's own Vite
	// resolution and defines instead of attempting to parse source. Components are
	// replaced with inert functions because only their registrations affect metadata.
	const result = await viteBuild({
		configFile: false,
		root: config.root,
		mode: config.mode,
		envDir: config.envDir,
		...(config.envPrefix === undefined ? {} : { envPrefix: config.envPrefix }),
		...(config.define === undefined ? {} : { define: config.define }),
		logLevel: "silent",
		plugins: [
			{
				name: "ridu-admin-bootstrap-config-evaluator",
				enforce: "pre",
				async resolveId(source, importer) {
					if (source === entry) return entry;
					if (source.startsWith("\0")) return;
					const resolved = await resolveApplicationImport(source, importer);
					if (resolved?.id.split("?", 1)[0]?.endsWith(".svelte")) {
						return componentPrefix + resolved.id + ".js";
					}
					return resolved?.id;
				},
				load(id) {
					if (id === entry) return adminBootstrapConfigEvaluationSource(configPath);
					if (id.startsWith(componentPrefix))
						return "export default function RiduBootstrapComponent() {}";
				},
				transform: {
					order: "pre",
					filter: { id: /\.svelte(?:\?|$)/ },
					handler() {
						return { code: "export default function RiduBootstrapComponent() {}", map: null };
					},
				},
			},
		],
		build: {
			write: false,
			minify: false,
			rolldownOptions: {
				input: entry,
				preserveEntrySignatures: "strict",
				output: { format: "es", codeSplitting: false },
			},
		},
	});

	// The nested build is intentionally in-memory: its sole output is the resolved
	// registration graph consumed below, not another application bundle.
	const output = Array.isArray(result) ? result[0] : result;
	if (output === undefined || !("output" in output)) {
		throw new Error("admin bootstrap config evaluation unexpectedly entered watch mode");
	}
	const chunk = output?.output.find((candidate) => candidate.type === "chunk" && candidate.isEntry);
	if (chunk === undefined || chunk.type !== "chunk")
		throw new Error("admin bootstrap config evaluation produced no entry");
	const evaluation = await evaluateAdminBootstrapChunk(chunk.code);
	if (typeof evaluation !== "string")
		throw new Error("admin bootstrap config evaluation produced no metadata");
	return JSON.parse(evaluation) as AdminCoreViewMetadata;
}

function adminBootstrapConfigEvaluationSource(configPath: string) {
	// Reuse the runtime resolver here so plugin/application ordering and collision
	// failures cannot drift between build metadata and the mounted admin.
	return `
import config from ${JSON.stringify(configPath)};
import { resolveAdminConfig } from '@riducms/plugin/admin';

const resolved = resolveAdminConfig(config);
const seedable = new Set([
  'account',
  'collectionCreate',
  'collectionEdit',
  'collectionList',
  'dashboard',
  'global',
  'login',
  'notFound',
  'setup',
]);
const replaced = new Set();
const viewLoaders = new Map();

const dashboardReplacement = resolved.extensions.dashboardPanels.find(
  (panel) => panel.position === 'replace',
);
const dashboardPanels = dashboardReplacement
  ? [dashboardReplacement]
  : resolved.extensions.dashboardPanels;

if (dashboardReplacement && !dashboardReplacement.loader) {
  seedable.delete('dashboard');
  replaced.add('dashboard:*');
}

for (const component of resolved.extensions.login) {
  if (component.position === 'replace') {
    seedable.delete('login');
    replaced.add('login:*');
  }
}

for (const component of resolved.extensions.account) {
  if (component.position === 'replace') {
    seedable.delete('account');
    replaced.add('account:*');
  }
}

for (const view of resolved.extensions.coreViews) {
  const target = view.surface === 'notFound'
    ? '*'
    : 'collection' in view
      ? (view.collection ?? '*')
      : 'global' in view
        ? (view.global ?? '*')
        : '*';

  replaced.add(view.surface + ':' + target);
  if (view.loader) viewLoaders.set(view.surface + ':' + target, view.loader.key);
}

export default JSON.stringify({
  dashboardLoaders: [
    ...new Set(
      dashboardPanels.flatMap((panel) => panel.loader ? [panel.loader.key] : []),
    ),
  ].sort(),
  routeLoaders: Object.fromEntries(
    resolved.extensions.routes
      .filter((route) => route.loader)
      .map((route) => [route.path, route.loader.key])
      .sort(([left], [right]) => left < right ? -1 : left > right ? 1 : 0),
  ),
  viewLoaders: Object.fromEntries(
    [...viewLoaders].sort(
      ([left], [right]) => left < right ? -1 : left > right ? 1 : 0,
    ),
  ),
  documentViewRoutes: [
    ...new Set(
      resolved.extensions.documentViews.map(
        (view) => (view.collection ?? '*') + ':' + view.key,
      ),
    ),
  ].sort(),
  extensionRoutes: [...new Set(resolved.extensions.routes.map((route) => route.path))].sort(),
  listCellFields: Object.fromEntries(
    [...new Set(resolved.extensions.listCellRenderers.map((cell) => cell.collection))]
      .sort()
      .map((collection) => [
        collection,
        [
          ...new Set(
            resolved.extensions.listCellRenderers
              .filter((cell) => cell.collection === collection)
              .map((cell) => cell.field),
          ),
        ].sort(),
      ]),
  ),
  replacedCoreViews: [...replaced].sort(),
  seedableCoreSurfaces: [...seedable].sort(),
});
`;
}

function evaluateAdminBootstrapChunk(code: string) {
	const moduleURL = `data:text/javascript;base64,${Buffer.from(code).toString("base64")}`;
	// Evaluate application-owned config away from the long-lived Vite module graph;
	// the worker exits after returning the JSON-only metadata value.
	const workerSource = `
import { parentPort } from "node:worker_threads";
const evaluated = await import(${JSON.stringify(moduleURL)});
parentPort.postMessage(evaluated.default);
`;
	const workerURL = new URL(
		`data:text/javascript;base64,${Buffer.from(workerSource).toString("base64")}`
	);
	return new Promise<unknown>((complete, reject) => {
		const worker = new Worker(workerURL);
		worker.once("message", complete);
		worker.once("error", reject);
		worker.once("exit", (code) => {
			if (code !== 0)
				reject(new Error(`admin bootstrap config evaluation exited with status ${code}`));
		});
	});
}
