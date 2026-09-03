import type { SiteSearchEntry } from '@/search';
import {
	normalizeSearchQuery,
	rankSearchCatalog,
	searchCategoryFor,
	searchTermsFor,
	type SearchCategory
} from './rank';

interface SearchElements {
	dialog: HTMLDialogElement;
	input: HTMLInputElement;
	form: HTMLFormElement;
	results: HTMLElement;
	viewport: HTMLElement;
	empty: HTMLElement;
	emptyDetail: HTMLElement;
	status: HTMLElement;
	statusTitle: HTMLElement;
	statusDetail: HTMLElement;
	announcer: HTMLElement;
	summary: HTMLElement;
	allResults: HTMLAnchorElement;
}

const MAX_VISIBLE_RESULTS = 12;
const searchCategories: SearchCategory[] = ['all', 'docs', 'guides', 'reference'];
const categoryLabels: Record<SearchCategory, string> = {
	all: 'All',
	docs: 'Docs',
	guides: 'Guides',
	reference: 'Reference'
};

const isSearchEntry = (value: unknown): value is SiteSearchEntry => {
	if (!value || typeof value !== 'object') return false;
	const entry = value as Partial<SiteSearchEntry>;
	return (
		typeof entry.label === 'string' &&
		typeof entry.href === 'string' &&
		typeof entry.type === 'string' &&
		typeof entry.searchText === 'string'
	);
};

const normalizedKind = (entry: SiteSearchEntry): string =>
	entry.type
		.toLocaleLowerCase()
		.replaceAll(/[^a-z]+/g, '-')
		.replaceAll(/^-|-$/g, '');

const markerFor = (entry: SiteSearchEntry): string => {
	const markers: Record<string, string> = {
		Docs: 'D',
		'Docs section': '§',
		Guide: 'G',
		'Guide section': '§',
		Module: 'M',
		class: 'C',
		command: '$',
		function: 'ƒ',
		interface: 'I',
		method: 'ƒ',
		type: 'T'
	};
	return markers[entry.type] ?? '·';
};

const escapeRegularExpression = (value: string): string =>
	value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');

function appendHighlightedText(parent: HTMLElement, value: string, terms: string[]): void {
	if (terms.length === 0) {
		parent.textContent = value;
		return;
	}

	const uniqueTerms = [...new Set(terms)].sort((left, right) => right.length - left.length);
	const expression = new RegExp(`(${uniqueTerms.map(escapeRegularExpression).join('|')})`, 'gi');
	const normalizedTerms = new Set(uniqueTerms.map((term) => term.toLocaleLowerCase()));

	for (const part of value.split(expression)) {
		if (!part) continue;
		if (normalizedTerms.has(part.toLocaleLowerCase())) {
			const mark = document.createElement('mark');
			mark.className = 'search-match';
			mark.textContent = part;
			parent.append(mark);
		} else {
			parent.append(document.createTextNode(part));
		}
	}
}

function displayRoute(href: string): string {
	try {
		return decodeURIComponent(href);
	} catch {
		return href;
	}
}

function elementsFor(dialog: HTMLDialogElement): SearchElements | undefined {
	const input = dialog.querySelector<HTMLInputElement>('[data-search-input]');
	const form = dialog.querySelector<HTMLFormElement>('[data-search-form]');
	const results = dialog.querySelector<HTMLElement>('[data-search-results]');
	const viewport = dialog.querySelector<HTMLElement>('[data-search-list]');
	const empty = dialog.querySelector<HTMLElement>('[data-search-empty]');
	const emptyDetail = dialog.querySelector<HTMLElement>('[data-search-empty-detail]');
	const status = dialog.querySelector<HTMLElement>('[data-search-status]');
	const statusTitle = dialog.querySelector<HTMLElement>('[data-search-status-title]');
	const statusDetail = dialog.querySelector<HTMLElement>('[data-search-status-detail]');
	const announcer = dialog.querySelector<HTMLElement>('[data-search-announcer]');
	const summary = dialog.querySelector<HTMLElement>('[data-search-summary]');
	const allResults = dialog.querySelector<HTMLAnchorElement>('[data-search-all]');
	if (
		!input ||
		!form ||
		!results ||
		!viewport ||
		!empty ||
		!emptyDetail ||
		!status ||
		!statusTitle ||
		!statusDetail ||
		!announcer ||
		!summary ||
		!allResults
	) {
		return;
	}
	return {
		dialog,
		input,
		form,
		results,
		viewport,
		empty,
		emptyDetail,
		status,
		statusTitle,
		statusDetail,
		announcer,
		summary,
		allResults
	};
}

