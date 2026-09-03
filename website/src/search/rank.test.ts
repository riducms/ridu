/// <reference types="bun-types" />

import { expect, test } from 'bun:test';
import { referenceModules } from '@/reference';
import type { SiteSearchEntry } from './index';
import { rankSearchCatalog } from './rank';
import { buildReferenceSearchText, searchableText } from './text';

function referenceCatalog(): SiteSearchEntry[] {
	return referenceModules.flatMap((module) =>
		module.symbols.map((symbol) => ({
			label:
				symbol.kind === 'command' || symbol.name.includes('.')
					? symbol.name
					: `${module.name}.${symbol.name}`,
			href: `/reference/${module.slug}/${symbol.slug}/`,
			type: symbol.kind,
			aliases: [symbol.name],
			searchText: buildReferenceSearchText(module, symbol)
		}))
	);
}

test('ranks exact qualified API symbols first', () => {
	const catalog = referenceCatalog();
	for (const query of ['field.Select', 'LocalAPI.Find', 'RiduClient.list']) {
		const results = rankSearchCatalog(catalog, query);
		expect(results.length, query).toBeGreaterThan(0);
		expect(results[0].entry.label, query).toBe(query);
	}
});

test('keeps commands and environment variables searchable', () => {
	const catalog = referenceCatalog();
	expect(rankSearchCatalog(catalog, 'ridu migrate verify')[0]?.entry.label).toBe(
		'ridu migrate verify'
	);
	expect(rankSearchCatalog(catalog, 'DATABASE_URL').length).toBeGreaterThan(0);
});

test('prefers task pages for a human query while canonical queries prefer API symbols', () => {
	const entries: SiteSearchEntry[] = [
		{
			label: 'Select field',
			href: '/docs/fields/select/',
			type: 'Docs',
			searchText: searchableText('Select field', 'configure a choice field')
		},
		{
			label: 'field.Select',
			href: '/reference/field/select/',
			type: 'function',
			searchText: searchableText('field.Select', 'Select field')
		}
	];
	expect(rankSearchCatalog(entries, 'Select field')[0].entry.type).toBe('Docs');
	expect(rankSearchCatalog(entries, 'field.Select')[0].entry.type).toBe('function');
});

test('normalizes camelCase, acronyms, punctuation, and import paths without losing exact aliases', () => {
	const text = searchableText(
		'RiduClient.list',
		'LocalAPI.Find',
		'@riducms/sdk',
		'/api/posts',
		'DATABASE_URL'
	);
	expect(text).toContain('riduclient.list');
	expect(text).toContain('ridu client list');
	expect(text).toContain('local api find');
	expect(text).toContain('@riducms/sdk');
	expect(text).toContain('/api/posts');
	expect(text).toContain('database_url');
});
