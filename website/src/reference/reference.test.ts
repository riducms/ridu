/// <reference types="bun-types" />

import { existsSync, readFileSync, readdirSync } from 'node:fs';
import path from 'node:path';
import { describe, expect, test } from 'bun:test';
import ts from 'typescript';
import {
	findReferenceModule,
	findReferenceSymbol,
	groupReferenceSymbols,
	inlineReferencedTypeEntries,
	receiverMethodEntries,
	referencedTypeEntries,
	resolvedReferenceTypeLinks,
	referenceSymbolDisplayName,
	referenceModules,
	shouldInlineReferencedTypes,
	type ReferenceModule,
	type ReferenceSymbol
} from './index';

const expectedCatalog = {
	moduleSymbols: {
		ridu: 188,
		core: 360,
		field: 254,
		query: 65,
		schema: 153,
		store: 146,
		storage: 8,
		migration: 87,
		'migration-payload': 13,
		'go-protocol': 52,
		plugintest: 3,
		postgres: 85,
		sqlite: 71,
		mongodb: 73,
		graphql: 8,
		mcp: 8,
		richtext: 19,
		seo: 24,
		formbuilder: 35,
		'storage-local': 7,
		'storage-s3': 9,
		'store-conformance': 2,
		sdk: 137,
		protocol: 110,
		plugin: 115,
		build: 9,
		ui: 36,
		'plugin-richtext': 7,
		'plugin-seo': 8,
		'plugin-form-builder': 34,
		admin: 4,
		translations: 31,
		'cli-launcher': 1,
		cli: 24
	}
} as const;

const expectedGroupOrders: Readonly<Record<string, readonly string[]>> = {
	ridu: [
		'Functions',
		'Types',
		'Configuration',
		'Runtime setup',
		'Local API',
		'Authentication',
		'Uploads',
		'Durable tasks',
		'Access',
		'Hooks',
		'Custom endpoints',
		'Plugins',
		'Observability',
		'Application contracts',
		'Runtime'
	],
	core: [
		'Types',
		'Runtime',
		'Application',
		'Local API',
		'Local API options',
		'Authentication',
		'Uploads',
		'Publishing',
		'Document locks',
		'Live preview',
		'Preferences',
		'Generated Go API',
		'Access',
		'Hooks',
		'Interfaces',
		'Capabilities',
		'Custom endpoints',
		'Descriptors'
	],
	field: ['Field builders', 'Field helpers', 'Field options', 'Option contracts', 'Core types'],
	query: [
		'Comparisons',
		'Logical operators',
		'Values',
		'Paths',
		'Result controls',
		'Expressions',
		'Adapter contracts'
	],
	schema: [
		'Functions',
		'Types',
		'Manifest model',
		'Capability settings',
		'Field details',
		'Plugin manifest',
		'Validation'
	],
	store: [
		'Interfaces',
		'Types',
		'Values',
		'Functions',
		'Methods',
		'Optional store capabilities',
		'Authentication adapter contracts',
		'Version adapter contracts',
		'Publishing adapter contracts',
		'Task adapter contracts',
		'Reference integrity',
		'Safety bounds',
		'Errors'
	],
	storage: ['Adapter contracts', 'Optional capabilities', 'Errors'],
	migration: ['Artifacts', 'Review model', 'Execution', 'Payloads'],
	'migration-payload': ['Workflow', 'Import model'],
	'go-protocol': [
		'Protocol',
		'Errors',
		'Content envelopes',
		'Authentication',
		'Authoring state',
		'Schema'
	],
	plugintest: ['Conformance'],
	sdk: [
		'Functions',
		'Classes',
		'Collections',
		'Globals',
		'Authentication',
		'Uploads',
		'Versions and publishing',
		'Trash',
		'Schema and preferences',
		'Custom endpoints',
		'Access and locks',
		'Live preview',
		'Plugins',
		'Interfaces',
		'Types'
	],
	protocol: ['Types', 'Functions', 'Interfaces'],
	plugin: ['Functions', 'Interfaces', 'Types'],
	build: ['Functions', 'Interfaces'],
	ui: ['Types', 'Functions'],
	postgres: ['Functions', 'Types', 'Methods', 'Production migrations'],
	sqlite: ['Functions', 'Types', 'Methods', 'Migrations'],
	mongodb: ['Functions', 'Types', 'Methods', 'Migrations'],
	graphql: ['Functions', 'Types'],
	mcp: ['Functions', 'Types'],
	richtext: ['Functions', 'Types'],
	'plugin-richtext': ['Types', 'Interfaces'],
	seo: ['Functions', 'Types'],
	'plugin-seo': ['Functions', 'Types', 'Interfaces'],
	formbuilder: ['Functions', 'Types'],
	'plugin-form-builder': ['Functions', 'Types', 'Interfaces'],
	'storage-local': ['Functions', 'Types', 'Methods'],
	'storage-s3': ['Functions', 'Types', 'Methods'],
	cli: ['Commands', 'Agent guidance', 'Migrations', 'Plugins']
};

const routeSegment = /^[a-z0-9]+(?:-[a-z0-9]+)*$/;
const repositoryRoot = path.resolve(import.meta.dir, '../../..');

const typescriptEntrypoints: Readonly<Record<string, readonly string[]>> = {
	sdk: ['packages/sdk/src/index.ts'],
	protocol: ['packages/protocol/src/index.ts'],
	plugin: ['packages/plugin/src/index.ts'],
	build: ['packages/build/src/index.ts', 'packages/build/src/vite/index.ts'],
	ui: ['packages/ui/src/index.ts'],
	'plugin-richtext': ['packages/plugin-richtext/src/index.ts'],
	'plugin-seo': ['packages/plugin-seo/src/index.ts'],
	'plugin-form-builder': [
		'packages/plugin-form-builder/src/index.ts',
		'packages/plugin-form-builder/src/admin.ts'
	]
};

