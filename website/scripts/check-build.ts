import { existsSync, readdirSync, readFileSync, statSync } from 'node:fs';
import { dirname, extname, relative, resolve, sep } from 'node:path';
import { fileURLToPath } from 'node:url';
import { gzipSync } from 'node:zlib';
import { referenceModules } from '../src/reference';
import { frameworkVersion } from '../src/llms';
import { rankSearchCatalog } from '../src/search/rank';

const websiteRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const distRoot = resolve(websiteRoot, 'dist');
const sourceRoot = resolve(websiteRoot, 'src');
// Most reference symbols intentionally have their own static page. Budget a small fixed site shell
// plus an average allowance per exact route so expanding the reviewed API catalog does not require
// weakening the regression check. The total budget and explicit search-embedding check catch shared
// payloads copied into every page without penalizing legitimately large reference indexes.
const htmlBaseSizeAllowance = 2 * 1024 * 1024;
const htmlPerRouteSizeAllowance = 34 * 1024;
const searchIndexRawCeiling = 8 * 1024 * 1024;
const searchIndexGzipCeiling = 1536 * 1024;
const diagnosticLimit = 30;

class Diagnostics {
	readonly groups = new Map<string, Set<string>>();

	add(group: string, message: string): void {
		const messages = this.groups.get(group) ?? new Set<string>();
		messages.add(message);
		this.groups.set(group, messages);
	}

	get size(): number {
		return [...this.groups.values()].reduce((total, messages) => total + messages.size, 0);
	}

	report(): never {
		console.error(`\nBuild regression check failed with ${this.size} problem(s):`);
		for (const [group, messages] of this.groups) {
			console.error(`\n${group}:`);
			const list = [...messages];
			for (const message of list.slice(0, diagnosticLimit)) console.error(`  - ${message}`);
			if (list.length > diagnosticLimit) {
				console.error(`  - …and ${list.length - diagnosticLimit} more`);
			}
		}
		console.error(
			'\nRebuild with `bun run build`, fix the diagnostics above, and rerun this script.'
		);
		process.exit(1);
	}
}

interface ParsedHTML {
	ids: Set<string>;
	hrefs: string[];
	resources: string[];
	duplicateIDs: string[];
	preRenderedSearchMarkers: number;
}

interface SearchEntry {
	label: string;
	href: string;
	type: string;
	searchText: string;
	aliases?: string[];
	breadcrumb?: string[];
	snippet?: string;
}

interface LinkIssue {
	kind: 'invalid URL' | 'missing target' | 'missing hash anchor';
	target: string;
	sources: Set<string>;
}

const diagnostics = new Diagnostics();
const parsedHTML = new Map<string, ParsedHTML>();
const linkIssues = new Map<string, LinkIssue>();

for (const requiredOutput of [
	'llms.txt',
	'llms-full.txt',
	`v/${frameworkVersion}/llms.txt`,
	`v/${frameworkVersion}/llms-full.txt`
]) {
	if (!existsSync(resolve(distRoot, requiredOutput))) {
		diagnostics.add('LLM documentation', `missing ${requiredOutput}`);
	}
}

function toPosix(value: string): string {
	return value.split(sep).join('/');
}

function walkFiles(directory: string): string[] {
	if (!existsSync(directory)) return [];
	return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
		const path = resolve(directory, entry.name);
		return entry.isDirectory() ? walkFiles(path) : [path];
	});
}

function routeToHTMLFile(route: string): string {
	if (route === '/') return 'index.html';
	return `${route.replace(/^\//, '')}index.html`;
}

function htmlFileToRoute(file: string): string {
	const output = toPosix(relative(distRoot, file));
	if (output === 'index.html') return '/';
	if (output.endsWith('/index.html')) return `/${output.slice(0, -'index.html'.length)}`;
	return `/${output}`;
}

