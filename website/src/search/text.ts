import type { ReferenceModule, ReferenceSymbol } from '@/reference';

function searchVariants(value: string): string[] {
	const normalized = value.normalize('NFKC').toLocaleLowerCase();
	const identifierWords = value
		.replaceAll(/([a-z0-9])([A-Z])/g, '$1 $2')
		.replaceAll(/([A-Z]+)([A-Z][a-z])/g, '$1 $2')
		.replaceAll(/[^\p{L}\p{N}_@-]+/gu, ' ')
		.toLocaleLowerCase()
		.replaceAll(/\s+/g, ' ')
		.trim();
	const punctuationWords = normalized
		.replaceAll(/[^\p{L}\p{N}_@-]+/gu, ' ')
		.replaceAll(/\s+/g, ' ')
		.trim();
	return [...new Set([normalized, identifierWords, punctuationWords].filter(Boolean))];
}

export function searchableText(...values: unknown[]): string {
	const variants = [
		...new Set(
			values
				.flat(Infinity)
				.filter((value): value is string => typeof value === 'string' && value.length > 0)
				.flatMap(searchVariants)
		)
	].sort((left, right) => right.length - left.length);
	// Search uses substring membership, so a fragment already contained in a
	// longer variant adds no matches. Keep every original phrase and identifier
	// while avoiding repeated aliases, parameter types, and description fragments.
	const retained: string[] = [];
	for (const variant of variants) {
		if (!retained.some((existing) => existing.includes(variant))) retained.push(variant);
	}
	// Overlapping whole-word suffixes/prefixes can share their text without
	// dropping any original phrase or introducing a new searchable token.
	const compact: string[] = [];
	for (const variant of retained) {
		let bestIndex = -1;
		let bestOverlap = 0;
		let bestValue = variant;
		for (const [index, existing] of compact.entries()) {
			for (const [left, right] of [
				[existing, variant],
				[variant, existing]
			] as const) {
				const overlap = wordOverlap(left, right);
				if (overlap > bestOverlap) {
					bestIndex = index;
					bestOverlap = overlap;
					bestValue = left + right.slice(overlap);
				}
			}
		}
		if (bestIndex < 0) compact.push(variant);
		else compact[bestIndex] = bestValue;
	}
	return compact.join(' ');
}

function wordOverlap(left: string, right: string): number {
	for (let start = Math.max(0, left.length - right.length); start < left.length; start++) {
		if (start > 0 && !/\s/.test(left.charAt(start - 1))) continue;
		const length = left.length - start;
		if (length < right.length && !/\s/.test(right.charAt(length))) continue;
		if (left.slice(start) === right.slice(0, length)) return length;
	}
	return 0;
}

export const buildReferenceSearchText = (
	module: ReferenceModule,
	symbol: ReferenceSymbol
): string =>
	searchableText(
		module.name,
		module.slug,
		module.packageName,
		symbol.name.includes('.') ? symbol.name : `${module.name}.${symbol.name}`,
		`${module.packageName}#${symbol.name}`,
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
		symbol.returns?.description,
		'reference api'
	);
