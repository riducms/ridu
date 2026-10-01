/// <reference types="bun-types" />

import { expect, test } from 'bun:test';
import { referenceModules } from '@/reference';
import { normalizeSearchQuery } from './rank';
import { buildReferenceSearchText } from './text';

test('compacted reference records retain every declaration and authored search fragment', () => {
	const records = referenceModules.flatMap((module) =>
		module.symbols.map((symbol) => ({
			id: symbol.id,
			text: buildReferenceSearchText(module, symbol),
			fragments: [
				module.name,
				module.slug,
				module.packageName,
				symbol.name,
				symbol.slug,
				symbol.aliases,
				symbol.summary,
				symbol.details,
				symbol.overloads,
				symbol.relatedDocs,
				symbol.relatedSymbols,
				symbol.source?.path,
				symbol.group,
				symbol.kind,
				symbol.signature,
				symbol.parameters.flatMap((parameter) => [
					parameter.name,
					parameter.type,
					parameter.description
				]),
				symbol.optionsLabel,
				(symbol.options ?? []).flatMap((option) => [option.name, option.type, option.description]),
				symbol.returns?.type,
				symbol.returns?.description
			]
				.flat(Infinity)
				.filter((value): value is string => typeof value === 'string')
		}))
	);
	expect(new Set(records.map((record) => record.id)).size).toBe(
		referenceModules.reduce((total, module) => total + module.symbols.length, 0)
	);
	for (const record of records) {
		for (const fragment of record.fragments) {
			expect(record.text, `${record.id}: ${fragment}`).toContain(normalizeSearchQuery(fragment));
		}
	}
});
