import { getCollection, render, type CollectionEntry, type RenderResult } from 'astro:content';
import { extractMarkdownSearchSections } from '@/search/markdown';

export type DocumentationArea = 'docs' | 'guides';
export type DocProduct =
	'core' | 'data' | 'admin' | 'sdk' | 'cli' | 'adapters' | 'plugins' | 'guides';
export type DocumentationEntry = CollectionEntry<'docs'> | CollectionEntry<'guides'>;
export type DocumentationLayout = 'standard' | 'wide';
export type DocumentationSection =
	| 'Get started'
	| 'Model content'
	| 'Work with data'
	| 'Admin & workflows'
	| 'Extend Ridu'
	| 'Develop & operate';

export const documentationSections: ReadonlyArray<{
	label: DocumentationSection;
	order: number;
}> = [
	{ label: 'Get started', order: 10 },
	{ label: 'Model content', order: 20 },
	{ label: 'Work with data', order: 30 },
	{ label: 'Admin & workflows', order: 40 },
	{ label: 'Extend Ridu', order: 50 },
	{ label: 'Develop & operate', order: 60 }
];

export interface DocumentationAvailability {
	status: 'available' | 'limited' | 'experimental' | 'planned';
	label: string;
	description: string;
	anchor?: string;
}

export interface DocumentationPage {
	area: DocumentationArea;
	slug: string;
	href: string;
	product: DocProduct;
	section: DocumentationSection;
	parent?: string;
	navigationGroup?: string;
	navigationTitle: string;
	eyebrow: string;
	title: string;
	description: string;
	availability?: DocumentationAvailability;
	aliases: string[];
	capabilityIds: string[];
	relatedSymbolIds: string[];
	layout: DocumentationLayout;
	order: number;
	entry: DocumentationEntry;
}

export interface DocumentationNavigationItem {
	id: string;
	label: string;
	href?: string;
	children: DocumentationNavigationItem[];
}

export interface DocumentationNavigationSection {
	label: DocumentationSection;
	items: DocumentationNavigationItem[];
}

export interface DocumentationPaginationPage {
	title: string;
	href: string;
}

export interface DocumentationSearchEntry {
	label: string;
	href: string;
	type: 'Docs' | 'Docs section' | 'Guide' | 'Guide section';
	product: DocProduct;
	description: string;
	keywords: string[];
	searchText: string;
	breadcrumb: string[];
	snippet: string;
}

export interface AuthoredDocumentationPage {
	area: DocumentationArea;
	slug: string;
	href: string;
	product: DocProduct;
	section: DocumentationSection;
	parent?: string;
	layout: DocumentationLayout;
	eyebrow: string;
	title: string;
	description: string;
	includeInNavigation: boolean;
	navigation: {
		order: number;
		title: string;
	};
	sections: Array<{ id: string; label: string }>;
}

export const authoredDocumentationPages: AuthoredDocumentationPage[] = [
	{
		area: 'docs',
		slug: '',
		href: '/docs/',
		product: 'core',
		section: 'Get started',
		layout: 'standard',
		eyebrow: 'Documentation',
		title: 'Ridu documentation',
		description:
			'Choose a practical path through setup, content modelling, data access, the admin, extensions, and production.',
		includeInNavigation: true,
		navigation: {
			order: 0,
			title: 'Documentation overview'
		},
		sections: [
			{ id: 'start-building', label: 'Start building' },
			{ id: 'choose-a-topic', label: 'Choose a topic' }
		]
	},
	{
		area: 'guides',
		slug: '',
		href: '/guides/',
		product: 'guides',
		section: 'Get started',
		layout: 'standard',
		eyebrow: 'Guides',
		title: 'Ridu guides',
		description: 'Task-focused paths for adopting Ridu and extending an existing application.',
		includeInNavigation: false,
		navigation: {
			order: 90,
			title: 'Guides'
		},
		sections: [{ id: 'all-guides', label: 'All guides' }]
	},
	{
		area: 'guides',
		slug: 'project-structure',
		href: '/guides/project-structure/',
		product: 'guides',
		section: 'Get started',
		layout: 'wide',
		eyebrow: 'Guide',
		title: 'Project structure',
		description:
			'Understand the few files you edit, the output Ridu generates, and what can stay out of your way.',
		includeInNavigation: true,
		navigation: {
			order: 80,
			title: 'Project structure'
		},
		sections: [
			{ id: 'project-map', label: 'Project map' },
			{ id: 'first-edit', label: 'Add a collection' },
			{ id: 'generation-flow', label: 'Generated output' },
			{ id: 'ownership', label: 'Editing rules' }
		]
	}
];

