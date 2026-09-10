import { existsSync, readFileSync, readdirSync, writeFileSync } from 'node:fs';
import path from 'node:path';
import { gzipSync } from 'node:zlib';
import {
	referenceModuleRegistry,
	type ReferenceModuleRegistration,
	type TypeScriptReferenceSource
} from '../authoring/modules';
import { referenceEditorialOverlays, type ReferenceEditorialOverlay } from '../authoring/overlays';
import type { ReferenceKind, ReferenceModule, ReferenceParameter, ReferenceSymbol } from '../types';
import { extractTypeScriptEntrypoint } from './extract-typescript';
import type { ExtractedDeclaration, GoExtractionCatalog } from './types';

export interface ReferenceCatalogFile {
	schemaVersion: 1;
	modules: ReferenceModule[];
}

interface LockedRoute {
	module: string;
	symbol: string;
	redirect?: string;
}

interface RouteLockFile {
	schemaVersion: 1;
	routes: Record<string, LockedRoute>;
}

export interface GenerateReferenceOptions {
	repositoryRoot: string;
	write: boolean;
	seedRoutes?: boolean;
}

// Includes the live-validation methods and their authored callback contracts.
export const REFERENCE_CATALOG_MAX_BYTES = 4.25 * 1024 * 1024;
export const REFERENCE_CATALOG_MAX_GZIP_BYTES = 440 * 1024;

export function generateReferenceCatalog(options: GenerateReferenceOptions): ReferenceCatalogFile {
	const go = extractGo(options.repositoryRoot);
	const extractedByPackage = new Map(
		go.packages.map((loaded) => [
			loaded.importPath,
			loaded.declarations.map(normalizeGoDeclaration)
		])
	);
	const routeLockPath = path.join(
		options.repositoryRoot,
		'website/src/reference/authoring/route-lock.json'
	);
	const routeLock = readRouteLock(routeLockPath);
	const generatedIDs = new Set<string>();
	const modules = referenceModuleRegistry.map((registration) => {
		const declarations = extractModuleDeclarations(
			options.repositoryRoot,
			registration,
			extractedByPackage
		);
		const module = buildModule(registration, declarations, routeLock);
		for (const symbol of module.symbols) {
			if (generatedIDs.has(symbol.id))
				throw new Error(`duplicate generated declaration ID ${symbol.id}`);
			generatedIDs.add(symbol.id);
		}
		return module;
	});
	synchronizeGoFacadeAliases(modules);
	resolveEditorialLinks(modules);
	validateOverlays(generatedIDs);

	validateRoutes(modules, routeLock, generatedIDs, Boolean(options.seedRoutes));
	validateCLI(options.repositoryRoot, modules);
	validatePackageExportMaps(options.repositoryRoot);
	validatePublicGoPackages(options.repositoryRoot);
	const catalog: ReferenceCatalogFile = { schemaVersion: 1, modules };
	assertPortable(catalog);
	const serializedCatalog = stableJSON(catalog);
	validateCatalogSize(serializedCatalog);

	if (options.write) {
		writeFileSync(routeLockPath, stableJSON(routeLock));
		writeFileSync(
			path.join(options.repositoryRoot, 'website/src/reference/generated/catalog.json'),
			serializedCatalog
		);
	}
	return catalog;
}

export function stableJSON(value: unknown): string {
	return `${JSON.stringify(value, null, '\t')}\n`;
}

function extractGo(repositoryRoot: string): GoExtractionCatalog {
	const patterns = referenceModuleRegistry.flatMap((registration) =>
		registration.sources.flatMap((source) => (source.kind === 'go' ? [source.pattern] : []))
	);
	const process = Bun.spawnSync(
		[
			'go',
			'run',
			'./website/tools/referencegen/goextract',
			'--root',
			repositoryRoot,
			'--',
			...patterns
		],
		{ cwd: repositoryRoot, stdout: 'pipe', stderr: 'pipe' }
	);
	if (process.exitCode !== 0) {
		throw new Error(`Go reference extraction failed:\n${process.stderr.toString()}`);
	}
	return JSON.parse(process.stdout.toString()) as GoExtractionCatalog;
}

function normalizeGoDeclaration(
	declaration: GoExtractionCatalog['packages'][number]['declarations'][number]
): ExtractedDeclaration {
	const kind: ReferenceKind =
		declaration.kind === 'const'
			? 'constant'
			: declaration.kind === 'var'
				? 'variable'
				: declaration.kind;
	return {
		...declaration,
		kind,
		members: declaration.members,
		parameters: declaration.parameters,
		overloads: []
	};
}

