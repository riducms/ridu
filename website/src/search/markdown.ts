import { fromMarkdown } from 'mdast-util-from-markdown';
import type { Root, RootContent } from 'mdast';

export interface MarkdownSearchSection {
	depth: 1 | 2 | 3 | 4;
	heading: string;
	id?: string;
	context: string[];
	text: string;
	identifiers: string[];
	snippet: string;
}

const authoredAnchorPattern = /\s*\{#([A-Za-z][\w:.-]*)\}\s*$/;
const identifierPattern =
	/(?:@[a-z0-9][\w.-]*\/[\w./-]+|[A-Za-z_$][\w$]*(?:[./#:][A-Za-z_$][\w$-]*)+|[A-Z][A-Z0-9_]{2,}|--[a-z][\w-]*)/g;

function textFor(node: RootContent): string {
	if ('value' in node && typeof node.value === 'string') return node.value;
	if ('children' in node && Array.isArray(node.children)) {
		return node.children.map((child) => textFor(child as RootContent)).join(' ');
	}
	return '';
}

function normalizedExcerpt(value: string): string {
	return value
		.replaceAll(/<[^>]+>/g, ' ')
		.replaceAll(/\s+/g, ' ')
		.trim();
}

function identifiersFor(value: string): string[] {
	const identifiers = value.match(identifierPattern) ?? [];
	return [
		...new Set(
			identifiers.flatMap((identifier) => {
				const segments = identifier.split('.');
				return segments.length > 2
					? [
							identifier,
							...segments.slice(1, -1).map((_, index) => segments.slice(index + 1).join('.'))
						]
					: [identifier];
			})
		)
	];
}

function snippetFor(value: string, fallback: string): string {
	const normalized = normalizedExcerpt(value);
	if (!normalized) return fallback;
	return normalized.length <= 220 ? normalized : `${normalized.slice(0, 217).trimEnd()}…`;
}

/**
 * Convert one authored Markdown document into page and H2–H4 search records.
 * Code fences and table source stay in their owning section so commands and
 * identifiers are discoverable even when they never appear in a heading.
 */
export function extractMarkdownSearchSections(
	source: string,
	pageTitle: string
): MarkdownSearchSection[] {
	const tree: Root = fromMarkdown(source);
	const sections: MarkdownSearchSection[] = [];
	const context: Partial<Record<1 | 2 | 3 | 4, string>> = { 1: pageTitle };
	let current: MarkdownSearchSection = {
		depth: 1,
		heading: pageTitle,
		context: [pageTitle],
		text: '',
		identifiers: [],
		snippet: ''
	};
	sections.push(current);

	for (const node of tree.children) {
		if (node.type === 'heading' && node.depth >= 2 && node.depth <= 4) {
			const depth = node.depth as 2 | 3 | 4;
			const rawHeading = normalizedExcerpt(textFor(node));
			const authoredAnchor = rawHeading.match(authoredAnchorPattern)?.[1];
			const heading = rawHeading.replace(authoredAnchorPattern, '').trim();
			for (let descendantDepth = depth; descendantDepth <= 4; descendantDepth += 1) {
				delete context[descendantDepth as 2 | 3 | 4];
			}
			context[depth] = heading;
			current = {
				depth,
				heading,
				id: authoredAnchor,
				context: [1, 2, 3, 4]
					.map((depth) => context[depth as 1 | 2 | 3 | 4])
					.filter((value): value is string => Boolean(value)),
				text: '',
				identifiers: [],
				snippet: ''
			};
			sections.push(current);
			continue;
		}

		const text = normalizedExcerpt(textFor(node));
		if (!text) continue;
		current.text = `${current.text} ${text}`.trim();
		current.identifiers = [...new Set([...current.identifiers, ...identifiersFor(text)])];
	}

	for (const section of sections) {
		section.snippet = snippetFor(section.text, section.heading);
	}
	return sections;
}