function formatBytes(bytes: number): string {
	if (bytes < 1024) return `${bytes} B`;
	if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KiB`;
	return `${(bytes / (1024 * 1024)).toFixed(2)} MiB`;
}

function decodeHTMLEntities(value: string): string {
	const named: Record<string, string> = {
		amp: '&',
		apos: "'",
		gt: '>',
		lt: '<',
		quot: '"'
	};
	return value.replace(/&(#(?:x[\da-f]+|\d+)|amp|apos|gt|lt|quot);/gi, (entity, key: string) => {
		if (!key.startsWith('#')) return named[key.toLowerCase()] ?? entity;
		const hexadecimal = key[1]?.toLowerCase() === 'x';
		const codePoint = Number.parseInt(key.slice(hexadecimal ? 2 : 1), hexadecimal ? 16 : 10);
		if (!Number.isSafeInteger(codePoint)) return entity;
		try {
			return String.fromCodePoint(codePoint);
		} catch {
			return entity;
		}
	});
}

function attributesForTag(tag: string): Map<string, string> {
	const attributes = new Map<string, string>();
	const pattern = /\s+([^\s=/>]+)(?:\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'=<>`]+)))?/g;
	for (const match of tag.matchAll(pattern)) {
		attributes.set(
			match[1].toLowerCase(),
			decodeHTMLEntities(match[2] ?? match[3] ?? match[4] ?? '')
		);
	}
	return attributes;
}

function parseHTML(file: string): ParsedHTML {
	const cached = parsedHTML.get(file);
	if (cached) return cached;

	const source = readFileSync(file, 'utf8');
	const ids = new Set<string>();
	const hrefs: string[] = [];
	const resources: string[] = [];
	const duplicateIDs: string[] = [];
	let preRenderedSearchMarkers = 0;
	const tags = source.match(/<[a-z][^<>]*>/gi) ?? [];

	for (const tag of tags) {
		const attributes = attributesForTag(tag);
		const id = attributes.get('id');
		if (id) {
			if (ids.has(id)) duplicateIDs.push(id);
			ids.add(id);
		}
		const name = attributes.get('name');
		if (name) ids.add(name);

		const href = attributes.get('href');
		if (href !== undefined) hrefs.push(href);
		for (const attribute of ['src', 'poster']) {
			const resource = attributes.get(attribute);
			if (resource !== undefined) resources.push(resource);
		}

		if (
			attributes.has('data-search-item') ||
			attributes.has('data-search-value') ||
			attributes.get('role') === 'option' ||
			/^search-result-\d+$/.test(id ?? '')
		) {
			preRenderedSearchMarkers += 1;
		}
	}

	const parsed = { ids, hrefs, resources, duplicateIDs, preRenderedSearchMarkers };
	parsedHTML.set(file, parsed);
	return parsed;
}

function safeDecodePath(pathname: string): string | undefined {
	try {
		return decodeURIComponent(pathname);
	} catch {
		return undefined;
	}
}

function outputCandidates(pathname: string): string[] {
	const decoded = safeDecodePath(pathname);
	if (decoded === undefined) return [];
	const relativePath = decoded.replace(/^\/+/, '');
	if (!relativePath) return [resolve(distRoot, 'index.html')];
	if (decoded.endsWith('/')) return [resolve(distRoot, relativePath, 'index.html')];
	if (extname(relativePath)) return [resolve(distRoot, relativePath)];
	return [
		resolve(distRoot, relativePath),
		resolve(distRoot, relativePath, 'index.html'),
		resolve(distRoot, `${relativePath}.html`)
	];
}

function recordLinkIssue(kind: LinkIssue['kind'], target: string, source: string): void {
	const key = `${kind}\0${target}`;
	const issue = linkIssues.get(key) ?? { kind, target, sources: new Set<string>() };
	issue.sources.add(source);
	linkIssues.set(key, issue);
}

function validateInternalURL(rawValue: string, sourceFile: string, sourceLabel: string): void {
	const value = decodeHTMLEntities(rawValue.trim());
	if (!value || value.startsWith('//')) return;
	if (/^[a-z][a-z\d+.-]*:/i.test(value)) return;
	if (!value.startsWith('/') && !value.startsWith('#')) return;

	let targetURL: URL;
	try {
		targetURL = new URL(value, 'https://ridu.invalid/');
	} catch {
		recordLinkIssue('invalid URL', value, sourceLabel);
		return;
	}

	const candidates = value.startsWith('#') ? [sourceFile] : outputCandidates(targetURL.pathname);
	const targetFile = candidates.find((candidate) => {
		const fromDist = relative(distRoot, candidate);
		return (
			fromDist !== '..' &&
			!fromDist.startsWith(`..${sep}`) &&
			existsSync(candidate) &&
			statSync(candidate).isFile()
		);
	});

	if (!targetFile) {
		recordLinkIssue('missing target', `${targetURL.pathname}${targetURL.search}`, sourceLabel);
		return;
	}

	if (!targetURL.hash || extname(targetFile).toLowerCase() !== '.html') return;
	let fragment: string;
	try {
		fragment = decodeURIComponent(targetURL.hash.slice(1));
	} catch {
		recordLinkIssue('invalid URL', value, sourceLabel);
		return;
	}
	if (fragment && !parseHTML(targetFile).ids.has(fragment)) {
		recordLinkIssue(
			'missing hash anchor',
			`${htmlFileToRoute(targetFile)}#${fragment}`,
			sourceLabel
		);
	}
}