function extractModuleDeclarations(
	repositoryRoot: string,
	registration: ReferenceModuleRegistration,
	goDeclarations: ReadonlyMap<string, ExtractedDeclaration[]>
): ExtractedDeclaration[] {
	const declarations: ExtractedDeclaration[] = [];
	for (const source of registration.sources) {
		if (source.kind === 'go') {
			const found = goDeclarations.get(registration.packageName);
			if (!found)
				throw new Error(`Go package was registered but not extracted: ${registration.packageName}`);
			declarations.push(...found);
		}
		if (source.kind === 'typescript') {
			appendTypeScriptDeclarations(repositoryRoot, declarations, source);
		}
		if (source.kind === 'cli') {
			declarations.push(
				...readRecordDeclarations(repositoryRoot, source.records, registration.slug)
			);
		}
	}
	if (registration.records) {
		declarations.push(
			...readRecordDeclarations(repositoryRoot, registration.records, registration.slug)
		);
	}
	return uniqueByID(declarations);
}

function appendTypeScriptDeclarations(
	repositoryRoot: string,
	declarations: ExtractedDeclaration[],
	source: TypeScriptReferenceSource
): void {
	for (const entrypoint of source.entrypoints) {
		const extracted = extractTypeScriptEntrypoint(repositoryRoot, entrypoint);
		if (!entrypoint.aliasesOnly) {
			declarations.push(...extracted);
			continue;
		}
		for (const alias of extracted) {
			const canonical = declarations.find((declaration) => declaration.name === alias.name);
			if (canonical) {
				canonical.aliases = [...new Set([...canonical.aliases, ...alias.aliases])];
			} else {
				declarations.push(alias);
			}
		}
	}
}

function buildModule(
	registration: ReferenceModuleRegistration,
	extracted: ExtractedDeclaration[],
	routeLock: RouteLockFile
): ReferenceModule {
	const byID = new Map(extracted.map((declaration) => [declaration.id, declaration]));
	const orderedDeclarations = registration.symbolOrder.map((id) => {
		const declaration = byID.get(id);
		if (!declaration) throw new Error(`registered reference declaration disappeared: ${id}`);
		return declaration;
	});
	orderedDeclarations.push(
		...extracted
			.filter((declaration) => !registration.symbolOrder.includes(declaration.id))
			.sort((left, right) => left.id.localeCompare(right.id))
	);
	const symbols = uniqueByID(orderedDeclarations).map((declaration) => {
		const overlay = referenceEditorialOverlays[declaration.id];
		const route = routeFor(routeLock, registration.slug, declaration);
		return buildSymbol(registration, declaration, overlay, route.symbol);
	});
	const discoveredGroups = [...new Set(symbols.map((symbol) => symbol.group))];
	const groupOrder = [
		...registration.groupOrder.filter((group) => discoveredGroups.includes(group)),
		...discoveredGroups.filter((group) => !registration.groupOrder.includes(group))
	];
	return {
		id: registration.id,
		name: registration.name,
		slug: registration.slug,
		packageName: registration.packageName,
		language: registration.language,
		group: registration.group,
		summary: registration.summary,
		groupOrder,
		symbols
	};
}

function buildSymbol(
	registration: ReferenceModuleRegistration,
	declaration: ExtractedDeclaration,
	overlay: ReferenceEditorialOverlay | undefined,
	slug: string
): ReferenceSymbol {
	const structuralRows = ['function', 'method'].includes(declaration.kind)
		? declaration.parameters
		: declaration.members;
	// TypeScript/Svelte shape is compiler-owned. Editorial rows may describe extracted members,
	// but must never manufacture members for aliases such as Record, unions, or intersections.
	const fallbackRows = declaration.id.startsWith('ts:') ? [] : (overlay?.parameters ?? []);
	const parameters = mergeParameterDescriptions(
		structuralRows.length > 0 ? structuralRows : fallbackRows,
		overlay?.parameters ?? []
	);
	const source = declaration.source
		? {
				...declaration.source,
				url: `https://github.com/riducms/ridu/blob/main/${declaration.source.path}#L${declaration.source.line}`
			}
		: undefined;
	const sourceSummary = firstParagraph(declaration.summary);
	const returns = declaration.returns
		? {
				type: declaration.returns,
				description: overlay?.returns?.description ?? 'The declared result.'
			}
		: overlay?.returns;
	return {
		id: declaration.id,
		name: declaration.name,
		slug,
		kind: declaration.kind,
		group: overlay?.group ?? groupForKind(declaration.kind),
		summary:
			overlay?.summary ??
			sourceSummary ??
			`Public ${declaration.kind} ${declaration.name} from ${registration.packageName}.`,
		signature: declaration.signature,
		parameters,
		parametersLabel: overlay?.parametersLabel ?? '',
		...(overlay?.options?.length
			? {
					options: overlay.options.map(({ name, type, description }) => ({
						name,
						type,
						description
					})),
					optionsLabel: overlay.optionsLabel ?? 'Options'
				}
			: {}),
		...(returns ? { returns } : {}),
		details: overlay?.details ?? [],
		example: overlay?.example ?? '',
		language: registration.language,
		typeLinks: {},
		aliases: [
			...new Set([...(declaration.aliases ?? []), ...(overlay?.keywords ?? []), declaration.name])
		],
		...(source ? { source } : {}),
		relatedDocs: overlay?.relatedDocs ?? [],
		relatedSymbols: overlay?.relatedSymbols ?? [],
		overloads: declaration.overloads
	};
}

