/// <reference types="bun-types" />

import { describe, expect, test } from 'bun:test';
import { readFileSync, readdirSync } from 'node:fs';
import { basename, resolve } from 'node:path';

const repositoryRoot = resolve(import.meta.dir, '../..');
const quickstart = readFileSync(resolve(import.meta.dir, 'content/docs/quickstart.md'), 'utf8');

function titledFence(source: string, language: string, title: string): string {
	const escaped = title.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
	const match = source.match(
		new RegExp(`\`\`\`${language} title="${escaped}"[^\\n]*\\n([\\s\\S]*?)\\n\`\`\``)
	);
	if (!match) throw new Error(`Missing ${language} fence titled ${title}`);
	return match[1];
}

describe('documentation examples', () => {
	test('publishes the compile-checked live validation client', () => {
		const source = readFileSync(resolve(import.meta.dir, 'content/docs/typescript-sdk.md'), 'utf8');
		const fixture = readFileSync(
			resolve(repositoryRoot, 'examples/documentation/live-validation.ts'),
			'utf8'
		).trim();
		expect(titledFence(source, 'ts', 'scripts/check-sale-price.ts')).toBe(fixture);
	});

	test('publishes the checked Go package examples, including their comments and outputs', () => {
		for (const name of ['operation', 'store', 'query', 'schema']) {
			const source = readFileSync(
				resolve(import.meta.dir, `content/docs/go-packages/${name}.md`),
				'utf8'
			);
			const fences = [...source.matchAll(/```go title="([^"]+)"[^\n]*\n([\s\S]*?)\n```/g)];
			expect(fences.length, `${name}: missing source-backed examples`).toBeGreaterThan(0);
			for (const [, title, code] of fences) {
				const fixture = readFileSync(
					resolve(repositoryRoot, 'examples/documentation/go-packages', name, basename(title)),
					'utf8'
				).trim();
				expect(code, `${name}: ${title}`).toBe(fixture);
			}
		}
	});

	test('publishes the behavior-tested hook examples', () => {
		const directory = resolve(import.meta.dir, 'content/docs/hooks');
		const source = readdirSync(directory)
			.filter((file) => file.endsWith('.md'))
			.map((file) => readFileSync(resolve(directory, file), 'utf8'))
			.join('\n');
		const files = readdirSync(resolve(repositoryRoot, 'examples/documentation/hooks')).filter(
			(file) => file.endsWith('.go') && !file.endsWith('_test.go')
		);
		for (const file of files) {
			const fixture = readFileSync(
				resolve(repositoryRoot, 'examples/documentation/hooks', file),
				'utf8'
			).trim();
			expect(titledFence(source, 'go', `content/${file}`), file).toBe(fixture);
		}
	});

	test('keeps the Payload and Ridu comparisons synchronized with their source files', () => {
		const source = readFileSync(resolve(import.meta.dir, 'content/guides/from-payload.md'), 'utf8');
		for (const [language, title, file] of [
			['ts', 'src/collections/Posts.ts', 'payload/posts.ts'],
			['go', 'content/posts.go', 'content/posts.go'],
			['ts', 'src/access/ownPosts.ts', 'payload/own-posts.ts'],
			['go', 'content/post_access.go', 'content/post_access.go']
		]) {
			const fixture = readFileSync(
				resolve(repositoryRoot, 'examples/documentation/payload-comparison', file),
				'utf8'
			).trim();
			expect(titledFence(source, language, title), title).toBe(fixture);
		}
	});

	test('publishes the compile-checked field rule examples', () => {
		for (const [page, name] of [
			['validation', 'links'],
			['callback-values', 'variants'],
			['defaults', 'defaults'],
			['defaults', 'default_author'],
			['defaults', 'default_rows']
		]) {
			const source = readFileSync(
				resolve(import.meta.dir, `content/docs/fields/${page}.md`),
				'utf8'
			);
			const fixture = readFileSync(
				resolve(repositoryRoot, `examples/documentation/field-rules/${name}.go`),
				'utf8'
			).trim();
			expect(titledFence(source, 'go', `content/${name}.go`)).toBe(fixture);
		}
	});

	test('assembles the live validation sections into one compile-checked file', () => {
		const source = readFileSync(
			resolve(import.meta.dir, 'content/docs/fields/live-validation.md'),
			'utf8'
		);
		const sections = [
			...source.matchAll(/```go title="content\/products\.go"[^\n]*\n([\s\S]*?)\n```/g)
		];
		expect(sections).toHaveLength(2);
		const assembled = sections
			.map(([, code]) => code.replace(/^\/\/ \.\.\..*$/gm, '').trim())
			.join('\n\n');
		const fixture = readFileSync(
			resolve(repositoryRoot, 'examples/documentation/field-rules/products.go'),
			'utf8'
		).trim();
		expect(assembled).toBe(fixture);
	});

	test('keeps list examples synchronized with their compiled Go files', () => {
		const source = readFileSync(resolve(import.meta.dir, 'content/docs/fields/lists.md'), 'utf8');
		const sections = [
			...source.matchAll(/```go title="content\/products\.go"[^\n]*\n([\s\S]*?)\n```/g)
		];
		expect(sections).toHaveLength(2);
		const assembled = sections
			.map(([, code]) => code.replace(/^\/\/ \.\.\..*$/gm, '').trim())
			.join('\n\n')
			.replace('import (', 'import (\n\t"strings"\n')
			.replace(
				'\t"github.com/riducms/ridu/field"',
				'\t"github.com/riducms/ridu/field"\n\t"github.com/riducms/ridu/operation"'
			);
		expect(assembled).toBe(
			readFileSync(
				resolve(repositoryRoot, 'examples/documentation/primitive-lists/catalog.go'),
				'utf8'
			).trim()
		);
		const querying = readFileSync(resolve(import.meta.dir, 'content/docs/querying.md'), 'utf8');
		expect(titledFence(querying, 'go', 'content/find_tagged.go')).toBe(
			readFileSync(
				resolve(repositoryRoot, 'examples/documentation/primitive-lists/find_tagged.go'),
				'utf8'
			).trim()
		);
	});

	test('publishes the compile-checked custom component examples', () => {
		const content = resolve(import.meta.dir, 'content/docs');
		const directory = resolve(content, 'custom-components');
		const pages = [
			{ name: 'overview', path: resolve(content, 'custom-components.md') },
			...readdirSync(directory).map((file) => ({
				name: file.replace(/\.md$/, ''),
				path: resolve(directory, file)
			}))
		];
		for (const page of pages) {
			const source = readFileSync(page.path, 'utf8');
			const fences = [
				...source.matchAll(/```(svelte|ts|go) title="([^"]+)"[^\n]*\n([\s\S]*?)\n```/g)
			];
			expect(fences.length, `${page.name}: no source-backed examples found`).toBeGreaterThan(0);
			for (const [, , title, code] of fences) {
				const file =
					title === 'admin/src/admin.config.ts' ? `admin/src/${page.name}.config.ts` : title;
				const fixture = readFileSync(
					resolve(repositoryRoot, 'examples/documentation/custom-components', file),
					'utf8'
				).trim();
				expect(code, `${page.name}: ${title}`).toBe(fixture);
			}
		}
	});

	test('publishes the tested field plugin sources', () => {
		const source = readFileSync(
			resolve(import.meta.dir, 'content/guides/custom-fields.md'),
			'utf8'
		);
		const fences = [
			...source.matchAll(/```(svelte|ts|go) title="([^"]+)"[^\n]*\n([\s\S]*?)\n```/g)
		];
		expect(fences.length, 'no source-backed field plugin examples found').toBeGreaterThan(0);
		for (const [, , title, code] of fences) {
			const file = title === 'content/brands.go' ? 'example/brands.go' : title;
			const fixture = readFileSync(
				resolve(repositoryRoot, 'examples/documentation/custom-components/color', file),
				'utf8'
			)
				.replaceAll(
					'github.com/riducms/ridu/examples/documentation/custom-components/color',
					'example.com/acme/ridu-color'
				)
				.trim();
			expect(code, title).toBe(fixture);
		}
	});

	test('sources the Quickstart SDK program from its compile-tested fixture', () => {
		const documented = titledFence(quickstart, 'ts', 'scripts/read-posts.ts');
		const fixture = readFileSync(
			resolve(repositoryRoot, 'examples/documentation/quickstart/scripts/read-posts.ts'),
			'utf8'
		).trim();
		expect(documented).toBe(fixture);
	});

	test('keeps the Quickstart field change in the resolved Go fixture', () => {
		const documented = titledFence(quickstart, 'go', 'content/posts.go')
			.replaceAll(/\s+/g, ' ')
			.trim();
		const fixture = readFileSync(
			resolve(repositoryRoot, 'examples/documentation/quickstart/content/posts.go'),
			'utf8'
		).replaceAll(/\s+/g, ' ');
		expect(fixture).toContain(documented);
	});

	test('covers both Quickstart database branches in the automation reference', () => {
		for (const database of ['sqlite', 'postgres']) {
			expect(quickstart).toContain(`--database ${database}`);
		}
	});

	test('compile-checks every built-in field page against one maintained catalog', () => {
		const fixture = readFileSync(
			resolve(repositoryRoot, 'examples/documentation/adoption/catalog.go'),
			'utf8'
		);
		for (const constructor of [
			'Array',
			'Blocks',
			'Checkbox',
			'Code',
			'Collapsible',
			'Date',
			'Email',
			'Group',
			'Join',
			'JSON',
			'Number',
			'Plugin',
			'Point',
			'Radio',
			'Relationship',
			'Row',
			'Select',
			'Slug',
			'Tabs',
			'Text',
			'Textarea',
			'UI',
			'Upload',
			'Virtual'
		]) {
			const page = readFileSync(
				resolve(import.meta.dir, `content/docs/fields/${constructor.toLowerCase()}.md`),
				'utf8'
			);
			const namespace = 'field';
			expect(page, `${constructor} page`).toContain(`${namespace}.${constructor}`);
			expect(fixture, `${constructor} fixture`).toContain(`${namespace}.${constructor}(`);
		}
	});

	test('compile-checks the adapter and plugin constructors named by adoption guides', () => {
		const fixture = readFileSync(
			resolve(repositoryRoot, 'examples/documentation/adoption/catalog.go'),
			'utf8'
		);
		for (const name of [
			'postgres.Open',
			'postgres.ProjectMigrations',
			'sqlite.Open',
			'sqlite.ProjectMigrations',
			'mongodb.Open',
			'mongodb.ProjectMigrations',
			'richtext.New',
			'seo.New',
			'formbuilder.New',
			'graphqlplugin.New',
			'mcp.New'
		]) {
			expect(fixture).toContain(name);
		}
	});
});