function sourceSummary(sources: Set<string>): string {
	const list = [...sources].sort();
	const sample = list.slice(0, 4).join(', ');
	return list.length > 4 ? `${sample}, and ${list.length - 4} more` : sample;
}

function addExpectedRoute(expected: Map<string, string>, route: string, provenance: string): void {
	const previous = expected.get(route);
	if (previous) {
		diagnostics.add(
			'Expected route definition',
			`${route} is produced by both ${previous} and ${provenance}`
		);
		return;
	}
	expected.set(route, provenance);
}

function contentRoutes(expected: Map<string, string>, area: 'docs' | 'guides'): void {
	const contentRoot = resolve(sourceRoot, 'content', area);
	if (!existsSync(contentRoot)) {
		diagnostics.add('Expected route definition', `Missing source content directory ${contentRoot}`);
		return;
	}
	const files = walkFiles(contentRoot).filter((file) => /\.mdx?$/i.test(file));
	for (const file of files) {
		const slug = toPosix(relative(contentRoot, file)).replace(/\.mdx?$/i, '');
		addExpectedRoute(expected, `/${area}/${slug}/`, toPosix(relative(websiteRoot, file)));
	}
}

function expectedRoutes(): Map<string, string> {
	const expected = new Map<string, string>();
	addExpectedRoute(expected, '/', 'src/pages/index.astro');
	addExpectedRoute(expected, '/docs/', 'src/pages/docs/index.astro');
	addExpectedRoute(expected, '/guides/', 'src/pages/guides/index.astro');
	addExpectedRoute(expected, '/search/', 'src/pages/search/index.astro');
	contentRoutes(expected, 'docs');
	contentRoutes(expected, 'guides');
	addExpectedRoute(
		expected,
		'/guides/project-structure/',
		'src/pages/guides/project-structure/index.astro'
	);
	addExpectedRoute(expected, '/reference/', 'src/pages/reference/index.astro');
	for (const module of referenceModules) {
		addExpectedRoute(expected, `/reference/${module.slug}/`, `reference module ${module.slug}`);
		for (const symbol of module.symbols) {
			addExpectedRoute(
				expected,
				`/reference/${module.slug}/${symbol.slug}/`,
				`reference symbol ${module.slug}.${symbol.slug}`
			);
		}
	}
	return expected;
}

function isSearchEntry(value: unknown): value is SearchEntry {
	if (!value || typeof value !== 'object') return false;
	const entry = value as Record<string, unknown>;
	return (
		typeof entry.label === 'string' &&
		typeof entry.href === 'string' &&
		typeof entry.type === 'string' &&
		typeof entry.searchText === 'string'
	);
}