function routeFor(
	routeLock: RouteLockFile,
	moduleSlug: string,
	declaration: ExtractedDeclaration
): LockedRoute {
	const existing = routeLock.routes[declaration.id];
	if (existing) {
		if (existing.module !== moduleSlug) {
			throw new Error(
				`locked reference route ${declaration.id} moved from ${existing.module} to ${moduleSlug}`
			);
		}
		return existing;
	}
	const generatedSlug = `${declarationSlug(declaration.name)}${declaration.kind === 'method' ? '-method' : ''}`;
	const route = { module: moduleSlug, symbol: generatedSlug };
	routeLock.routes[declaration.id] = route;
	return route;
}

function readRecordDeclarations(
	repositoryRoot: string,
	recordPath: string,
	moduleSlug: string
): ExtractedDeclaration[] {
	const file = JSON.parse(readFileSync(path.join(repositoryRoot, recordPath), 'utf8')) as {
		schemaVersion: number;
		records: Array<ExtractedDeclaration & { module: string }>;
	};
	if (file.schemaVersion !== 1)
		throw new Error(`unsupported reference record version in ${recordPath}`);
	return file.records.flatMap(({ module, ...record }) =>
		module === moduleSlug ? [{ ...record, summary: '' }] : []
	);
}

function readRouteLock(lockPath: string): RouteLockFile {
	if (!existsSync(lockPath)) return { schemaVersion: 1, routes: Object.create(null) };
	const parsed = JSON.parse(readFileSync(lockPath, 'utf8')) as RouteLockFile;
	if (parsed.schemaVersion !== 1 || !parsed.routes) throw new Error('invalid reference route lock');
	return parsed;
}

function validateRoutes(
	modules: readonly ReferenceModule[],
	routeLock: RouteLockFile,
	generatedIDs: ReadonlySet<string>,
	seedRoutes: boolean
): void {
	const routes = new Map<string, string>();
	for (const module of modules) {
		for (const symbol of module.symbols) {
			const route = `${module.slug}/${symbol.slug}`;
			const existing = routes.get(route);
			if (existing)
				throw new Error(`reference route collision ${route}: ${existing} and ${symbol.id}`);
			routes.set(route, symbol.id);
		}
	}
	for (const [id, route] of Object.entries(routeLock.routes)) {
		if (!generatedIDs.has(id) && !route.redirect) {
			if (seedRoutes) {
				delete routeLock.routes[id];
				continue;
			}
			throw new Error(`orphan reference route lock ${id} requires an explicit redirect`);
		}
	}
}

function synchronizeGoFacadeAliases(modules: ReferenceModule[]): void {
	const core = modules.find((module) => module.slug === 'core');
	const facade = modules.find((module) => module.slug === 'ridu');
	if (!core || !facade) return;
	for (const alias of facade.symbols) {
		const targetName = alias.signature.match(/^type\s+\w+(?:\[[^\]]+\])?\s*=\s*core\.(\w+)/)?.[1];
		if (!targetName) continue;
		const target = core.symbols.find((symbol) => symbol.name === targetName);
		if (!target) continue;
		if (target.parameters.length > 0) {
			alias.parameters = target.parameters.map((parameter) => ({ ...parameter }));
		}
		if (!alias.parametersLabel) alias.parametersLabel = target.parametersLabel;
		if (target.returns) alias.returns = { ...target.returns };
	}
}

