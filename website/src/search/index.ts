import { getDocumentationSearchEntries } from '@/config/documentation';
import { referenceModules } from '@/reference';
import { buildReferenceSearchText, searchableText } from './text';

export { buildReferenceSearchText } from './text';

export interface SiteSearchEntry {
	label: string;
	href: string;
	type: string;
	searchText: string;
	aliases?: string[];
	breadcrumb?: string[];
	snippet?: string;
}

/** Build the single static search catalog shared by every page. */
export async function buildSiteSearchIndex(): Promise<SiteSearchEntry[]> {
	const documentation = await getDocumentationSearchEntries();
	const entries: SiteSearchEntry[] = documentation.map((entry) => ({
		label: entry.label,
		href: entry.href,
		type: entry.type,
		aliases: entry.keywords,
		breadcrumb:
			'breadcrumb' in entry && Array.isArray(entry.breadcrumb) ? entry.breadcrumb : undefined,
		snippet:
			'snippet' in entry && typeof entry.snippet === 'string' ? entry.snippet : entry.description,
		searchText: searchableText(
			entry.label,
			entry.href,
			entry.type,
			entry.product,
			entry.description,
			entry.keywords,
			entry.searchText
		)
	}));

	for (const module of referenceModules) {
		entries.push({
			label: module.name,
			href: `/reference/${module.slug}/`,
			type: 'Module',
			aliases: [module.slug, module.packageName],
			breadcrumb: ['API reference', module.group, module.name],
			snippet: module.summary,
			searchText: searchableText(
				module.name,
				module.slug,
				module.packageName,
				module.summary,
				module.group,
				'module reference'
			)
		});

		for (const symbol of module.symbols) {
			entries.push({
				label:
					symbol.kind === 'command' || symbol.name.includes('.')
						? symbol.name
						: `${module.name}.${symbol.name}`,
				href: `/reference/${module.slug}/${symbol.slug}/`,
				type: symbol.kind,
				aliases: [symbol.name, symbol.slug, `${module.packageName}#${symbol.name}`],
				breadcrumb: ['API reference', module.name, symbol.group],
				snippet: symbol.summary,
				searchText: buildReferenceSearchText(module, symbol)
			});
		}
	}

	return entries;
}
