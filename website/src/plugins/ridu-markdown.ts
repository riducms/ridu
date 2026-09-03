import type { SatteriProcessorOptions } from '@astrojs/markdown-satteri';
import type { Element, Parents, Root } from 'hast';
import { normalizeSource } from '@/components/code/normalize-source';
import {
	isPackageManager,
	packageManagers,
	type PackageManager
} from '@/components/code/package-manager';
import { createCodeLanguageIcon } from './code-language-icon';

type MdastPlugin = NonNullable<SatteriProcessorOptions['mdastPlugins']>[number];
type HastPlugin = NonNullable<SatteriProcessorOptions['hastPlugins']>[number];

interface CodeMetadata {
	code: string;
	label: string;
	language: string;
	packageManager?: PackageManager;
}

const codeMetadataKey = 'riduCodeMetadata';
const codeMetadataIndexKey = 'riduCodeMetadataIndex';
const explicitHeadingId = /\s+\{#([A-Za-z][\w:.-]*)\}\s*$/;
const githubAlertMarker = /^\[!(NOTE|TIP|IMPORTANT|WARNING|CAUTION)\]\s*/;
const packageManagerMeta = /(?:^|\s)package-manager=("(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|[^\s]+)/;

export function createMarkdownAlert(node: Readonly<Element>): Element | undefined {
	const paragraphIndex = node.children.findIndex(
		(child) => child.type === 'element' && child.tagName === 'p'
	);
	const paragraph = node.children[paragraphIndex];
	if (paragraph?.type !== 'element' || paragraph.tagName !== 'p') return;
	const marker = paragraph.children[0];
	if (marker?.type !== 'text') return;

	const match = marker.value.match(githubAlertMarker);
	if (!match) return;

	const variant = match[1].toLowerCase();
	const remainingText = marker.value.slice(match[0].length);
	const remainingParagraphChildren = [
		...(remainingText ? [{ ...marker, value: remainingText }] : []),
		...paragraph.children.slice(1)
	];
	const remainingChildren = [
		...node.children.slice(0, paragraphIndex),
		...(remainingParagraphChildren.length > 0
			? [{ ...paragraph, children: remainingParagraphChildren }]
			: []),
		...node.children.slice(paragraphIndex + 1)
	];

	return {
		type: 'element',
		tagName: 'aside',
		properties: {
			className: ['markdown-alert', `markdown-alert-${variant}`],
			'data-callout': variant,
			'aria-label': `${match[1][0]}${match[1].slice(1).toLowerCase()}`
		},
		children: [
			{
				type: 'element',
				tagName: 'p',
				properties: { className: ['markdown-alert-title'] },
				children: [
					{
						type: 'text',
						value: `${match[1][0]}${match[1].slice(1).toLowerCase()}`
					}
				]
			},
			...remainingChildren
		]
	};
}

function readCodeMetadata(value: unknown): CodeMetadata[] {
	return Array.isArray(value) ? (value as CodeMetadata[]) : [];
}

function parseLabel(meta: string | null | undefined, language: string): string {
	const match = meta?.match(/(?:^|\s)(?:title|label)=("(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|[^\s]+)/);
	return parseMetaValue(match?.[1]) ?? language;
}

function parseMetaValue(value: string | undefined): string | undefined {
	if (!value) return;
	if (value.startsWith('"')) {
		try {
			return JSON.parse(value) as string;
		} catch {
			return value.slice(1, -1);
		}
	}
	if (value.startsWith("'")) return value.slice(1, -1).replaceAll("\\'", "'");
	return value;
}

function parsePackageManager(meta: string | null | undefined): PackageManager | undefined {
	const value = parseMetaValue(meta?.match(packageManagerMeta)?.[1]);
	return isPackageManager(value) ? value : undefined;
}

/**
 * Captures fence metadata before Shiki replaces each Markdown code node.
 * The matching HAST plugin consumes this per-document queue afterwards.
 */
export const riduMarkdownCodeMetadata = {
	name: 'ridu-code-metadata',
	code(node, context) {
		const code = normalizeSource(node.value);
		const language = node.lang ?? 'text';
		const packageManager = parsePackageManager(node.meta);
		const metadata = readCodeMetadata(context.data[codeMetadataKey]);
		metadata.push({
			code,
			label: parseLabel(node.meta, language),
			language,
			...(packageManager ? { packageManager } : {})
		});
		context.data[codeMetadataKey] = metadata;
		context.setProperty(node, 'value', code);
	}
} satisfies MdastPlugin;

function createCopyButton(
	code: string,
	className: string[] = [],
	properties: Element['properties'] = {}
): Element {
	return {
		type: 'element',
		tagName: 'button',
		properties: {
			...(className.length > 0 ? { className } : {}),
			type: 'button',
			'data-copy-code': '',
			'data-code': code,
			'aria-label': 'Copy code',
			...properties
		},
		children: [
			{
				type: 'element',
				tagName: 'svg',
				properties: { viewBox: '0 0 24 24', 'aria-hidden': 'true' },
				children: [
					{
						type: 'element',
						tagName: 'rect',
						properties: { x: '8', y: '8', width: '11', height: '11', rx: '1' },
						children: []
					},
					{
						type: 'element',
						tagName: 'path',
						properties: { d: 'M16 8V5H5v11h3' },
						children: []
					}
				]
			},
			{
				type: 'element',
				tagName: 'span',
				properties: {},
				children: [{ type: 'text', value: 'Copy' }]
			}
		]
	};
}

function createCodeBlock(node: Element, block: CodeMetadata): Element {
	return {
		type: 'element',
		tagName: 'figure',
		properties: {
			className: ['code-block'],
			'data-code-block': '',
			...(block.packageManager
				? {
						'data-package-manager-source': block.packageManager,
						'data-package-manager-command': block.code
					}
				: {})
		},
		children: [
			{
				type: 'element',
				tagName: 'header',
				properties: {},
				children: [
					{
						type: 'element',
						tagName: 'span',
						properties: { className: ['code-label'] },
						children: [createCodeLanguageIcon(block.language), { type: 'text', value: block.label }]
					},
					createCopyButton(block.code)
				]
			},
			{
				type: 'element',
				tagName: 'div',
				properties: { className: ['code-source'] },
				children: [node]
			}
		]
	};
}

interface PackageManagerSource {
	node: Readonly<Element>;
	manager: PackageManager;
	command: string;
	codeSource: Readonly<Element>;
}

interface PackageManagerGroup {
	first: Readonly<Element>;
	discard: Readonly<Root['children'][number]>[];
	sources: PackageManagerSource[];
}

function classNames(node: Readonly<Element>): string[] {
	const value = node.properties.className;
	return Array.isArray(value) ? value.map(String) : [];
}

function packageManagerSource(
	node: Readonly<Root['children'][number]>
): PackageManagerSource | undefined {
	if (node.type !== 'element' || node.tagName !== 'figure') return;
	const managerValue = node.properties['data-package-manager-source'];
	const commandValue = node.properties['data-package-manager-command'];
	const manager = typeof managerValue === 'string' ? managerValue : undefined;
	const command = typeof commandValue === 'string' ? commandValue : undefined;
	if (!isPackageManager(manager) || !command) return;

	const codeSource = node.children.find(
		(child): child is Element =>
			child.type === 'element' && classNames(child).includes('code-source')
	);
	if (!codeSource) return;
	return { node, manager, command, codeSource };
}

function isWhitespace(node: Readonly<Root['children'][number]>): boolean {
	return node.type === 'text' && /^\s*$/.test(node.value);
}

function cloneElement(node: Readonly<Element>): Element {
	return JSON.parse(JSON.stringify(node)) as Element;
}

function collectPackageManagerGroups(root: Readonly<Root>): PackageManagerGroup[] {
	const groups: PackageManagerGroup[] = [];

	function visitParent(parent: Readonly<Root | Parents>) {
		for (let index = 0; index < parent.children.length; index += 1) {
			const first = packageManagerSource(parent.children[index]);
			if (!first) {
				const child = parent.children[index];
				if (child?.type === 'element') visitParent(child);
				continue;
			}

			const sources = [first];
			const discard: Readonly<Root['children'][number]>[] = [];
			let cursor = index + 1;
			for (; cursor < parent.children.length; cursor += 1) {
				const candidate = parent.children[cursor];
				if (candidate && isWhitespace(candidate)) {
					discard.push(candidate);
					continue;
				}
				const source = candidate ? packageManagerSource(candidate) : undefined;
				if (!source) break;
				sources.push(source);
				discard.push(candidate);
			}

			const managers = new Set(sources.map((source) => source.manager));
			const complete =
				sources.length === packageManagers.length &&
				managers.size === packageManagers.length &&
				packageManagers.every((manager) => managers.has(manager.id));
			if (complete) {
				const orderedSources = packageManagers.map((manager) => {
					const source = sources.find((candidate) => candidate.manager === manager.id);
					if (!source) throw new Error(`Missing package manager ${manager.id}.`);
					return source;
				});
				groups.push({ first: first.node, discard, sources: orderedSources });
				index = cursor - 1;
			}
		}
	}

	visitParent(root);
	return groups;
}

function createPackageManagerTabs(sources: PackageManagerSource[], groupIndex: number): Element {
	const groupID = `package-manager-tabs-${groupIndex}`;
	const tabs = sources.map((source, index) => {
		const manager = packageManagers.find((item) => item.id === source.manager);
		if (!manager) throw new Error(`Unknown package manager ${source.manager}.`);
		const tabID = `${groupID}-tab-${source.manager}`;
		const panelID = `${groupID}-panel-${source.manager}`;
		return {
			source,
			manager,
			selected: index === 0,
			tabID,
			panelID
		};
	});

	return {
		type: 'element',
		tagName: 'div',
		properties: {
			className: ['code-compare', 'package-manager-tabs'],
			'data-package-manager-tabs': ''
		},
		children: [
			{
				type: 'element',
				tagName: 'header',
				properties: {},
				children: [
					{
						type: 'element',
						tagName: 'div',
						properties: {
							className: ['code-tabs'],
							role: 'tablist',
							'aria-label': 'Choose a package manager'
						},
						children: tabs.map(({ manager, selected, tabID, panelID }) => ({
							type: 'element',
							tagName: 'button',
							properties: {
								id: tabID,
								type: 'button',
								role: 'tab',
								'aria-selected': String(selected),
								'aria-controls': panelID,
								tabIndex: selected ? 0 : -1,
								'data-package-manager-tab': manager.id
							},
							children: [{ type: 'text', value: manager.label }]
						}))
					}
				]
			},
			...tabs.map(({ source, manager, selected, tabID, panelID }) => ({
				type: 'element' as const,
				tagName: 'div',
				properties: {
					className: ['code-tab-panel', 'package-manager-panel'],
					id: panelID,
					role: 'tabpanel',
					'aria-labelledby': tabID,
					'data-package-manager-panel': manager.id,
					...(selected ? {} : { hidden: true })
				},
				children: [
					createCopyButton(source.command, ['compare-copy', 'code-panel-copy'], {
						'aria-label': `Copy ${manager.label} command`
					}),
					cloneElement(source.codeSource)
				]
			}))
		]
	};
}

/**
 * Restores the labelled code-block chrome used by authored Astro pages,
 * honours explicit `{#id}` heading suffixes, and makes wide tables scroll.
 */
export const riduMarkdownComponents = {
	name: 'ridu-markdown-components',
	after(root, context) {
		collectPackageManagerGroups(root).forEach((group, index) => {
			context.replaceNode(group.first, createPackageManagerTabs(group.sources, index + 1));
			group.discard.forEach((node) => context.removeNode(node));
		});
	},
	element: [
		{
			filter: ['blockquote'],
			visit(node) {
				return createMarkdownAlert(node);
			}
		},
		{
			filter: ['h1', 'h2', 'h3', 'h4', 'h5', 'h6'],
			visit(node, context) {
				const finalChild = node.children.at(-1);
				if (finalChild?.type !== 'text') return;
				const match = finalChild.value.match(explicitHeadingId);
				if (!match) return;

				context.setProperty(finalChild, 'value', finalChild.value.replace(explicitHeadingId, ''));
				context.setProperty(node, 'id', match[1]);
			}
		},
		{
			filter: ['pre'],
			visit(node, context) {
				const metadata = readCodeMetadata(context.data[codeMetadataKey]);
				const index = Number(context.data[codeMetadataIndexKey] ?? 0);
				const block = metadata[index];
				if (!block) return;
				context.data[codeMetadataIndexKey] = index + 1;

				return createCodeBlock(node, block);
			}
		},
		{
			filter: ['table'],
			visit(node, context) {
				context.wrapNode(node, {
					type: 'element',
					tagName: 'div',
					properties: { className: ['doc-table-wrap'] },
					children: []
				});
			}
		}
	]
} satisfies HastPlugin;