function resolveEditorialLinks(modules: ReferenceModule[]): void {
	const entries = new Map(
		modules.flatMap((module) =>
			module.symbols.map((symbol) => [symbol.id, { module, symbol }] as const)
		)
	);
	const hrefFor = (target: string): string | undefined => {
		if (target.startsWith('/reference/')) return target;
		const entry = entries.get(target);
		return entry ? `/reference/${entry.module.slug}/${entry.symbol.slug}/` : undefined;
	};
	for (const module of modules) {
		for (const symbol of module.symbols) {
			const overlay = referenceEditorialOverlays[symbol.id];
			if (!overlay) continue;
			for (const [label, target] of Object.entries(overlay.typeLinkIDs)) {
				const href = hrefFor(target);
				if (!href) throw new Error(`${symbol.id} links to missing declaration ${target}`);
				symbol.typeLinks[label] = href;
			}
			for (const editorial of overlay.parameters) {
				if (!editorial.targetID) continue;
				const href = hrefFor(editorial.targetID);
				if (!href) throw new Error(`${symbol.id} member links to missing ${editorial.targetID}`);
				const parameter = symbol.parameters.find((candidate) => candidate.name === editorial.name);
				if (parameter) parameter.href = href;
			}
			for (const editorial of overlay.options ?? []) {
				if (!editorial.targetID) continue;
				const href = hrefFor(editorial.targetID);
				if (!href) throw new Error(`${symbol.id} option links to missing ${editorial.targetID}`);
				const option = symbol.options?.find((candidate) => candidate.name === editorial.name);
				if (option) option.href = href;
			}
		}
	}
}

function validateOverlays(generatedIDs: ReadonlySet<string>): void {
	for (const id of Object.keys(referenceEditorialOverlays)) {
		if (!generatedIDs.has(id)) throw new Error(`orphan reference editorial overlay ${id}`);
	}
}

function validateCLI(repositoryRoot: string, modules: readonly ReferenceModule[]): void {
	const cli = referenceModuleRegistry.find((module) => module.slug === 'cli');
	const cliModule = modules.find((module) => module.slug === 'cli');
	if (!cli || !cliModule) return;
	const snapshots = cli.sources.flatMap((source) =>
		source.kind === 'cli'
			? source.helpSnapshots.map((snapshot) =>
					readFileSync(path.join(repositoryRoot, snapshot), 'utf8')
				)
			: []
	);
	const rootCommands = cliModule.symbols
		.filter(
			(symbol) =>
				symbol.kind === 'command' &&
				symbol.name.startsWith('ridu ') &&
				!symbol.name.slice('ridu '.length).includes(' ')
		)
		.map((symbol) => symbol.name.replace(/^ridu\s+/, ''));
	for (const command of rootCommands) {
		if (
			!snapshots.some((snapshot) =>
				new RegExp(`^\\s+${escapeRegExp(command)}\\s+`, 'm').test(snapshot)
			)
		) {
			throw new Error(
				`CLI reference command ridu ${command} is absent from reviewed help snapshots`
			);
		}
	}
}

function validatePackageExportMaps(repositoryRoot: string): void {
	const registeredManifests = new Set<string>();
	for (const module of referenceModuleRegistry) {
		for (const source of module.sources) {
			if (source.kind !== 'typescript') continue;
			registeredManifests.add(source.packageJSON);
			const manifest = JSON.parse(
				readFileSync(path.join(repositoryRoot, source.packageJSON), 'utf8')
			) as { name: string; exports?: Record<string, unknown> | string };
			const exportKeys =
				typeof manifest.exports === 'string' ? ['.'] : Object.keys(manifest.exports ?? {});
			const registered = new Set(
				source.entrypoints.map((entrypoint) =>
					entrypoint.specifier === manifest.name
						? '.'
						: `.${entrypoint.specifier.slice(manifest.name.length)}`
				)
			);
			for (const exportKey of exportKeys) {
				if (!registered.has(exportKey)) {
					throw new Error(`${manifest.name} export map entry ${exportKey} is not registered`);
				}
			}
		}
	}

	const workspaceManifests = [
		'admin/package.json',
		...readdirSync(path.join(repositoryRoot, 'packages'), { withFileTypes: true })
			.filter((entry) => entry.isDirectory())
			.map((entry) => `packages/${entry.name}/package.json`)
			.filter((manifestPath) => existsSync(path.join(repositoryRoot, manifestPath)))
	];
	const unregistered = workspaceManifests.flatMap((manifestPath) => {
		const manifest = JSON.parse(readFileSync(path.join(repositoryRoot, manifestPath), 'utf8')) as {
			name?: string;
			private?: boolean;
			exports?: unknown;
		};
		return !manifest.private && manifest.exports && !registeredManifests.has(manifestPath)
			? [`${manifest.name ?? manifestPath} (${manifestPath})`]
			: [];
	});
	if (unregistered.length > 0) {
		throw new Error(`Unregistered public npm export maps:\n${unregistered.sort().join('\n')}`);
	}
}