export interface DocumentationProduct {
	key: DocProduct;
	label: string;
	href: string;
}

export const documentationProducts: DocumentationProduct[] = [
	{ key: 'core', label: 'Core', href: '/docs/getting-started/' },
	{ key: 'data', label: 'Data & APIs', href: '/docs/data-access/' },
	{ key: 'admin', label: 'Admin', href: '/docs/admin/' },
	{ key: 'sdk', label: 'SDK', href: '/docs/typescript-sdk/' },
	{ key: 'cli', label: 'CLI', href: '/docs/cli/' },
	{ key: 'adapters', label: 'Adapters', href: '/docs/adapters/' },
	{ key: 'plugins', label: 'Plugins', href: '/docs/plugins/' },
	{ key: 'guides', label: 'Guides', href: '/guides/project-structure/' }
];

export const docsProductNavigation = documentationProducts;

export function getDocumentationProduct(product: DocProduct): DocumentationProduct {
	return (
		documentationProducts.find((candidate) => candidate.key === product) ?? documentationProducts[0]
	);
}

interface ResolvedDocumentationNavigation {
	section: DocumentationSection;
	parent?: string;
	group?: string;
	order: number;
	title: string;
}

function productForSection(section: DocumentationSection): DocProduct {
	if (section === 'Work with data') return 'data';
	if (section === 'Admin & workflows') return 'admin';
	if (section === 'Extend Ridu') return 'plugins';
	if (section === 'Develop & operate') return 'core';
	return 'core';
}

function resolveDocumentationNavigation(
	entry: DocumentationEntry
): ResolvedDocumentationNavigation {
	const navigation = entry.data.navigation;
	return {
		section: navigation.section,
		parent: navigation.parent,
		group: navigation.group,
		order: navigation.order,
		title: navigation.title
	};
}

export function hrefForDocumentationPage(page: Pick<DocumentationPage, 'area' | 'slug'>): string {
	return `/${page.area}/${page.slug}/`;
}

export function toDocumentationPage(entry: DocumentationEntry): DocumentationPage {
	const area = entry.collection as DocumentationArea;
	const navigation = resolveDocumentationNavigation(entry);
	const page = {
		area,
		slug: entry.id,
		product: entry.data.product ?? productForSection(navigation.section),
		section: navigation.section,
		parent: navigation.parent,
		navigationGroup: navigation.group,
		navigationTitle: navigation.title,
		eyebrow: entry.data.eyebrow ?? navigation.section,
		title: entry.data.title,
		description: entry.data.description,
		availability: entry.data.availability,
		aliases: entry.data.aliases,
		capabilityIds: [...entry.data.capabilities, ...entry.data.capabilityIds],
		relatedSymbolIds: [...entry.data.symbols, ...entry.data.relatedSymbolIds],
		layout: entry.data.layout,
		order: navigation.order,
		entry
	};

	return { ...page, href: hrefForDocumentationPage(page) };
}

export async function getDocumentationPages(): Promise<DocumentationPage[]> {
	const [docs, guides] = await Promise.all([getCollection('docs'), getCollection('guides')]);
	return [...docs, ...guides]
		.map((entry) => toDocumentationPage(entry))
		.sort(
			(left, right) =>
				(left.area === right.area ? 0 : left.area === 'docs' ? -1 : 1) ||
				left.order - right.order ||
				left.slug.localeCompare(right.slug)
		);
}

export async function getDocumentationPage(
	area: DocumentationArea,
	slug: string
): Promise<DocumentationPage | undefined> {
	const pages = await getDocumentationPages();
	return pages.find((page) => page.area === area && page.slug === slug);
}

export function getAuthoredDocumentationPage(
	area: DocumentationArea,
	slug: string
): AuthoredDocumentationPage | undefined {
	return authoredDocumentationPages.find((page) => page.area === area && page.slug === slug);
}

interface NavigationCandidate extends DocumentationNavigationItem {
	section: DocumentationSection;
	parent?: string;
	group?: string;
	order: number;
}