export function initializeSiteSearch(dialog: HTMLDialogElement): void {
	if (dialog.dataset.searchReady === 'true') return;
	const elements = elementsFor(dialog);
	if (!elements) return;
	dialog.dataset.searchReady = 'true';

	const {
		input,
		form,
		results,
		viewport,
		empty,
		emptyDetail,
		status,
		statusTitle,
		statusDetail,
		announcer,
		summary,
		allResults
	} = elements;
	const openButtons = document.querySelectorAll<HTMLElement>('[data-search-open]');
	const filterButtons = [...dialog.querySelectorAll<HTMLButtonElement>('[data-search-filter]')];
	let catalog: SiteSearchEntry[] | undefined;
	let catalogPromise: Promise<SiteSearchEntry[]> | undefined;
	let visibleItems: HTMLAnchorElement[] = [];
	let activeIndex = -1;
	let activeFilter: SearchCategory = 'all';
	let returnFocus: HTMLElement | null = null;

	const shortcutLabels = document.querySelectorAll<HTMLElement>('[data-search-shortcut]');
	const applePlatform = /Mac|iPhone|iPad|iPod/i.test(navigator.platform);
	shortcutLabels.forEach((label) => (label.textContent = applePlatform ? '⌘K' : 'Ctrl K'));

	const clearActive = () => {
		activeIndex = -1;
		input.removeAttribute('aria-activedescendant');
		for (const item of visibleItems) {
			item.setAttribute('aria-selected', 'false');
			item.removeAttribute('data-active');
		}
	};

	const setActive = (index: number) => {
		if (visibleItems.length === 0) {
			clearActive();
			return;
		}
		activeIndex = (index + visibleItems.length) % visibleItems.length;
		for (const [itemIndex, item] of visibleItems.entries()) {
			const selected = itemIndex === activeIndex;
			item.setAttribute('aria-selected', String(selected));
			item.toggleAttribute('data-active', selected);
		}
		const active = visibleItems[activeIndex];
		input.setAttribute('aria-activedescendant', active.id);
		active.scrollIntoView({ block: 'nearest' });
	};

	const updateFilterButtons = (entries: SiteSearchEntry[]) => {
		const counts: Record<SearchCategory, number> = {
			all: entries.length,
			docs: 0,
			guides: 0,
			reference: 0
		};
		for (const entry of entries) counts[searchCategoryFor(entry)] += 1;

		for (const button of filterButtons) {
			const category = button.dataset.searchFilter as SearchCategory;
			if (!searchCategories.includes(category)) continue;
			const count = counts[category];
			button.setAttribute('aria-pressed', String(category === activeFilter));
			button.setAttribute(
				'aria-label',
				`${categoryLabels[category]}, ${count} result${count === 1 ? '' : 's'}`
			);
			button.dataset.empty = String(count === 0);
			const countNode = button.querySelector<HTMLElement>('[data-search-filter-count]');
			if (countNode) {
				countNode.textContent = count.toLocaleString();
				countNode.hidden = false;
			}
		}
	};

	const renderResult = (
		entry: SiteSearchEntry,
		index: number,
		terms: string[]
	): HTMLAnchorElement => {
		const link = document.createElement('a');
		link.className = 'search-result';
		link.id = `search-result-${index}`;
		link.href = entry.href;
		link.role = 'option';
		link.setAttribute('aria-selected', 'false');
		link.dataset.searchItem = '';
		link.dataset.searchCategory = searchCategoryFor(entry);
		link.dataset.searchKind = normalizedKind(entry);

		const marker = document.createElement('span');
		marker.className = 'search-result-marker';
		marker.setAttribute('aria-hidden', 'true');
		marker.textContent = markerFor(entry);

		const copy = document.createElement('span');
		copy.className = 'search-result-copy';
		const heading = document.createElement('span');
		heading.className = 'search-result-heading';
		const label = document.createElement('span');
		label.className = 'search-result-label';
		appendHighlightedText(label, entry.label, terms);
		const type = document.createElement('small');
		type.className = 'search-result-type';
		type.textContent = entry.type;
		heading.append(label, type);

		const context = document.createElement('span');
		context.className = 'search-result-route';
		appendHighlightedText(
			context,
			entry.breadcrumb?.length ? entry.breadcrumb.join(' › ') : displayRoute(entry.href),
			terms
		);
		copy.append(heading, context);
		if (entry.snippet) {
			const snippet = document.createElement('span');
			snippet.className = 'search-result-snippet';
			appendHighlightedText(snippet, entry.snippet, terms);
			copy.append(snippet);
		}

		const arrow = document.createElement('span');
		arrow.className = 'search-result-arrow';
		arrow.setAttribute('aria-hidden', 'true');
		arrow.textContent = '→';

		link.append(marker, copy, arrow);
		const activateFromPointer = () => {
			const itemIndex = visibleItems.indexOf(link);
			if (itemIndex >= 0 && itemIndex !== activeIndex) setActive(itemIndex);
		};
		// A stationary pointer must not reclaim selection when keyboard navigation scrolls the list.
		link.addEventListener('pointermove', activateFromPointer);
		link.addEventListener('pointerdown', activateFromPointer);
		link.addEventListener('focus', () => setActive(visibleItems.indexOf(link)));
		return link;
	};

	const render = () => {
		if (!catalog) return;
		const query = normalizeSearchQuery(input.value);
		const terms = searchTermsFor(query);
		const searchURL = new URL('/search/', window.location.origin);
		if (query) searchURL.searchParams.set('q', input.value.trim());
		if (activeFilter !== 'all') searchURL.searchParams.set('category', activeFilter);
		allResults.href = `${searchURL.pathname}${searchURL.search}`;
		viewport.scrollTop = 0;
		clearActive();
		visibleItems = [];
		results.replaceChildren();
		empty.hidden = true;

		if (terms.length === 0) {
			updateFilterButtons(catalog);
			status.hidden = false;
			statusTitle.textContent = 'Search across Ridu';
			statusDetail.textContent = `${catalog.length.toLocaleString()} pages, sections, modules, and API symbols are ready to search.`;
			summary.textContent = `${catalog.length.toLocaleString()} searchable entries`;
			announcer.textContent = `Search ready with ${catalog.length.toLocaleString()} entries.`;
			return;
		}

		const ranked = rankSearchCatalog(catalog, query);
		const allMatches = ranked.map(({ entry }) => entry);
		updateFilterButtons(allMatches);
		const filtered = ranked.filter(
			({ entry }) => activeFilter === 'all' || searchCategoryFor(entry) === activeFilter
		);
		const shown = filtered.slice(0, MAX_VISIBLE_RESULTS);
		status.hidden = true;

		if (filtered.length === 0) {
			empty.hidden = false;
			emptyDetail.textContent =
				activeFilter === 'all'
					? `Nothing in the index matches “${input.value.trim()}”. Try fewer or broader terms.`
					: `Nothing in ${categoryLabels[activeFilter]} matches “${input.value.trim()}”. Try another filter or broader terms.`;
			summary.textContent = `No ${categoryLabels[activeFilter].toLocaleLowerCase()} results`;
			announcer.textContent = `No ${categoryLabels[activeFilter].toLocaleLowerCase()} results for ${input.value.trim()}.`;
			return;
		}

		const fragment = document.createDocumentFragment();
		visibleItems = shown.map(({ entry }, index) => {
			const link = renderResult(entry, index, terms);
			fragment.append(link);
			return link;
		});
		results.replaceChildren(fragment);
		const visibleCount = shown.length;
		summary.textContent =
			visibleCount === filtered.length
				? `${filtered.length.toLocaleString()} result${filtered.length === 1 ? '' : 's'}`
				: `${visibleCount} of ${filtered.length.toLocaleString()} results · refine to see more`;
		announcer.textContent = `${filtered.length.toLocaleString()} ${categoryLabels[activeFilter].toLocaleLowerCase()} result${filtered.length === 1 ? '' : 's'} for ${input.value.trim()}.`;
		setActive(0);
	};

	const loadCatalog = (): Promise<SiteSearchEntry[]> => {
		catalogPromise ??= fetch('/search-index.json')
			.then(async (response) => {
				if (!response.ok) throw new Error(`Search index returned ${response.status}`);
				const value: unknown = await response.json();
				if (!Array.isArray(value) || !value.every(isSearchEntry)) {
					throw new Error('Search index has an invalid shape');
				}
				return value;
			})
			.catch((error: unknown) => {
				catalogPromise = undefined;
				throw error;
			});
		return catalogPromise;
	};

	const setFilter = (category: SearchCategory) => {
		activeFilter = category;
		for (const button of filterButtons) {
			button.setAttribute('aria-pressed', String(button.dataset.searchFilter === category));
		}
		render();
	};

	const resetSearch = () => {
		input.value = '';
		activeFilter = 'all';
		for (const button of filterButtons) {
			button.setAttribute('aria-pressed', String(button.dataset.searchFilter === 'all'));
		}
		if (catalog) render();
		else {
			results.replaceChildren();
			empty.hidden = true;
			clearActive();
		}
	};

	const openSearch = () => {
		if (!dialog.open) {
			returnFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null;
			resetSearch();
			dialog.showModal();
		}
		input.setAttribute('aria-expanded', 'true');
		openButtons.forEach((button) => button.setAttribute('aria-expanded', 'true'));
		viewport.setAttribute('aria-busy', String(!catalog));
		statusTitle.textContent = 'Loading search…';
		statusDetail.textContent = 'The documentation index is loaded only when you need it.';
		status.hidden = Boolean(catalog);
		requestAnimationFrame(() => {
			input.focus({ preventScroll: true });
			input.select();
		});

		if (!catalog) {
			void loadCatalog()
				.then((entries) => {
					catalog = entries;
					viewport.setAttribute('aria-busy', 'false');
					render();
				})
				.catch(() => {
					viewport.setAttribute('aria-busy', 'false');
					statusTitle.textContent = 'Search is temporarily unavailable';
					statusDetail.textContent =
						'Refresh the page to try loading the documentation index again.';
					status.hidden = false;
					summary.textContent = 'Index unavailable';
					announcer.textContent = 'Search is temporarily unavailable.';
				});
		}
	};

	const closeSearch = () => {
		if (dialog.open) dialog.close();
	};

	openButtons.forEach((button) => {
		button.addEventListener('click', openSearch);
	});
	dialog.querySelector<HTMLElement>('[data-search-close]')?.addEventListener('click', closeSearch);
	form.addEventListener('submit', (event) => {
		event.preventDefault();
		visibleItems[activeIndex >= 0 ? activeIndex : 0]?.click();
	});
	dialog.addEventListener('cancel', (event) => {
		event.preventDefault();
		closeSearch();
	});
	dialog.addEventListener('close', () => {
		input.setAttribute('aria-expanded', 'false');
		openButtons.forEach((button) => button.setAttribute('aria-expanded', 'false'));
		resetSearch();
		const focusTarget = returnFocus;
		returnFocus = null;
		if (focusTarget?.isConnected) requestAnimationFrame(() => focusTarget.focus());
	});
	dialog.addEventListener('click', (event) => {
		if (event.target === dialog) closeSearch();
	});

	filterButtons.forEach((button, buttonIndex) => {
		button.addEventListener('click', () => {
			const category = button.dataset.searchFilter as SearchCategory;
			if (searchCategories.includes(category)) setFilter(category);
		});
		button.addEventListener('keydown', (event) => {
			if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return;
			event.preventDefault();
			const direction = event.key === 'ArrowRight' ? 1 : -1;
			const nextIndex = (buttonIndex + direction + filterButtons.length) % filterButtons.length;
			const nextButton = filterButtons[nextIndex];
			const category = nextButton.dataset.searchFilter as SearchCategory;
			if (searchCategories.includes(category)) setFilter(category);
			nextButton.focus();
		});
	});

	document.addEventListener(
		'keydown',
		(event) => {
			const shortcut =
				(event.metaKey || event.ctrlKey) && !event.altKey && event.key.toLocaleLowerCase() === 'k';
			const target = event.target instanceof HTMLElement ? event.target : null;
			const isTyping =
				target?.matches('input, textarea, select, [contenteditable="true"]') ?? false;

			if (event.key === 'Escape' && dialog.open) {
				event.preventDefault();
				event.stopPropagation();
				closeSearch();
				return;
			}
			if (shortcut || (event.key === '/' && !isTyping)) {
				event.preventDefault();
				event.stopPropagation();
				openSearch();
			}
		},
		{ capture: true }
	);

	input.addEventListener('input', render);
	input.addEventListener('keydown', (event) => {
		if (event.key === 'ArrowDown') {
			event.preventDefault();
			setActive(activeIndex + 1);
		} else if (event.key === 'ArrowUp') {
			event.preventDefault();
			setActive(activeIndex < 0 ? -1 : activeIndex - 1);
		} else if (event.key === 'Home' && visibleItems.length > 0) {
			event.preventDefault();
			setActive(0);
		} else if (event.key === 'End' && visibleItems.length > 0) {
			event.preventDefault();
			setActive(visibleItems.length - 1);
		} else if (event.key === 'Enter') {
			event.preventDefault();
			visibleItems[activeIndex >= 0 ? activeIndex : 0]?.click();
		}
	});
}
