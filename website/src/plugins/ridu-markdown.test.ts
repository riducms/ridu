import { describe, expect, it } from 'bun:test';
import { readFileSync } from 'node:fs';
import { markdownToHtml } from 'satteri';
import { resolveCodeLanguage } from '@/components/code/code-language';
import { packageManagers } from '@/components/code/package-manager';
import {
	createMarkdownAlert,
	riduMarkdownCodeMetadata,
	riduMarkdownComponents
} from './ridu-markdown';

const packageManagerMarkdown = [
	'```bash title="Bun" package-manager="bun"',
	'bun add @riducms/sdk',
	'```',
	'',
	'```bash title="npm" package-manager="npm"',
	'npm install @riducms/sdk',
	'```',
	'',
	'```bash title="pnpm" package-manager="pnpm"',
	'pnpm add @riducms/sdk',
	'```',
	'',
	'```bash title="Yarn" package-manager="yarn"',
	'yarn add @riducms/sdk',
	'```'
].join('\n');

describe('createMarkdownAlert', () => {
	it('turns GitHub alert blockquotes into labelled callout variants', () => {
		const alert = createMarkdownAlert({
			type: 'element',
			tagName: 'blockquote',
			properties: {},
			children: [
				{
					type: 'element',
					tagName: 'p',
					properties: {},
					children: [{ type: 'text', value: '[!WARNING]\nBack up the database first.' }]
				}
			]
		});

		expect(alert?.tagName).toBe('aside');
		expect(alert?.properties).toEqual({
			className: ['markdown-alert', 'markdown-alert-warning'],
			'data-callout': 'warning',
			'aria-label': 'Warning'
		});
		expect(alert?.children).toEqual([
			{
				type: 'element',
				tagName: 'p',
				properties: { className: ['markdown-alert-title'] },
				children: [{ type: 'text', value: 'Warning' }]
			},
			{
				type: 'element',
				tagName: 'p',
				properties: {},
				children: [{ type: 'text', value: 'Back up the database first.' }]
			}
		]);
	});

	it('leaves ordinary blockquotes untouched', () => {
		expect(
			createMarkdownAlert({
				type: 'element',
				tagName: 'blockquote',
				properties: {},
				children: [
					{
						type: 'element',
						tagName: 'p',
						properties: {},
						children: [{ type: 'text', value: 'A quotation.' }]
					}
				]
			})
		).toBeUndefined();
	});

	it('transforms the whitespace-padded blockquote tree produced by Markdown', () => {
		const { html } = markdownToHtml('> [!TIP]\n> Generate after every schema change.', {
			hastPlugins: [
				{
					name: 'ridu-alert-test',
					element: {
						filter: ['blockquote'],
						visit(node) {
							return createMarkdownAlert(node);
						}
					}
				}
			]
		});

		expect(html).toContain('<aside class="markdown-alert markdown-alert-tip"');
		expect(html).toContain('data-callout="tip"');
		expect(html).toContain('<p class="markdown-alert-title">Tip</p>');
		expect(html).toContain('<p>Generate after every schema change.</p>');
		expect(html).not.toContain('[!TIP]');
	});
});

