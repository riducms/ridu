import type { SiteSearchEntry } from './index';

export type SearchCategory = 'all' | 'docs' | 'guides' | 'reference';

export interface RankedSearchEntry {
	entry: SiteSearchEntry;
	index: number;
	score: number;
}

export const normalizeSearchQuery = (value: string): string =>
	value.normalize('NFKC').trim().toLocaleLowerCase().replaceAll(/\s+/g, ' ');

export const searchTermsFor = (query: string): string[] => [
	...new Set(normalizeSearchQuery(query).split(' ').filter(Boolean))
];

export const searchCategoryFor = (entry: SiteSearchEntry): Exclude<SearchCategory, 'all'> => {
	if (entry.type.startsWith('Docs')) return 'docs';
	if (entry.type.startsWith('Guide')) return 'guides';
	return 'reference';
};

const isCanonicalSymbolQuery = (query: string): boolean =>
	/[.#/]/.test(query) ||
	/(?:^|\s)(?:field|riduclient|localapi|query|schema|store)[A-Z]/.test(query);

const isTaskQuery = (query: string): boolean =>
	!isCanonicalSymbolQuery(query) && /[a-z]/.test(query) && !/^ridu(?:\s|$)/.test(query);

function fieldScore(
	value: string | undefined,
	query: string,
	terms: string[],
	weight: number
): number {
	if (!value) return 0;
	const normalized = normalizeSearchQuery(value);
	let score = 0;
	if (normalized === query) score += weight * 8;
	else if (normalized.startsWith(query)) score += weight * 4;
	else if (normalized.includes(query)) score += weight * 2;
	for (const term of terms) {
		if (normalized === term) score += weight * 2;
		else if (normalized.startsWith(term)) score += weight;
		else if (normalized.includes(term)) score += Math.max(1, Math.floor(weight / 2));
	}
	return score;
}

export function rankSearchCatalog(
	catalog: SiteSearchEntry[],
	rawQuery: string,
	category: SearchCategory = 'all'
): RankedSearchEntry[] {
	const query = normalizeSearchQuery(rawQuery);
	const terms = searchTermsFor(query);
	if (terms.length === 0) return [];
	const canonicalSymbolQuery = isCanonicalSymbolQuery(rawQuery.trim());
	const taskQuery = isTaskQuery(rawQuery.trim());

	return catalog
		.map((entry, index): RankedSearchEntry | undefined => {
			if (category !== 'all' && searchCategoryFor(entry) !== category) return;
			if (!terms.every((term) => entry.searchText.includes(term))) return;

			const aliases = entry.aliases ?? [];
			let score = fieldScore(entry.label, query, terms, 160);
			score += Math.max(0, ...aliases.map((alias) => fieldScore(alias, query, terms, 140)));
			score += fieldScore(entry.href, query, terms, 20);
			score += fieldScore(entry.breadcrumb?.join(' '), query, terms, 18);
			score += fieldScore(entry.snippet, query, terms, 8);

			const searchCategory = searchCategoryFor(entry);
			if (canonicalSymbolQuery && searchCategory === 'reference') score += 420;
			if (taskQuery) {
				if (entry.type === 'Docs' || entry.type === 'Guide') score += 110;
				else if (searchCategory === 'docs' || searchCategory === 'guides') score += 45;
			}
			if (entry.type === 'Docs' || entry.type === 'Guide' || entry.type === 'Module') score += 4;

			return { entry, index, score };
		})
		.filter((entry): entry is RankedSearchEntry => Boolean(entry))
		.sort(
			(left, right) =>
				right.score - left.score ||
				left.entry.label.localeCompare(right.entry.label) ||
				left.index - right.index
		);
}