function compareNavigationItems(left: NavigationCandidate, right: NavigationCandidate): number {
	return left.order - right.order || left.label.localeCompare(right.label);
}

function publicNavigationItem(candidate: NavigationCandidate): DocumentationNavigationItem {
	return {
		id: candidate.id,
		label: candidate.label,
		href: candidate.href,
		children: candidate.children.map((child) => publicNavigationItem(child as NavigationCandidate))
	};
}

export async function getDocumentationNavigation(): Promise<DocumentationNavigationSection[]> {
	const pages = await getDocumentationPages();
	const candidates: NavigationCandidate[] = [
		...pages.map((page) => ({
			id: page.slug,
			section: page.section,
			parent: page.parent,
			group: page.navigationGroup,
			order: page.order,
			label: page.navigationTitle,
			href: page.href,
			children: []
		})),
		...authoredDocumentationPages
			.filter((page) => page.includeInNavigation)
			.map((page) => ({
				id: page.slug || `${page.area}-home`,
				section: page.section,
				parent: page.parent,
				order: page.navigation.order,
				label: page.navigation.title,
				href: page.href,
				children: []
			}))
	];
	const groupOrder: Record<string, number> = {
		'Scalar & choice': 10,
		Structured: 20,
		'Relationship & media': 30,
		Layout: 40,
		'Computed & plugin': 50
	};
	const virtualGroups = new Map<string, NavigationCandidate>();
	for (const candidate of candidates) {
		if (!candidate.group || !candidate.parent) continue;
		const groupKey = `${candidate.section}:${candidate.parent}:${candidate.group}`;
		let group = virtualGroups.get(groupKey);
		if (!group) {
			group = {
				id: `group:${groupKey}`,
				section: candidate.section,
				parent: candidate.parent,
				order: groupOrder[candidate.group] ?? candidate.order,
				label: candidate.group,
				children: []
			};
			virtualGroups.set(groupKey, group);
		}
		candidate.parent = group.id;
	}
	candidates.push(...virtualGroups.values());

	return documentationSections.map(({ label }) => {
		const sectionCandidates = candidates.filter((candidate) => candidate.section === label);
		const byID = new Map(sectionCandidates.map((candidate) => [candidate.id, candidate]));
		const roots: NavigationCandidate[] = [];

		for (const candidate of sectionCandidates) {
			const parent = candidate.parent
				? (byID.get(candidate.parent) ??
					sectionCandidates.find((item) => item.id.split('/').at(-1) === candidate.parent))
				: undefined;
			if (parent && parent !== candidate) parent.children.push(candidate);
			else roots.push(candidate);
		}

		const sortTree = (items: NavigationCandidate[]) => {
			items.sort(compareNavigationItems);
			for (const item of items) sortTree(item.children as NavigationCandidate[]);
		};
		sortTree(roots);

		return { label, items: roots.map(publicNavigationItem) };
	});
}

function flattenNavigationItems(
	items: DocumentationNavigationItem[]
): DocumentationNavigationItem[] {
	return items.flatMap((item) => [item, ...flattenNavigationItems(item.children)]);
}

export async function getAdjacentDocumentationLocation(
	page: Pick<DocumentationPage, 'area' | 'slug'>
): Promise<{
	previous?: DocumentationPaginationPage;
	next?: DocumentationPaginationPage;
}> {
	const navigation = await getDocumentationNavigation();
	const pages = navigation
		.flatMap((section) => flattenNavigationItems(section.items))
		.filter(
			(candidate): candidate is DocumentationNavigationItem & { href: string } =>
				typeof candidate.href === 'string'
		);
	const href = `/${page.area}/${page.slug ? `${page.slug}/` : ''}`;
	const index = pages.findIndex((candidate) => candidate.href === href);
	return {
		previous:
			index > 0 ? { title: pages[index - 1].label, href: pages[index - 1].href } : undefined,
		next:
			index >= 0 && index < pages.length - 1
				? { title: pages[index + 1].label, href: pages[index + 1].href }
				: undefined
	};
}

export function getAdjacentDocumentationPage(page: DocumentationPage) {
	return getAdjacentDocumentationLocation(page);
}

function searchText(parts: Array<string | number>): string {
	return parts.join(' ').toLowerCase();
}