describe('riduMarkdownCodeMetadata', () => {
	it('renders a language icon before a titled fence label', () => {
		const { html } = markdownToHtml('```ts title="server.ts"\nconst port = 3000;\n```', {
			mdastPlugins: [riduMarkdownCodeMetadata],
			hastPlugins: [riduMarkdownComponents]
		});

		const icon = '<span class="code-language-icon" aria-hidden="true"><svg';
		expect(html).toContain(icon);
		expect(html.indexOf(icon)).toBeLessThan(html.indexOf('server.ts'));
	});

	it('shares aliases and the text fallback with authored code blocks', () => {
		expect(resolveCodeLanguage('typescript')).toBe('ts');
		expect(resolveCodeLanguage('shell')).toBe('bash');
		expect(resolveCodeLanguage('unregistered-language')).toBe('text');
		expect(resolveCodeLanguage(undefined)).toBe('text');
	});

	it('normalizes fenced source before highlighting and copying', () => {
		const data: Record<string, unknown> = {};
		const updates: Array<{ key: string; value: unknown }> = [];
		const node = {
			type: 'code',
			lang: 'go',
			meta: 'title="content/example.go"',
			value: '\n\troot\n\t\tchild\n'
		};

		riduMarkdownCodeMetadata.code(
			node as never,
			{
				data,
				setProperty(_target: unknown, key: string, value: unknown) {
					updates.push({ key, value });
				}
			} as never
		);

		expect(updates).toEqual([{ key: 'value', value: 'root\n  child' }]);
		expect(data.riduCodeMetadata).toEqual([
			{
				code: 'root\n  child',
				label: 'content/example.go',
				language: 'go'
			}
		]);
	});

	it('keeps presentation metadata out of copied source', () => {
		const data: Record<string, unknown> = {};
		const node = {
			type: 'code',
			lang: 'go',
			meta: 'title="content/example.go" add={2-3} highlight={4}',
			value: 'package content\n\nfunc Example() {\n}\n'
		};

		riduMarkdownCodeMetadata.code(
			node as never,
			{
				data,
				setProperty() {}
			} as never
		);

		expect(data.riduCodeMetadata).toEqual([
			{
				code: 'package content\n\nfunc Example() {\n}',
				label: 'content/example.go',
				language: 'go'
			}
		]);
	});

	it('groups a complete package-manager run into one accessible command switcher', () => {
		const { html } = markdownToHtml(packageManagerMarkdown, {
			mdastPlugins: [riduMarkdownCodeMetadata],
			hastPlugins: [riduMarkdownComponents]
		});

		expect(html).toContain('class="code-compare package-manager-tabs"');
		expect(html).toContain('data-package-manager-tabs=""');
		expect(html.match(/role="tab"/g)).toHaveLength(4);
		expect(html.match(/role="tabpanel"/g)).toHaveLength(4);
		expect(html.match(/data-copy-code=""/g)).toHaveLength(4);
		expect(html).not.toContain('class="package-manager-icon"');
		expect(html).not.toContain('class="code-block"');
		expect(html).not.toContain('data-package-manager-source');
		expect(html).toContain(
			'data-package-manager-panel="npm"><button class="compare-copy code-panel-copy"'
		);
		expect(html).toContain(
			'aria-selected="true" aria-controls="package-manager-tabs-1-panel-npm" tabindex="0"'
		);
		expect(html).toContain(
			'id="package-manager-tabs-1-tab-npm" type="button" role="tab" aria-selected="true"'
		);
		expect(html).toContain(
			'id="package-manager-tabs-1-panel-npm" role="tabpanel" aria-labelledby="package-manager-tabs-1-tab-npm"'
		);
		expect(html).toContain('data-package-manager-panel="npm"');
		expect(html).toContain('hidden');

		const commands = [
			'npm install @riducms/sdk',
			'bun add @riducms/sdk',
			'pnpm add @riducms/sdk',
			'yarn add @riducms/sdk'
		];
		for (let index = 1; index < commands.length; index += 1) {
			expect(html.indexOf(commands[index]!)).toBeGreaterThan(html.indexOf(commands[index - 1]!));
		}
	});

	it('groups every complete package-manager run in the rendered Quickstart', () => {
		const markdown = readFileSync(
			new URL('../content/docs/quickstart.md', import.meta.url),
			'utf8'
		).replace(/^---\n[\s\S]*?\n---\n/, '');
		const { html } = markdownToHtml(markdown, {
			mdastPlugins: [riduMarkdownCodeMetadata],
			hastPlugins: [riduMarkdownComponents]
		});
		const sourceCount = markdown.match(/package-manager=/g)?.length ?? 0;

		expect(sourceCount % packageManagers.length).toBe(0);
		expect(html.match(/data-package-manager-tabs=""/g)).toHaveLength(
			sourceCount / packageManagers.length
		);
		expect(html.match(/class="compare-copy code-panel-copy"/g)).toHaveLength(sourceCount);
		expect(html).not.toContain('data-package-manager-source');
	});

	it('leaves incomplete package-manager runs as ordinary code blocks', () => {
		const incomplete = packageManagerMarkdown.split('\n\n').slice(0, 3).join('\n\n');
		const { html } = markdownToHtml(incomplete, {
			mdastPlugins: [riduMarkdownCodeMetadata],
			hastPlugins: [riduMarkdownComponents]
		});

		expect(html).not.toContain('data-package-manager-tabs');
		expect(html.match(/class="code-block"/g)).toHaveLength(3);
		expect(html).toContain('bun add @riducms/sdk');
		expect(html).toContain('pnpm add @riducms/sdk');
	});

	it('marks every public Bun package acquisition or initializer for package-manager tabs', async () => {
		const content = new Bun.Glob('src/content/**/*.md');
		let packageCommandCount = 0;

		for await (const relativePath of content.scan({ cwd: process.cwd(), onlyFiles: true })) {
			const source = await Bun.file(relativePath).text();
			for (const fence of source.matchAll(/```(?:bash|sh|shell)([^\n]*)\n([\s\S]*?)```/g)) {
				if (!/^bun (?:add|create)\b/m.test(fence[2] ?? '')) continue;
				packageCommandCount += 1;
				expect(fence[1]).toContain('package-manager="bun"');
			}
		}

		expect(packageCommandCount).toBeGreaterThan(0);
	});
});