function validateSearchIndex(expected: Map<string, string>, fallbackSource: string): number {
	const searchIndexFile = resolve(distRoot, 'search-index.json');
	if (!existsSync(searchIndexFile)) {
		diagnostics.add(
			'Search index',
			'Missing dist/search-index.json; the search catalog must be emitted once per build'
		);
		return 0;
	}
	const raw = readFileSync(searchIndexFile);
	const compressedBytes = gzipSync(raw).byteLength;
	if (raw.byteLength > searchIndexRawCeiling) {
		diagnostics.add(
			'Search size',
			`dist/search-index.json is ${formatBytes(raw.byteLength)}, above the ${formatBytes(searchIndexRawCeiling)} raw ceiling`
		);
	}
	if (compressedBytes > searchIndexGzipCeiling) {
		diagnostics.add(
			'Search size',
			`dist/search-index.json is ${formatBytes(compressedBytes)} gzipped, above the ${formatBytes(searchIndexGzipCeiling)} compressed ceiling`
		);
	}

	let data: unknown;
	try {
		data = JSON.parse(readFileSync(searchIndexFile, 'utf8'));
	} catch (error) {
		diagnostics.add(
			'Search index',
			`dist/search-index.json is not valid JSON: ${error instanceof Error ? error.message : String(error)}`
		);
		return 0;
	}
	if (!Array.isArray(data)) {
		diagnostics.add('Search index', 'dist/search-index.json must contain a JSON array');
		return 0;
	}

	const hrefs = new Map<string, number>();
	const pageTargets = new Set<string>();
	for (const [index, value] of data.entries()) {
		if (!isSearchEntry(value)) {
			diagnostics.add(
				'Search index',
				`Entry ${index} must contain string label, href, type, and searchText fields`
			);
			continue;
		}
		const label = `search entry ${index} (${JSON.stringify(value.label)})`;
		for (const field of ['label', 'href', 'type', 'searchText'] as const) {
			if (!value[field].trim()) diagnostics.add('Search index', `${label} has an empty ${field}`);
		}
		if (!value.href.startsWith('/') || value.href.startsWith('//')) {
			diagnostics.add('Search index', `${label} must use an internal absolute href: ${value.href}`);
		} else {
			validateInternalURL(value.href, fallbackSource, label);
			try {
				const url = new URL(value.href, 'https://ridu.invalid/');
				if (!url.hash && !url.search) pageTargets.add(url.pathname);
			} catch {
				// validateInternalURL reports the malformed URL.
			}
		}
		if (value.searchText !== value.searchText.toLocaleLowerCase()) {
			diagnostics.add('Search index', `${label} has searchText that is not lower-case`);
		}
		if (value.aliases !== undefined && !value.aliases.every((alias) => typeof alias === 'string')) {
			diagnostics.add('Search index', `${label} has invalid aliases`);
		}
		if (
			value.breadcrumb !== undefined &&
			!value.breadcrumb.every((crumb) => typeof crumb === 'string' && crumb.length > 0)
		) {
			diagnostics.add('Search index', `${label} has an invalid breadcrumb`);
		}
		if (value.snippet !== undefined && typeof value.snippet !== 'string') {
			diagnostics.add('Search index', `${label} has an invalid snippet`);
		}
		const previous = hrefs.get(value.href);
		if (previous !== undefined) {
			diagnostics.add(
				'Search index',
				`${label} duplicates href ${value.href} from entry ${previous}`
			);
		} else {
			hrefs.set(value.href, index);
		}
	}

	for (const route of expected.keys()) {
		if (route === '/' || route === '/reference/' || route === '/search/') continue;
		if (!pageTargets.has(route)) {
			diagnostics.add('Search index', `No page-level search entry targets ${route}`);
		}
	}

	const searchable = data.filter(isSearchEntry);
	const acceptanceQueries: Array<{
		query: string;
		expectedHref?: string;
		expectedLabel?: string;
	}> = [
		{ query: 'field.Select', expectedHref: '/reference/field/select/' },
		{ query: 'Select field', expectedHref: '/docs/fields/select/' },
		{ query: 'LocalAPI.Find', expectedLabel: 'LocalAPI.Find' },
		{ query: 'RiduClient.list', expectedLabel: 'RiduClient.list' },
		{ query: 'ridu migrate verify', expectedLabel: 'ridu migrate verify' },
		{ query: 'DATABASE_URL' },
		{ query: 'ridu_session', expectedHref: '/docs/quickstart/' }
	];
	for (const acceptance of acceptanceQueries) {
		const result = rankSearchCatalog(searchable, acceptance.query)[0]?.entry;
		if (!result) {
			diagnostics.add('Search ranking', `${acceptance.query} returned no result`);
			continue;
		}
		if (acceptance.expectedHref && result.href !== acceptance.expectedHref) {
			diagnostics.add(
				'Search ranking',
				`${acceptance.query} ranked ${result.href} first instead of ${acceptance.expectedHref}`
			);
		}
		if (acceptance.expectedLabel && result.label !== acceptance.expectedLabel) {
			diagnostics.add(
				'Search ranking',
				`${acceptance.query} ranked ${JSON.stringify(result.label)} first instead of ${JSON.stringify(acceptance.expectedLabel)}`
			);
		}
	}
	return data.length;
}