function validatePublicGoPackages(repositoryRoot: string): void {
	const modulePath = 'github.com/riducms/ridu';
	const registered = new Set(
		referenceModuleRegistry.flatMap((module) =>
			module.sources.flatMap((source) => {
				if (source.kind !== 'go') return [];
				const relative = source.pattern.replace(/^\.\//, '');
				return [relative === '.' ? modulePath : `${modulePath}/${relative}`];
			})
		)
	);
	const process = Bun.spawnSync(['go', 'list', '-f', '{{.ImportPath}}', './...'], {
		cwd: repositoryRoot,
		stdout: 'pipe',
		stderr: 'pipe'
	});
	if (process.exitCode !== 0) {
		throw new Error(`Public Go package discovery failed:\n${process.stderr.toString()}`);
	}
	const excludedRoots = new Set(['cmd', 'examples', 'internal', 'playground', 'tests', 'website']);
	const publicPackages = process.stdout
		.toString()
		.trim()
		.split('\n')
		.filter(Boolean)
		.filter((importPath) => {
			if (importPath === modulePath) return true;
			if (!importPath.startsWith(`${modulePath}/`)) return false;
			return !excludedRoots.has(importPath.slice(modulePath.length + 1).split('/')[0] ?? '');
		});
	const missing = publicPackages.filter((importPath) => !registered.has(importPath)).sort();
	if (missing.length > 0) {
		throw new Error(`Unregistered public Go reference packages:\n${missing.join('\n')}`);
	}
}

function validateCatalogSize(serialized: string): void {
	const rawBytes = Buffer.byteLength(serialized);
	const gzipBytes = gzipSync(serialized, { level: 9 }).byteLength;
	if (rawBytes > REFERENCE_CATALOG_MAX_BYTES || gzipBytes > REFERENCE_CATALOG_MAX_GZIP_BYTES) {
		throw new Error(
			`Generated reference catalog exceeds its size budget: ${rawBytes} raw bytes / ${gzipBytes} gzip bytes ` +
				`(limits ${REFERENCE_CATALOG_MAX_BYTES} / ${REFERENCE_CATALOG_MAX_GZIP_BYTES})`
		);
	}
}

function mergeParameterDescriptions(
	structural: readonly ReferenceParameter[],
	editorial: ReferenceEditorialOverlay['parameters']
): ReferenceParameter[] {
	const descriptions = new Map(editorial.map((parameter) => [parameter.name, parameter]));
	return structural.map((parameter) => {
		const overlay = descriptions.get(parameter.name);
		return {
			...parameter,
			description: overlay?.description || parameter.description || `The ${parameter.name} value.`
		};
	});
}

function uniqueByID(declarations: readonly ExtractedDeclaration[]): ExtractedDeclaration[] {
	const result = new Map<string, ExtractedDeclaration>();
	for (const declaration of declarations) {
		const existing = result.get(declaration.id);
		if (existing) {
			existing.aliases = [...new Set([...existing.aliases, ...declaration.aliases])];
			continue;
		}
		result.set(declaration.id, declaration);
	}
	return [...result.values()];
}

function firstParagraph(value: string): string | undefined {
	const paragraph = value
		.trim()
		.split(/\n\s*\n/, 1)[0]
		?.replaceAll(/\s+/g, ' ')
		.trim();
	return paragraph || undefined;
}

function declarationSlug(name: string): string {
	return name
		.replaceAll('.', '-')
		.replace(/([a-z0-9])([A-Z])/g, '$1-$2')
		.replace(/([A-Z]+)([A-Z][a-z])/g, '$1-$2')
		.replaceAll(/[^A-Za-z0-9]+/g, '-')
		.replaceAll(/^-|-$/g, '')
		.toLowerCase();
}

function groupForKind(kind: ReferenceKind): string {
	return {
		function: 'Functions',
		type: 'Types',
		method: 'Methods',
		interface: 'Interfaces',
		class: 'Classes',
		constant: 'Constants',
		variable: 'Variables',
		component: 'Components',
		command: 'Commands'
	}[kind];
}

function assertPortable(catalog: ReferenceCatalogFile): void {
	const serialized = stableJSON(catalog);
	if (/\/Users\/|[A-Za-z]:\\\\/.test(serialized)) {
		throw new Error('generated reference catalog contains an absolute path');
	}
	if (/"(?:generatedAt|timestamp)"/.test(serialized)) {
		throw new Error('generated reference catalog contains a timestamp');
	}
}

function escapeRegExp(value: string): string {
	return value.replaceAll(/[.*+?^${}()|[\]\\]/g, '\\$&');
}
