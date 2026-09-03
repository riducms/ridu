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

export const searchableText = (...values: unknown[]): string =>
	[
		...new Set(
			values
				.flat(Infinity)
				.filter((value): value is string => typeof value === 'string' && value.length > 0)
				.flatMap(searchVariants)
		)
	].join(' ');

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
		symbol.returns?.type,
		symbol.returns?.description,
		'reference api'
	);