function validateThemeVariables(): void {
	const cssFiles = walkFiles(distRoot).filter((file) => extname(file).toLowerCase() === '.css');
	const source = cssFiles.map((file) => readFileSync(file, 'utf8')).join('\n');
	const definitions = new Set(
		[...source.matchAll(/(--colors-[\w-]+)\s*:/g)].map((match) => match[1])
	);
	const references = new Set(
		[...source.matchAll(/var\((--colors-[\w-]+)/g)].map((match) => match[1])
	);

	for (const name of [
		'--colors-canvas',
		'--colors-surface',
		'--colors-surface-raised',
		'--colors-surface-hover',
		'--colors-ink',
		'--colors-ink-soft',
		'--colors-ink-faint',
		'--colors-line',
		'--colors-line-strong',
		'--colors-accent',
		'--colors-accent-soft',
		'--colors-blue',
		'--colors-cyan',
		'--colors-green',
		'--colors-violet'
	]) {
		if (!definitions.has(name)) {
			diagnostics.add('Theme variables', `${name} is not emitted by the Uno theme`);
		}
	}

	for (const name of references) {
		if (!definitions.has(name)) {
			diagnostics.add('Theme variables', `${name} is referenced but never defined`);
		}
	}
}

if (!existsSync(distRoot)) {
	console.error(
		`Build regression check failed: ${distRoot} does not exist. Run \`bun run build\` first.`
	);
	process.exit(1);
}

validateThemeVariables();

const expected = expectedRoutes();
const htmlSizeCeiling = htmlBaseSizeAllowance + expected.size * htmlPerRouteSizeAllowance;
const htmlFiles = walkFiles(distRoot).filter((file) => extname(file).toLowerCase() === '.html');
const actualFiles = new Set(htmlFiles.map((file) => toPosix(relative(distRoot, file))));
const expectedFiles = new Map(
	[...expected].map(([route, provenance]) => [routeToHTMLFile(route), { route, provenance }])
);

for (const [file, { route, provenance }] of expectedFiles) {
	if (!actualFiles.has(file)) {
		diagnostics.add('HTML route set', `Missing ${route} (${file}), expected from ${provenance}`);
	}
}

for (const file of actualFiles) {
	if (!expectedFiles.has(file)) {
		diagnostics.add(
			'HTML route set',
			`Unexpected HTML output ${htmlFileToRoute(resolve(distRoot, file))} (${file})`
		);
	}
}

let totalHTMLBytes = 0;
const pageSizes: Array<{ route: string; size: number }> = [];
const pagesWithSearchValues: string[] = [];

for (const file of htmlFiles) {
	const route = htmlFileToRoute(file);
	const size = statSync(file).size;
	totalHTMLBytes += size;
	pageSizes.push({ route, size });
	const parsed = parseHTML(file);

	for (const duplicate of parsed.duplicateIDs) {
		diagnostics.add('HTML anchors', `${route} contains duplicate id ${JSON.stringify(duplicate)}`);
	}
	if (parsed.preRenderedSearchMarkers > 0) pagesWithSearchValues.push(route);
	for (const href of parsed.hrefs) validateInternalURL(href, file, route);
	for (const resource of parsed.resources) validateInternalURL(resource, file, route);
}

if (pagesWithSearchValues.length > 0) {
	const sample = pagesWithSearchValues.slice(0, 8).join(', ');
	diagnostics.add(
		'Search embedding',
		`${pagesWithSearchValues.length} HTML page(s) contain pre-rendered search result items or values (${sample}${pagesWithSearchValues.length > 8 ? ', …' : ''}). Keep results only in dist/search-index.json.`
	);
}

pageSizes.sort((left, right) => right.size - left.size);
if (totalHTMLBytes > htmlSizeCeiling) {
	const largest = pageSizes
		.slice(0, 5)
		.map((page) => `${page.route} ${formatBytes(page.size)}`)
		.join(', ');
	diagnostics.add(
		'HTML size budget',
		`Total HTML is ${formatBytes(totalHTMLBytes)}, above the ${formatBytes(htmlSizeCeiling)} route-scaled ceiling (${formatBytes(htmlBaseSizeAllowance)} base + ${formatBytes(htmlPerRouteSizeAllowance)} × ${expected.size} routes). Keep shared catalogs out of per-page HTML. Largest pages: ${largest}`
	);
}

const fallbackSource =
	htmlFiles.find((file) => htmlFileToRoute(file) === '/') ?? resolve(distRoot, 'index.html');
const searchEntryCount = validateSearchIndex(expected, fallbackSource);

for (const issue of linkIssues.values()) {
	diagnostics.add(
		'Internal links',
		`${issue.kind}: ${issue.target} (from ${sourceSummary(issue.sources)})`
	);
}

if (diagnostics.size > 0) diagnostics.report();

console.log(
	`Build regression check passed: ${htmlFiles.length} exact HTML routes, ${searchEntryCount} unique search entries, ${formatBytes(totalHTMLBytes)} total HTML.`
);
