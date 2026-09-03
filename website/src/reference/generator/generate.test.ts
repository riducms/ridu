import { describe, expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';
import path from 'node:path';
import { gzipSync } from 'node:zlib';
import registryFile from '../authoring/module-registry.json';
import routeLockFile from '../authoring/route-lock.json';
import catalogFile from '../generated/catalog.json';
import type { ReferenceModule } from '../types';
import { REFERENCE_CATALOG_MAX_BYTES, REFERENCE_CATALOG_MAX_GZIP_BYTES } from './generate';

interface CatalogFixture {
	schemaVersion: 1;
	modules: ReferenceModule[];
}

interface RegistryFixture {
	schemaVersion: 1;
	modules: Array<{ slug: string }>;
}

interface RouteLockFixture {
	schemaVersion: 1;
	routes: Record<string, { module: string; symbol: string }>;
}

const catalog = catalogFile as CatalogFixture;
const registry = registryFile as RegistryFixture;
const routeLock = routeLockFile as RouteLockFixture;

describe('generated reference catalog', () => {
	test('covers the reviewed modules and previously escaped declarations', () => {
		expect(catalog.modules.map((module) => module.slug)).toEqual(
			registry.modules.map((module) => module.slug)
		);
		const ids = new Set(
			catalog.modules.flatMap((module) => module.symbols.map((symbol) => symbol.id))
		);
		for (const id of [
			'go:github.com/riducms/ridu/adapters/postgres#ProjectMigrations',
			'go:github.com/riducms/ridu/migration/payload#ID',
			'go:github.com/riducms/ridu/field#Select',
			'ts:@riducms/sdk#RiduClient.list',
			'ts:@riducms/cli/run#runRidu'
		]) {
			expect(ids.has(id), id).toBeTrue();
		}
	});

	test('locks every generated route to its stable declaration identity', () => {
		for (const module of catalog.modules) {
			for (const symbol of module.symbols) {
				expect(routeLock.routes[symbol.id]).toEqual({ module: module.slug, symbol: symbol.slug });
			}
		}
	});

	test('keeps generated source links repository-owned and portable', () => {
		for (const symbol of catalog.modules.flatMap((module) => module.symbols)) {
			if (!symbol.source) continue;
			expect(path.isAbsolute(symbol.source.path), symbol.id).toBeFalse();
			expect(symbol.source.path.split('/')).not.toContain('node_modules');
		}
	});

	test('stays within the checked static catalog budget', () => {
		const serialized = readFileSync(new URL('../generated/catalog.json', import.meta.url));
		expect(serialized.byteLength).toBeLessThanOrEqual(REFERENCE_CATALOG_MAX_BYTES);
		expect(gzipSync(serialized, { level: 9 }).byteLength).toBeLessThanOrEqual(
			REFERENCE_CATALOG_MAX_GZIP_BYTES
		);
	});
});
