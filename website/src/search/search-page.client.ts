import type { SiteSearchEntry } from './index';
import {
	normalizeSearchQuery,
	rankSearchCatalog,
	searchCategoryFor,
	type SearchCategory
} from './rank';

const categories: SearchCategory[] = ['all', 'docs', 'guides', 'reference'];

function isSearchEntry(value: unknown): value is SiteSearchEntry {
	if (!value || typeof value !== 'object') return false;
	const entry = value as Partial<SiteSearchEntry>;
	return (
		typeof entry.label === 'string' &&
		typeof entry.href === 'string' &&
		typeof entry.type === 'string' &&
		typeof entry.searchText === 'string'
	);
}

function resultElement(entry: SiteSearchEntry): HTMLAnchorElement {
	const link = document.createElement('a');
	link.className = 'full-search-result';
	link.href = entry.href;
	link.dataset.category = searchCategoryFor(entry);

	const heading = document.createElement('span');
	heading.className = 'full-search-result-heading';
	const label = document.createElement('strong');
	label.textContent = entry.label;
	const type = document.createElement('small');
	type.textContent = entry.type;
	heading.append(label, type);

	const breadcrumb = document.createElement('span');
	breadcrumb.className = 'full-search-result-breadcrumb';
	breadcrumb.textContent = entry.breadcrumb?.length
		? entry.breadcrumb.join(' › ')
		: decodeURIComponent(entry.href);
	link.append(heading, breadcrumb);

	if (entry.snippet) {
		const snippet = document.createElement('span');
		snippet.className = 'full-search-result-snippet';
		snippet.textContent = entry.snippet;
		link.append(snippet);
	}

	const route = document.createElement('span');
	route.className = 'full-search-result-route';
	route.textContent = decodeURIComponent(entry.href);
	link.append(route);
	return link;
}

export function initializeSearchPage(root: HTMLElement): void {
	if (root.dataset.fullSearchReady !== undefined) return;
	const form = root.querySelector<HTMLFormElement>('[data-full-search-form]');
	const input = root.querySelector<HTMLInputElement>('[data-full-search-input]');
	const results = root.querySelector<HTMLElement>('[data-full-search-results]');
	const status = root.querySelector<HTMLElement>('[data-full-search-status]');
	const filters = [...root.querySelectorAll<HTMLButtonElement>('[data-full-search-filter]')];
	if (!form || !input || !results || !status) return;
	root.dataset.fullSearchReady = '';

	const initialURL = new URL(window.location.href);
	const requestedCategory = initialURL.searchParams.get('category') as SearchCategory | null;
	let activeCategory: SearchCategory =
		requestedCategory && categories.includes(requestedCategory) ? requestedCategory : 'all';
	input.value = initialURL.searchParams.get('q') ?? '';
	let catalog: SiteSearchEntry[] = [];

	const updateFilters = () => {
		for (const filter of filters) {
			filter.setAttribute(
				'aria-pressed',
				String(filter.dataset.fullSearchFilter === activeCategory)
			);
		}
	};

	const updateURL = () => {
		const url = new URL(window.location.href);
		const query = input.value.trim();
		if (query) url.searchParams.set('q', query);
		else url.searchParams.delete('q');
		if (activeCategory === 'all') url.searchParams.delete('category');
		else url.searchParams.set('category', activeCategory);
		history.replaceState(null, '', `${url.pathname}${url.search}`);
	};

	const render = () => {
		const query = normalizeSearchQuery(input.value);
		updateURL();
		results.replaceChildren();
		if (!query) {
			status.textContent = 'Enter a task, command, prose term, or exact API symbol.';
			return;
		}

		const ranked = rankSearchCatalog(catalog, query, activeCategory);
		status.textContent = `${ranked.length.toLocaleString()} result${ranked.length === 1 ? '' : 's'} for “${input.value.trim()}”`;
		const fragment = document.createDocumentFragment();
		for (const { entry } of ranked) fragment.append(resultElement(entry));
		results.append(fragment);
	};

	form.addEventListener('submit', (event) => {
		event.preventDefault();
		render();
	});
	input.addEventListener('input', render);
	for (const filter of filters) {
		filter.addEventListener('click', () => {
			const category = filter.dataset.fullSearchFilter as SearchCategory;
			if (!categories.includes(category)) return;
			activeCategory = category;
			updateFilters();
			render();
		});
	}

	updateFilters();
	status.textContent = 'Loading the local documentation index…';
	fetch('/search-index.json')
		.then(async (response) => {
			if (!response.ok) throw new Error(`Search index returned ${response.status}`);
			const data: unknown = await response.json();
			if (!Array.isArray(data) || !data.every(isSearchEntry))
				throw new Error('Invalid search index');
			catalog = data;
			render();
		})
		.catch(() => {
			status.textContent = 'The search index could not be loaded. Refresh this page to try again.';
		});
}
