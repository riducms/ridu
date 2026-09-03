/// <reference types="bun-types" />

import { describe, expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

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
			expect(page, `${constructor} page`).toContain(`field.${constructor}`);
			expect(fixture, `${constructor} fixture`).toContain(`field.${constructor}(`);
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
