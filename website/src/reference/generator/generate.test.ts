import { describe, expect, test } from 'bun:test';
import path from 'node:path';
import registryFile from '../authoring/module-registry.json';
import catalogFile from '../generated/catalog.json';
import type { ReferenceModule } from '../types';

interface CatalogFixture {
	schemaVersion: 1;
	modules: ReferenceModule[];
}

interface RegistryFixture {
	schemaVersion: 1;
	modules: Array<{ slug: string }>;
}

const catalog = catalogFile as CatalogFixture;
const registry = registryFile as RegistryFixture;

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

	test('derives routes from names and separates names that share one by kind', () => {
		const slugs = new Map(
			catalog.modules.flatMap((module) => module.symbols.map((symbol) => [symbol.id, symbol.slug]))
		);
		expect(slugs.get('ts:@riducms/protocol#ErrorPayload')).toBe('error-payload-interface');
		expect(slugs.get('ts:@riducms/protocol#errorPayload')).toBe('error-payload-function');
		expect(slugs.get('ts:@riducms/sdk#RiduClient.uploadFromURL')).toBe(
			'ridu-client-upload-from-url-method'
		);
	});

	test('keeps generated source links repository-owned and portable', () => {
		for (const symbol of catalog.modules.flatMap((module) => module.symbols)) {
			if (!symbol.source) continue;
			expect(path.isAbsolute(symbol.source.path), symbol.id).toBeFalse();
			expect(symbol.source.path.split('/')).not.toContain('node_modules');
		}
	});
});