async function markdownSearchEntries(
	page: DocumentationPage,
	rendered: RenderResult
): Promise<DocumentationSearchEntry[]> {
	const pageType: DocumentationSearchEntry['type'] = page.area === 'guides' ? 'Guide' : 'Docs';
	const sectionType: DocumentationSearchEntry['type'] =
		page.area === 'guides' ? 'Guide section' : 'Docs section';
	const shared = [
		page.title,
		page.slug,
		page.description,
		page.eyebrow,
		page.product,
		page.area,
		page.availability?.status ?? '',
		page.availability?.label ?? '',
		page.availability?.description ?? '',
		...page.aliases
	];
	const extracted = extractMarkdownSearchSections(page.entry.body ?? '', page.title);
	const bodyText = extracted.flatMap((section) => [
		section.heading,
		section.text,
		...section.identifiers
	]);
	const renderedHeadings = rendered.headings.filter(
		(heading) => heading.depth >= 2 && heading.depth <= 4
	);
	const extractedSections = extracted.filter((section) => section.depth >= 2 && section.depth <= 4);

	return [
		{
			label: page.title,
			href: page.href,
			type: pageType,
			product: page.product,
			description: page.description,
			keywords: [
				page.slug,
				page.eyebrow,
				page.product,
				page.area,
				page.section,
				...page.aliases,
				...page.capabilityIds,
				...page.relatedSymbolIds
			],
			searchText: searchText([...shared, pageType, page.section, ...bodyText]),
			breadcrumb: [page.section, page.title],
			snippet: page.description
		},
		...extractedSections.map((section, index) => {
			const renderedHeading = renderedHeadings[index];
			const id = section.id ?? renderedHeading?.slug;
			if (!id) throw new Error(`${page.href} has a search section without a resolved anchor`);
			return {
				label: section.heading,
				href: `${page.href}#${id}`,
				type: sectionType,
				product: page.product,
				description: section.snippet || page.description,
				keywords: [
					section.heading,
					...section.context,
					...section.identifiers,
					page.slug,
					page.eyebrow,
					page.product,
					page.area,
					page.section,
					...page.aliases,
					...page.capabilityIds,
					...page.relatedSymbolIds
				],
				searchText: searchText([
					section.heading,
					section.text,
					...section.context,
					...section.identifiers,
					...shared,
					sectionType
				]),
				breadcrumb: [page.section, ...section.context],
				snippet: section.snippet
			};
		})
	];
}

async function createDocumentationSearchEntries(): Promise<DocumentationSearchEntry[]> {
	const pages = await getDocumentationPages();
	const rendered = await Promise.all(pages.map((page) => render(page.entry)));
	const markdownEntries = await Promise.all(
		pages.map((page, index) => markdownSearchEntries(page, rendered[index]))
	);
	const authoredEntries = authoredDocumentationPages.flatMap((page) => {
		const shared = [page.title, page.slug, page.description, page.eyebrow, page.product, page.area];
		const pageType: DocumentationSearchEntry['type'] = page.area === 'guides' ? 'Guide' : 'Docs';
		const sectionType: DocumentationSearchEntry['type'] =
			page.area === 'guides' ? 'Guide section' : 'Docs section';
		return [
			{
				label: page.title,
				href: page.href,
				type: pageType,
				product: page.product,
				description: page.description,
				keywords: [page.slug, page.eyebrow, page.product, page.area, page.section],
				searchText: searchText([...shared, pageType, page.section]),
				breadcrumb: [page.section, page.title],
				snippet: page.description
			},
			...page.sections.map((section) => ({
				label: section.label,
				href: `${page.href}#${section.id}`,
				type: sectionType,
				product: page.product,
				description: page.description,
				keywords: [section.label, page.slug, page.eyebrow, page.product, page.area, page.section],
				searchText: searchText([section.label, ...shared, sectionType, page.section]),
				breadcrumb: [page.section, page.title, section.label],
				snippet: page.description
			}))
		];
	});

	return [...authoredEntries, ...markdownEntries.flat()];
}

let productionSearchEntries: Promise<DocumentationSearchEntry[]> | undefined;

export function getDocumentationSearchEntries(): Promise<DocumentationSearchEntry[]> {
	if (import.meta.env.DEV) return createDocumentationSearchEntries();
	productionSearchEntries ??= createDocumentationSearchEntries();
	return productionSearchEntries;
}