const goPackageDirectories: Readonly<Record<string, string>> = {
	ridu: '.',
	core: 'core',
	field: 'field',
	query: 'query',
	schema: 'schema',
	store: 'store',
	storage: 'storage',
	migration: 'migration',
	plugintest: 'plugintest',
	postgres: 'adapters/postgres',
	sqlite: 'adapters/sqlite',
	mongodb: 'adapters/mongodb',
	graphql: 'plugins/graphql',
	mcp: 'plugins/mcp',
	richtext: 'plugins/richtext',
	seo: 'plugins/seo',
	formbuilder: 'plugins/formbuilder',
	'storage-local': 'adapters/storage/local',
	'storage-s3': 'adapters/storage/s3'
};

describe('reference data', () => {
	test('matches the reviewed integrated catalog snapshot', () => {
		const moduleSymbols = Object.fromEntries(
			referenceModules.map((module) => [module.slug, module.symbols.length])
		);
		expect(moduleSymbols).toEqual(expectedCatalog.moduleSymbols);
	});

	test('keeps every module and symbol unique and navigable', () => {
		const moduleNames = referenceModules.map((module) => module.name);
		const moduleSlugs = referenceModules.map((module) => module.slug);
		const packageNames = referenceModules.map((module) => module.packageName);
		const symbolRoutes = referenceModules.flatMap((module) =>
			module.symbols.map((symbol) => `${module.slug}/${symbol.slug}`)
		);

		expect(new Set(moduleNames).size).toBe(moduleNames.length);
		expect(new Set(moduleSlugs).size).toBe(moduleSlugs.length);
		expect(new Set(packageNames).size).toBe(packageNames.length);
		expect(new Set(symbolRoutes).size).toBe(symbolRoutes.length);

		for (const module of referenceModules) {
			expect(module.slug, `${module.name} has a non-route-safe module slug`).toMatch(routeSegment);
			expect(
				findReferenceModule(module.slug),
				`${module.slug} is not selectable by its route`
			).toBe(module);
			expect(module.symbols.length, `${module.slug} has no navigable symbols`).toBeGreaterThan(0);

			const symbolNames = module.symbols.map((symbol) => symbol.name);
			expect(new Set(symbolNames).size, `${module.slug} repeats a symbol name`).toBe(
				symbolNames.length
			);
			for (const symbol of module.symbols) {
				expect(
					symbol.slug,
					`${module.slug}/${symbol.name} has a non-route-safe symbol slug`
				).toMatch(routeSegment);
				expect(
					findReferenceSymbol(module.slug, symbol.slug),
					`${module.slug}/${symbol.slug} is not selectable by its route`
				).toBe(symbol);
			}
		}
	});

	test('requires complete module, symbol, parameter, and return metadata', () => {
		for (const module of referenceModules) {
			for (const [field, value] of Object.entries({
				name: module.name,
				packageName: module.packageName,
				group: module.group,
				summary: module.summary
			})) {
				expect(value.trim(), `${module.slug} has empty ${field} metadata`).not.toBe('');
			}
			for (const symbol of module.symbols) assertCompleteSymbol(module, symbol);
		}
	});

	test('uses a complete curated group order without changing order inside each group', () => {
		for (const module of referenceModules) {
			const expectedOrder = expectedGroupOrders[module.slug];
			if (expectedOrder) {
				expect(
					module.groupOrder.slice(0, expectedOrder.length),
					`${module.slug} reviewed group priority drifted`
				).toEqual([...expectedOrder]);
			}

			const grouped = groupReferenceSymbols(module);
			expect([...grouped.keys()]).toEqual(module.groupOrder);
			for (const [group, symbols] of grouped) {
				expect(symbols, `${module.slug}/${group} changed symbol order`).toEqual(
					module.symbols.filter((symbol) => symbol.group === group)
				);
			}
		}
	});

	test('omits implied TypeScript packages and preserves useful Go and receiver qualifiers', () => {
		expect(displayName('sdk', 'upload-image-input')).toEqual({
			qualifier: '',
			label: 'UploadImageInput'
		});
		expect(displayName('sdk', 'ridu-client-list')).toEqual({
			qualifier: 'RiduClient.',
			label: 'list'
		});
		expect(displayName('field', 'text')).toEqual({ qualifier: 'field.', label: 'Text' });
		expect(displayName('go-protocol', 'current-version')).toEqual({
			qualifier: 'protocol.',
			label: 'CurrentVersion'
		});
		expect(displayName('migration-payload', 'export')).toEqual({
			qualifier: 'payload.',
			label: 'Export'
		});
		expect(displayName('storage-local', 'backend')).toEqual({
			qualifier: 'local.',
			label: 'Backend'
		});
		expect(displayName('cli', 'new')).toEqual({ qualifier: '', label: 'ridu new' });
	});

	test('only links to registered reference symbols', () => {
		for (const module of referenceModules) {
			for (const symbol of module.symbols) {
				const links = [
					...Object.entries(symbol.typeLinks),
					...symbol.parameters.flatMap((parameter) =>
						parameter.href ? [[parameter.name, parameter.href] as const] : []
					)
				];
				for (const [label, href] of links) {
					expect(
						label.trim(),
						`${module.slug}/${symbol.slug} has an empty type-link label`
					).not.toBe('');
					const match = href.match(/^\/reference\/([^/]+)\/([^/]+)\/$/);
					expect(
						match,
						`${module.slug}/${symbol.slug} has an invalid reference URL for ${label}: ${href}`
					).not.toBeNull();
					if (!match) continue;
					expect(
						findReferenceSymbol(match[1], match[2]),
						`${module.slug}/${symbol.slug} links to missing ${href}`
					).toBeDefined();
				}
			}
		}
	});

	test('resolves only directly referenced type-like declarations in source order', () => {
		expect(referencedRoutes('sdk', 'create-client')).toEqual([
			'sdk/ridu-config-shape',
			'sdk/client-options',
			'sdk/ridu-client'
		]);
		expect(referencedRoutes('sdk', 'ridu-client-schema')).toEqual([
			'sdk/request-options',
			'protocol/schema-manifest'
		]);
		expect(referencedRoutes('ridu', 'new')).toEqual(['ridu/config', 'store/store', 'core/app']);
		expect(referencedRoutes('ridu', 'execute')).toEqual(['ridu/config', 'ridu/execute-option']);
		expect(referencedRoutes('ridu', 'with-store')).toEqual([
			'ridu/store-factory',
			'ridu/execute-option'
		]);
		expect(referencedRoutes('core', 'local-api')).toEqual([]);
		expect(referencedRoutes('ridu', 'handler-options')).toEqual([
			'core/handler-options',
			'ridu/audit-event',
			'ridu/request-observation',
			'ridu/request-error-event',
			'ridu/readiness-check'
		]);
	});

	test('keeps Bun-style inlining focused and exposes receiver method indexes', () => {
		const request = findReferenceSymbol('store', 'request');
		const handlerOptions = findReferenceSymbol('ridu', 'handler-options');
		const withStore = findReferenceSymbol('ridu', 'with-store');
		const hookContext = findReferenceSymbol('ridu', 'hook-context');
		const typedTaskAlias = findReferenceSymbol('ridu', 'typed-task');
		const app = findReferenceSymbol('core', 'app');
		const core = findReferenceModule('core');
		expect(request && shouldInlineReferencedTypes(request)).toBeFalse();
		expect(handlerOptions && shouldInlineReferencedTypes(handlerOptions)).toBeTrue();
		expect(withStore && shouldInlineReferencedTypes(withStore)).toBeTrue();
		expect(handlerOptions && inlineReferencedTypeEntries(handlerOptions)).toEqual([]);
		expect(hookContext && inlineReferencedTypeEntries(hookContext)).toEqual([]);
		expect(
			typedTaskAlias &&
				inlineReferencedTypeEntries(typedTaskAlias).map((entry) => entry.symbol.name)
		).toEqual([]);
		expect(typedTaskAlias?.parameters.map((parameter) => parameter.name)).toEqual([
			'TaskSlug',
			'Enqueue',
			'Result',
			'Cancel'
		]);
		expect(
			core && app && receiverMethodEntries(core, app).map((entry) => entry.symbol.name)
		).toContain('App.RunTasks');
		const ridu = findReferenceModule('ridu');
		const riduApp = findReferenceSymbol('ridu', 'app');
		expect(
			ridu && riduApp && receiverMethodEntries(ridu, riduApp).map((entry) => entry.symbol.name)
		).toContain('App.RunTasks');
		const local = findReferenceSymbol('core', 'local-api');
		expect(
			core && local && receiverMethodEntries(core, local).map((entry) => entry.symbol.name)
		).not.toContain('LocalAPI.Bulk operations');
		for (const owner of [
			[core, app],
			[ridu, riduApp],
			[core, local]
		] as const) {
			if (!owner[0] || !owner[1]) continue;
			for (const entry of receiverMethodEntries(owner[0], owner[1])) {
				expect(
					findReferenceSymbol(entry.module.slug, entry.symbol.slug),
					`${owner[0].slug}/${owner[1].slug} links to a missing receiver route`
				).toBe(entry.symbol);
			}
		}
	});

	test('keeps root facade aliases as useful as their canonical core contracts', () => {
		const source = readFileSync(path.join(repositoryRoot, 'ridu.go'), 'utf8');
		const aliases = [...source.matchAll(/^type \(\n([\s\S]*?)^\)/gm)].flatMap((block) =>
			[...block[1].matchAll(/^\s*(\w+)(?:\[[^\]]+\])?\s+= core\.(\w+)/gm)].map((match) => ({
				name: match[1],
				target: match[2]
			}))
		);

		for (const { name, target } of aliases) {
			const facade = findReferenceModule('ridu')?.symbols.find((symbol) => symbol.name === name);
			const canonical = findReferenceModule('core')?.symbols.find(
				(symbol) => symbol.name === target
			);
			expect(facade, `ridu reference is missing facade alias ${name}`).toBeDefined();
			expect(canonical, `core reference is missing canonical contract ${target}`).toBeDefined();
			if (!facade || !canonical) continue;

			expect(facade.signature, `${name} should show its source alias`).toContain(
				`= core.${target}`
			);
			expect(facade.typeLinks[`core.${target}`]).toBe(`/reference/core/${canonical.slug}/`);
			if (canonical.parameters.length > 0) {
				expect(
					facade.parameters,
					`${name} should expose every ${target} member without requiring a second page`
				).toEqual(canonical.parameters);
				if (canonical.parametersLabel) {
					expect(facade.parametersLabel).toBe(canonical.parametersLabel);
				}
			}
			for (const detail of canonical.details) {
				expect(facade.details).toContain(detail);
			}
		}
	});

	test('documents every public core struct as member rows', () => {
		const sparseStructs =
			findReferenceModule('core')?.symbols.filter(
				(symbol) =>
					symbol.kind === 'type' &&
					/struct\s*\{/.test(symbol.signature) &&
					symbol.parameters.length === 0
			) ?? [];
		expect(sparseStructs.map((symbol) => symbol.name)).toEqual([]);

		const localization = findReferenceSymbol('core', 'localization-config');
		expect(localization?.parameters.map((parameter) => parameter.name)).toEqual([
			'Locales',
			'DefaultLocale',
			'DisableFallback',
			'AvailableLocales'
		]);
		expect(localization?.details.join(' ')).toContain('Leave Locales empty');
		expect(localization?.details.join(' ')).toContain('admin request');
		expect(localization?.example).toContain('FallbackLocales');
	});

	test('infers safe same-package and qualified type links', () => {
		const admin = findReferenceSymbol('core', 'admin-config');
		const typedTask = findReferenceSymbol('core', 'typed-task');
		expect(admin && resolvedReferenceTypeLinks(admin)['schema.CollectionSlug']).toBe(
			'/reference/schema/collection-slug/'
		);
		expect(typedTask && resolvedReferenceTypeLinks(typedTask).TaskEnqueueOptions).toBe(
			'/reference/core/task-enqueue-options/'
		);
		expect(typedTask && resolvedReferenceTypeLinks(typedTask).Input).toBeUndefined();
		const pluginAdd = findReferenceSymbol('cli', 'plugin-add');
		expect(pluginAdd).toBeDefined();
		expect(pluginAdd && resolvedReferenceTypeLinks(pluginAdd).constructor).toBeUndefined();
	});

	test('keeps curated receiver method lists in sync with their Go declarations', () => {
		const contracts = [
			['core', 'core/typed_local.go', 'BoundTypedCollection'],
			['core', 'core/typed_global.go', 'BoundTypedGlobal'],
			['field', 'field/field.go', 'Definition'],
			['field', 'field/field.go', 'DefaultValue']
		] as const;

		for (const [moduleSlug, sourcePath, receiver] of contracts) {
			const symbol = findReferenceModule(moduleSlug)?.symbols.find(
				(candidate) => candidate.name === receiver
			);
			expect(symbol, `${moduleSlug} reference is missing ${receiver}`).toBeDefined();
			const source = readFileSync(path.join(repositoryRoot, sourcePath), 'utf8');
			const methods = [
				...source.matchAll(
					new RegExp(`^func \\([^)]*\\b${receiver}(?:\\[[^\\]]+\\])?\\) ([A-Z]\\w*)\\s*\\(`, 'gm')
				)
			].map((match) => match[1]);
			const documented = symbol?.parameters
				.map((parameter) => parameter.name.match(/^([A-Z]\w*)/)?.[1])
				.filter(Boolean);
			expect(documented, `${moduleSlug}/${symbol?.slug} method list drifted`).toEqual(methods);
		}
	});

	test('documents every App method with its exact current Go signature', () => {
		const core = findReferenceModule('core');
		const app = core && findReferenceSymbol('core', 'app');
		expect(core).toBeDefined();
		expect(app).toBeDefined();
		if (!core || !app) return;

		const documented = new Map(
			receiverMethodEntries(core, app).map((entry) => [
				entry.symbol.name.slice('App.'.length),
				entry.symbol
			])
		);
		const sourceMethods = readdirSync(path.join(repositoryRoot, 'core'))
			.filter((file) => file.endsWith('.go') && !file.endsWith('_test.go'))
			.flatMap((file) => {
				const source = readFileSync(path.join(repositoryRoot, 'core', file), 'utf8');
				return [
					...source.matchAll(/^func \(application \*App\) ([A-Z]\w*)\((.*)\)\s*([^\{]*)\s*\{/gm)
				].map((match) => ({
					name: match[1],
					signature: `func (application *App) ${match[1]}(${match[2]})${match[3].trim() ? ` ${match[3].trim()}` : ''}`
				}));
			});

		expect(documented.size, 'App method reference count drifted').toBe(sourceMethods.length);
		for (const method of sourceMethods) {
			const reference = documented.get(method.name);
			expect(reference, `core is missing App.${method.name}`).toBeDefined();
			expect(reference?.signature.replaceAll(/\s+/g, ' ').trim()).toBe(
				method.signature.replaceAll(/\s+/g, ' ').trim()
			);
		}
	});

	test('documents every LocalAPI method with its exact current Go signature', () => {
		const core = findReferenceModule('core');
		const localAPI = core && findReferenceSymbol('core', 'local-api');
		expect(core).toBeDefined();
		expect(localAPI).toBeDefined();
		if (!core || !localAPI) return;

		const documented = new Map(
			receiverMethodEntries(core, localAPI).map((entry) => [
				entry.symbol.name.slice('LocalAPI.'.length),
				entry.symbol
			])
		);
		const source = readFileSync(path.join(repositoryRoot, 'core/local.go'), 'utf8');
		const sourceMethods = [
			...source.matchAll(/^func \(local \*LocalAPI\) ([A-Z]\w*)\((.*)\)\s*([^\{]*)\s*\{/gm)
		].map((match) => ({
			name: match[1],
			signature: `func (local *LocalAPI) ${match[1]}(${match[2]})${match[3].trim() ? ` ${match[3].trim()}` : ''}`
		}));

		expect(documented.size, 'LocalAPI method reference count drifted').toBe(sourceMethods.length);
		for (const method of sourceMethods) {
			const reference = documented.get(method.name);
			expect(reference, `core is missing LocalAPI.${method.name}`).toBeDefined();
			expect(reference?.signature.replaceAll(/\s+/g, ' ').trim()).toBe(
				method.signature.replaceAll(/\s+/g, ' ').trim()
			);
		}
	});

	test('covers every exported Go type and top-level function by its exact name', () => {
		for (const [moduleSlug, directory] of Object.entries(goPackageDirectories)) {
			const module = findReferenceModule(moduleSlug);
			expect(module, `Go source coverage references missing module ${moduleSlug}`).toBeDefined();
			if (!module) continue;

			const publicNames = new Set<string>();
			for (const filename of readdirSync(path.join(repositoryRoot, directory))) {
				if (!filename.endsWith('.go') || filename.endsWith('_test.go')) continue;
				const source = readFileSync(path.join(repositoryRoot, directory, filename), 'utf8');
				for (const match of source.matchAll(/^(?:type|func) ([A-Z]\w*)/gm)) {
					publicNames.add(match[1]);
				}
			}

			for (const name of publicNames) {
				expect(
					module.symbols.some((symbol) => symbol.name === name),
					`${moduleSlug} does not provide an exact reference entry for public Go export ${name}`
				).toBeTrue();
			}
		}
	});

	test('keeps public compatibility and safety constants exact and searchable', () => {
		const contracts = [
			['core', 'core/version.go', 'FrameworkVersion'],
			['core', 'core/plugin.go', 'PluginAPIVersion'],
			['core', 'core/plugin.go', 'AdminPluginAPIVersion'],
			['core', 'core/task.go', 'MaxTaskPayloadBytes'],
			['schema', 'schema/manifest.go', 'CurrentVersion'],
			['schema', 'schema/manifest.go', 'CurrentPluginAPIVersion'],
			['schema', 'schema/manifest.go', 'CurrentAdminPluginAPIVersion'],
			['store', 'store/store.go', 'MaxListWindowDocuments'],
			['store', 'store/store.go', 'MaxUploadReferenceCandidates'],
			['store', 'store/population.go', 'MaxPopulationMaterializedDocuments'],
			['store', 'store/auth_maintenance.go', 'MaxAuthPruneBatch'],
			...[
				'MaxTaskPayloadBytes',
				'MaxTaskErrorBytes',
				'MaxTaskBatch',
				'MaxTaskAttempts',
				'MaxTaskConcurrencyKeyBytes',
				'MaxTaskReferenceIDBytes',
				'MaxTaskRetryDelay',
				'MaxTaskTimeout',
				'MinTaskRetention',
				'MaxTaskRetention',
				'MaxTaskLeaseDuration'
			].map((name) => ['store', 'store/task_validation.go', name] as const)
		] as const;

		for (const [moduleSlug, sourcePath, name] of contracts) {
			const symbol = findReferenceModule(moduleSlug)?.symbols.find(
				(candidate) => candidate.name === name
			);
			expect(symbol, `${moduleSlug} reference is missing public constant ${name}`).toBeDefined();
			const source = readFileSync(path.join(repositoryRoot, sourcePath), 'utf8');
			const declaration = source.match(
				new RegExp(`^\\s*(?:const\\s+)?${name}\\s+(.*?)$`, 'm')
			)?.[0];
			expect(declaration, `${sourcePath} is missing constant ${name}`).toBeDefined();
			expect(symbol?.signature.replaceAll(/\s+/g, ' ')).toContain(
				declaration
					?.trim()
					.replace(/^const\s+/, '')
					.replaceAll(/\s+/g, ' ')
			);
		}
	});

	test('maps the PostgreSQL adapter to its public store capabilities', () => {
		const postgresStore = findReferenceSymbol('postgres', 'store');
		expect(postgresStore).toBeDefined();
		expect(postgresStore?.parametersLabel).toBe('Implemented capabilities');
		expect(postgresStore?.parameters.map((parameter) => parameter.name)).toEqual([
			'store.Store',
			'store.SnapshotStore',
			'store.HealthStore',
			'store.ReadinessStore',
			'store.MigrationReadinessStore',
			'store.PreferenceStore',
			'store.DocumentLockStore',
			'store.AuthStore',
			'store.AuthMaintenanceStore',
			'store.AuthUnlockStore',
			'store.TaskStore',
			'store.UploadObjectLocker'
		]);
		for (const capability of postgresStore?.parameters ?? []) {
			expect(
				capability.href && findReferenceSymbol('store', capability.href.split('/').at(-2) ?? ''),
				`postgres.Store capability ${capability.name} links to a missing store contract`
			).toBeDefined();
		}
	});

	test('maps the SQLite adapter to its public store capabilities', () => {
		const sqliteStore = findReferenceSymbol('sqlite', 'store');
		expect(sqliteStore).toBeDefined();
		expect(sqliteStore?.parametersLabel).toBe('Implemented capabilities');
		expect(sqliteStore?.parameters.map((parameter) => parameter.name)).toEqual([
			'store.Store',
			'store.SnapshotStore',
			'store.HealthStore',
			'store.ReadinessStore',
			'store.MigrationReadinessStore',
			'store.PreferenceStore',
			'store.DocumentLockStore',
			'store.AuthStore',
			'store.AuthMaintenanceStore',
			'store.AuthUnlockStore',
			'store.TaskStore',
			'store.UploadObjectLocker'
		]);
	});

	test('maps the MongoDB adapter to its public store capabilities and bounded lifecycle', () => {
		const mongodbStore = findReferenceSymbol('mongodb', 'store');
		expect(mongodbStore).toBeDefined();
		expect(mongodbStore?.parametersLabel).toBe('Implemented capabilities');
		expect(mongodbStore?.parameters.map((parameter) => parameter.name)).toEqual([
			'store.Store',
			'store.SnapshotStore',
			'store.HealthStore',
			'store.ReadinessStore',
			'store.MigrationReadinessStore',
			'store.PreferenceStore',
			'store.DocumentLockStore',
			'store.AuthStore',
			'store.AuthMaintenanceStore',
			'store.AuthUnlockStore',
			'store.TaskStore',
			'store.UploadObjectLocker'
		]);
		for (const capability of mongodbStore?.parameters ?? []) {
			expect(
				capability.href && findReferenceSymbol('store', capability.href.split('/').at(-2) ?? ''),
				`mongodb.Store capability ${capability.name} links to a missing store contract`
			).toBeDefined();
		}
		expect(mongodbStore?.details.join(' ')).toContain('MongoDB Community 8.2.9');
		expect(mongodbStore?.details.join(' ')).toContain('not supported');

		const legacyCreator = findReferenceSymbol('mongodb', 'create-artifact');
		expect(legacyCreator?.summary).toContain('frozen legacy');
		expect(legacyCreator?.details.join(' ')).toContain('planner 2.0.0');
		const currentCreator = findReferenceSymbol('mongodb', 'create-artifact-with-options');
		expect(currentCreator?.summary).toContain('planner 2.0.0');
		expect(currentCreator?.details.join(' ')).toContain('ridu migrate create');
		expect(findReferenceSymbol('mongodb', 'store-sync-indexes')?.details.join(' ')).toContain(
			'Production changes use reviewed immutable artifacts'
		);
	});

	test('documents official database selection and bounded migration lifecycles', () => {
		const databaseParameter = findReferenceSymbol('cli', 'new')?.parameters.find(
			(parameter) => parameter.name === '--database'
		);
		expect(databaseParameter?.type).toBe('postgres | sqlite | mongodb');
		expect(
			findReferenceSymbol('cli', 'dev')?.parameters.map((parameter) => parameter.name)
		).toContain('--database-path');

		const migrateCreate = findReferenceSymbol('cli', 'migrate-create');
		expect(migrateCreate?.parameters.map((parameter) => parameter.name)).toContain('--transform');
		expect(
			migrateCreate?.parameters.find((parameter) => parameter.name === '--accept-renames')
				?.description
		).toContain('MongoDB');
		expect(
			migrateCreate?.parameters.find((parameter) => parameter.name === '--transform')?.description
		).toContain('MongoDB');

		for (const slug of ['migrate-plan', 'migrate-status', 'migrate-verify', 'migrate-up']) {
			const command = findReferenceSymbol('cli', slug);
			expect(command, `cli/${slug} is missing`).toBeDefined();
			expect(command?.details.join(' '), `cli/${slug} omits MongoDB`).toContain('MongoDB');
		}

		const postgresRunner = findReferenceSymbol('cli', 'migration-runner-options');
		expect(postgresRunner?.name).toBe('PostgreSQL migration runner options');
		for (const option of [
			'--lock-timeout',
			'--statement-timeout',
			'--batch-timeout',
			'--idle-in-transaction-timeout',
			'--stop-after-phase',
			'--stop-after-step'
		]) {
			expect(postgresRunner?.signature, `PostgreSQL runner omits ${option}`).toContain(option);
		}

		const mongodbRunner = findReferenceSymbol('cli', 'mongodb-migration-runner-options');
		expect(mongodbRunner?.name).toBe('MongoDB migration runner options');
		expect(mongodbRunner?.signature).toBe(
			'--database-url <url> --allow-insecure-database --allow-maintenance --allow-unbounded --advisory-lock-wait <duration> --concurrent-index-timeout <duration>'
		);
		for (const option of [
			'--lock-timeout',
			'--statement-timeout',
			'--batch-timeout',
			'--idle-in-transaction-timeout',
			'--stop-after-phase',
			'--stop-after-step'
		]) {
			expect(mongodbRunner?.signature, `MongoDB runner wrongly includes ${option}`).not.toContain(
				option
			);
		}

		for (const slug of ['migrate-down', 'migrate-reset', 'migrate-refresh', 'migrate-fresh']) {
			const command = findReferenceSymbol('cli', slug);
			expect(command, `cli/${slug} is missing`).toBeDefined();
			expect(command?.signature).toContain('--database-path <path>');
			expect(command?.signature).toContain('--allow-destructive');
		}
	});

	test('keeps public enum identifier and literal tables in sync with Go source', () => {
		const contracts = [
			['schema', 'field-type', 'FieldType', ['schema/manifest.go']],
			['schema', 'value-type', 'ValueType', ['schema/manifest.go']],
			['schema', 'plugin-database-adapter', 'PluginDatabaseAdapter', ['schema/manifest.go']],
			['query', 'expression-kind', 'ExpressionKind', ['query/expression.go']],
			['query', 'operator', 'Operator', ['query/expression.go']],
			['query', 'value-kind', 'ValueKind', ['query/value.go']],
			['query', 'direction', 'Direction', ['query/controls.go']],
			['store', 'task-state', 'TaskState', ['store/store.go']],
			['store', 'task-backoff', 'TaskBackoff', ['store/store.go']],
			['migration', 'step-kind', 'StepKind', ['migration/migration.go', 'migration/phases.go']],
			['migration', 'phase-mode', 'PhaseMode', ['migration/phases.go']],
			['migration', 'concurrent-index-action', 'ConcurrentIndexAction', ['migration/phases.go']],
			['postgres', 'rename-kind', 'RenameKind', ['adapters/postgres/migrate.go']]
		] as const;

		for (const [moduleSlug, symbolSlug, typeName, sourcePaths] of contracts) {
			const symbol = findReferenceSymbol(moduleSlug, symbolSlug);
			expect(symbol, `${moduleSlug}/${symbolSlug} is missing`).toBeDefined();
			const source = sourcePaths
				.map((sourcePath) => readFileSync(path.join(repositoryRoot, sourcePath), 'utf8'))
				.join('\n');
			const constants = [
				...source.matchAll(
					new RegExp(`^\\s*(?:const\\s+)?([A-Z]\\w*)\\s+${typeName}\\s*=\\s*([^\\s/]+)`, 'gm')
				)
			].map((match) => [match[1], match[2]]);
			expect(constants.length, `${typeName} source constants were not found`).toBeGreaterThan(0);
			expect(symbol?.parameters.map((parameter) => [parameter.name, parameter.type])).toEqual(
				constants
			);
		}

		const notice = findReferenceSymbol('postgres', 'migration-notice');
		expect(notice?.parameters.map((parameter) => [parameter.name, parameter.type])).toEqual([
			['Code', 'string'],
			['Artifact', 'string'],
			['Message', 'string']
		]);
		const provenance = findReferenceSymbol('postgres', 'notice-atlas-provenance');
		expect(provenance?.signature).toContain('"RIDU_ATLAS_PROVENANCE"');
	});

	test('documents every public HandlerOptions field from the Go source', () => {
		const handlerOptions = findReferenceSymbol('ridu', 'handler-options');
		expect(handlerOptions).toBeDefined();

		const source = readFileSync(path.join(repositoryRoot, 'core/http.go'), 'utf8');
		const declaration = source.match(/type HandlerOptions struct \{([\s\S]*?)^\}/m)?.[1];
		expect(declaration, 'core/http.go is missing HandlerOptions').toBeDefined();
		const publicFields = [...(declaration ?? '').matchAll(/^\s*([A-Z]\w*)\s+/gm)].map(
			(match) => match[1]
		);

		expect(handlerOptions?.parameters.map((parameter) => parameter.name)).toEqual(publicFields);
		expect(handlerOptions?.example).toContain('ridu.HandlerOptions{');
	});

	test('documents every public ExecuteOption constructor and its lifecycle contract', () => {
		const executeOption = findReferenceSymbol('ridu', 'execute-option');
		expect(executeOption).toBeDefined();

		const source = readFileSync(path.join(repositoryRoot, 'core/execute.go'), 'utf8');
		const constructors = [...source.matchAll(/^func (With\w+)\([^\n]*\) ExecuteOption \{/gm)].map(
			(match) => match[1]
		);
		expect(executeOption?.parameters.map((parameter) => parameter.name).sort()).toEqual(
			[...constructors].sort()
		);
		expect(executeOption?.details.join(' ')).toContain('argument order');
		expect(executeOption?.details.join(' ')).toContain('before applying these options');
		expect(executeOption?.example).toContain('[]ridu.ExecuteOption{');

		for (const constructor of constructors) {
			const symbol = findReferenceSymbol(
				'ridu',
				constructor.replaceAll(/([a-z0-9])([A-Z])/g, '$1-$2').toLocaleLowerCase()
			);
			expect(symbol, `${constructor} is missing from the ridu reference`).toBeDefined();
			expect(
				symbol?.parameters.length,
				`${constructor} has no parameter documentation`
			).toBeGreaterThan(0);
			expect(symbol?.returns?.type, `${constructor} has no return documentation`).toBe(
				'ExecuteOption'
			);
			expect(symbol?.example, `${constructor} has no practical example`).not.toBe('');
			expect(symbol?.typeLinks.ExecuteOption, `${constructor} does not link ExecuteOption`).toBe(
				'/reference/ridu/execute-option/'
			);
		}

		const withStore = findReferenceSymbol('ridu', 'with-store');
		expect(withStore?.details.join(' ')).toContain('Project commands');
		expect(withStore?.details.join(' ')).toContain('store.ReadinessStore');
		expect(withStore?.details.join(' ')).toContain('Close()');
		expect(findReferenceSymbol('ridu', 'store-factory')?.signature).toBe(
			'type StoreFactory = core.StoreFactory'
		);
		expect(findReferenceSymbol('ridu', 'store-factory')?.parameters[0]?.type).toBe(
			'context.Context'
		);
		expect(findReferenceSymbol('ridu', 'store-factory')?.returns?.type).toBe(
			'(store.Store, error)'
		);
	});

	test('keeps every root core alias navigable to a useful contract', () => {
		const ridu = findReferenceModule('ridu');
		const core = findReferenceModule('core');
		expect(ridu).toBeDefined();
		expect(core).toBeDefined();
		if (!ridu || !core) return;

		const coreByName = new Map(core.symbols.map((symbol) => [symbol.name, symbol]));
		for (const alias of ridu.symbols) {
			const match = alias.signature.match(/^type \w+(?:\[[^\]]+\])? = core\.(\w+)/);
			if (!match) continue;

			const targetName = match[1];
			const target = coreByName.get(targetName);
			if (!target) {
				expect(
					hasUsefulDefinition(alias),
					`ridu/${alias.slug} aliases undocumented core.${targetName} without explaining the contract itself`
				).toBeTrue();
				continue;
			}

			expect(
				alias.typeLinks[`core.${targetName}`],
				`ridu/${alias.slug} does not link its core.${targetName} definition`
			).toBe(`/reference/core/${target.slug}/`);
			expect(
				hasUsefulDefinition(target),
				`core/${target.slug} exists but still hides the public shape of ${targetName}`
			).toBeTrue();
			expect(referencedTypeEntries(alias).map((entry) => entry.symbol.name)).toContain(targetName);
		}
	});

	test('keeps curated core struct members in sync with their Go declarations', () => {
		const contracts = [
			['core/config.go', 'Config'],
			['core/config.go', 'LocalizationConfig'],
			['core/config.go', 'LocaleAvailabilityContext'],
			['core/config.go', 'Locale'],
			['core/config.go', 'Collection'],
			['core/config.go', 'CollectionAdmin'],
			['core/config.go', 'LivePreviewConfig'],
			['core/config.go', 'Global'],
			['core/config.go', 'AuthConfig'],
			['core/config.go', 'UploadConfig'],
			['core/config.go', 'VersionConfig'],
			['core/config.go', 'AdminConfig'],
			['core/config.go', 'CollectionLabels'],
			['core/config.go', 'CollectionIndex'],
			['core/config.go', 'GlobalAdmin'],
			['core/config.go', 'DocumentLockConfig'],
			['core/config.go', 'PasswordPolicy'],
			['core/config.go', 'PasswordResetConfig'],
			['core/config.go', 'VerifyEmailConfig'],
			['core/config.go', 'PasswordResetNotification'],
			['core/config.go', 'VerifyEmailNotification'],
			['core/config.go', 'ImageSize'],
			['core/config.go', 'PreviewBreakpoint'],
			['core/auth_contract.go', 'AuthAccess'],
			['core/auth_contract.go', 'AuthContext'],
			['core/auth_contract.go', 'AuthHooks'],
			['core/auth_contract.go', 'AuthStrategy'],
			['core/auth_contract.go', 'AuthStrategyContext'],
			['core/auth_contract.go', 'AuthStrategyResult'],
			['core/access.go', 'AccessContext'],
			['core/access.go', 'FieldAccessContext'],
			['internal/httpapi/http.go', 'AuthSessionInfo'],
			['internal/httpapi/http.go', 'APIKey'],
			['internal/httpapi/http.go', 'APIKeyInfo'],
			['core/uploads.go', 'ReconcileResult'],
			['core/http.go', 'AuditEvent'],
			['core/http.go', 'RequestObservation'],
			['core/http.go', 'RequestErrorEvent'],
			['core/http.go', 'HandlerOptions'],
			['core/execute.go', 'ServerOptions'],
			['core/uploads.go', 'UpdateUploadImageInput'],
			['internal/operation/engine.go', 'Error', 'OperationError'],
			['core/computed.go', 'ComputedContext'],
			['core/local.go', 'OperationCapabilities'],
			['core/local.go', 'FieldCapabilities'],
			['core/local.go', 'JoinMutationResult'],
			['core/plugin.go', 'PluginHookContribution'],
			['core/plugin.go', 'PluginFieldValidationContext'],
			['core/plugin.go', 'PluginEndpoint'],
			['core/plugin.go', 'PluginEndpointContext'],
			['core/plugin.go', 'PluginTransport'],
			['core/plugin.go', 'PluginTransportContext'],
			['core/task.go', 'TaskContext'],
			['core/task.go', 'TaskEnqueueOptions'],
			['core/task.go', 'TaskReceipt'],
			['core/task.go', 'TaskResult'],
			['core/task.go', 'TaskError'],
			['core/task.go', 'TaskRunSummary']
		] as const;

		for (const [sourcePath, declarationName, referenceName = declarationName] of contracts) {
			const symbol = findReferenceModule('core')?.symbols.find(
				(candidate) => candidate.name === referenceName
			);
			expect(symbol, `core reference is missing ${referenceName}`).toBeDefined();
			const source = readFileSync(path.join(repositoryRoot, sourcePath), 'utf8');
			const escaped = declarationName.replaceAll(/[.*+?^${}()|[\]\\]/g, '\\$&');
			const declaration = source.match(
				new RegExp(`type ${escaped}(?:\\[[^\\]]+\\])? struct \\{([\\s\\S]*?)^\\}`, 'm')
			)?.[1];
			expect(declaration, `${sourcePath} is missing struct ${declarationName}`).toBeDefined();
			const publicFields = [...(declaration ?? '').matchAll(/^\s*([A-Z]\w*)\s+/gm)].map(
				(match) => match[1]
			);
			expect(
				symbol?.parameters.map((parameter) => parameter.name),
				`core/${symbol?.slug} member documentation drifted from ${sourcePath}`
			).toEqual(publicFields);
		}
	});

	test('covers every export from the public TypeScript entrypoints', () => {
		for (const [moduleSlug, entrypoints] of Object.entries(typescriptEntrypoints)) {
			const module = findReferenceModule(moduleSlug);
			expect(module, `source coverage references missing module ${moduleSlug}`).toBeDefined();
			if (!module) continue;

			const exports = new Set(
				entrypoints.flatMap((entrypoint) => [
					...typescriptExports(path.join(repositoryRoot, entrypoint))
				])
			);
			for (const exportedName of exports) {
				expect(
					module.symbols.some((symbol) => symbolDocumentsExport(symbol, exportedName)),
					`${moduleSlug} does not document public export ${exportedName}`
				).toBeTrue();
			}
		}
	});

	test('does not present private SDK declarations as importable standalone exports', () => {
		const sdk = findReferenceModule('sdk');
		expect(sdk).toBeDefined();
		if (!sdk) return;
		const exports = typescriptExports(path.join(repositoryRoot, typescriptEntrypoints.sdk[0]));
		for (const symbol of sdk.symbols) {
			if (symbol.name.includes('.') || symbol.name === 'Live preview constants') continue;
			expect(
				exports.has(symbol.name),
				`sdk/${symbol.slug} presents private ${symbol.name} as a public standalone export`
			).toBeTrue();
		}
	});
});

function referencedRoutes(moduleSlug: string, symbolSlug: string): string[] {
	const symbol = findReferenceSymbol(moduleSlug, symbolSlug);
	if (!symbol) throw new Error(`missing reference symbol ${moduleSlug}/${symbolSlug}`);
	return referencedTypeEntries(symbol).map((entry) => `${entry.module.slug}/${entry.symbol.slug}`);
}

function displayName(moduleSlug: string, symbolSlug: string) {
	const module = findReferenceModule(moduleSlug);
	const symbol = findReferenceSymbol(moduleSlug, symbolSlug);
	if (!module || !symbol) throw new Error(`missing reference symbol ${moduleSlug}/${symbolSlug}`);
	return referenceSymbolDisplayName(module, symbol);
}

function assertCompleteSymbol(module: ReferenceModule, symbol: ReferenceSymbol) {
	const route = `${module.slug}/${symbol.slug}`;
	for (const [field, value] of Object.entries({
		name: symbol.name,
		group: symbol.group,
		summary: symbol.summary,
		signature: symbol.signature
	})) {
		expect(value.trim(), `${route} has empty ${field} metadata`).not.toBe('');
	}
	expect(symbol.language, `${route} does not inherit its module language`).toBe(module.language);

	for (const [index, parameter] of symbol.parameters.entries()) {
		for (const [field, value] of Object.entries(parameter)) {
			expect(value.trim(), `${route} parameter ${index} has empty ${field}`).not.toBe('');
		}
	}
	if (symbol.returns) {
		expect(symbol.returns.type.trim(), `${route} has an empty return type`).not.toBe('');
		expect(symbol.returns.description.trim(), `${route} has an empty return description`).not.toBe(
			''
		);
	}
	for (const [index, paragraph] of symbol.details.entries()) {
		expect(paragraph.trim(), `${route} has an empty detail paragraph at ${index}`).not.toBe('');
	}
}

function hasUsefulDefinition(symbol: ReferenceSymbol): boolean {
	if (
		symbol.parameters.length > 0 ||
		symbol.returns ||
		symbol.details.length > 0 ||
		symbol.example
	) {
		return true;
	}
	const body = symbol.signature.match(/\{([\s\S]*)\}/)?.[1]?.trim();
	return Boolean(body && body !== '...' && !body.includes('private'));
}

function symbolDocumentsExport(symbol: ReferenceSymbol, exportedName: string): boolean {
	return (
		symbol.name === exportedName ||
		symbol.parameters.some((parameter) => parameter.name === exportedName)
	);
}

function typescriptExports(entrypoint: string, visited = new Set<string>()): Set<string> {
	const sourcePath = path.resolve(entrypoint);
	if (visited.has(sourcePath)) return new Set();
	visited.add(sourcePath);

	const source = ts.createSourceFile(
		sourcePath,
		readFileSync(sourcePath, 'utf8'),
		ts.ScriptTarget.Latest,
		true
	);
	const exported = new Set<string>();

	for (const statement of source.statements) {
		if (ts.isExportDeclaration(statement)) {
			if (statement.exportClause && ts.isNamedExports(statement.exportClause)) {
				for (const element of statement.exportClause.elements) exported.add(element.name.text);
				continue;
			}
			if (
				!statement.exportClause &&
				statement.moduleSpecifier &&
				ts.isStringLiteral(statement.moduleSpecifier) &&
				statement.moduleSpecifier.text.startsWith('.')
			) {
				const target = resolveTypeScriptModule(sourcePath, statement.moduleSpecifier.text);
				for (const name of typescriptExports(target, visited)) exported.add(name);
			}
			continue;
		}

		const modifiers = ts.canHaveModifiers(statement) ? ts.getModifiers(statement) : undefined;
		if (!modifiers?.some((modifier) => modifier.kind === ts.SyntaxKind.ExportKeyword)) continue;
		const namedStatement = statement as ts.Statement & { name?: ts.DeclarationName };
		if (namedStatement.name && ts.isIdentifier(namedStatement.name)) {
			exported.add(namedStatement.name.text);
		}
		if (ts.isVariableStatement(statement)) {
			for (const declaration of statement.declarationList.declarations) {
				if (ts.isIdentifier(declaration.name)) exported.add(declaration.name.text);
			}
		}
	}

	return exported;
}

function resolveTypeScriptModule(sourcePath: string, specifier: string): string {
	const target = path.resolve(path.dirname(sourcePath), specifier);
	const sourceTarget = target.replace(/\.(?:c|m)?js$/, '');
	for (const candidate of [
		target,
		`${target}.ts`,
		`${sourceTarget}.ts`,
		`${sourceTarget}.tsx`,
		path.join(target, 'index.ts'),
		path.join(sourceTarget, 'index.ts')
	]) {
		if (existsSync(candidate)) return candidate;
	}
	throw new Error(`cannot resolve TypeScript export ${specifier} from ${sourcePath}`);
}
