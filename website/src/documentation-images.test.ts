/// <reference types="bun-types" />

import { describe, expect, test } from 'bun:test';
import { existsSync, readFileSync, readdirSync } from 'node:fs';
import { basename, resolve } from 'node:path';

interface CaptureField {
	slug: string;
	collection: string;
	documentId: string;
	route: string;
	selector: string;
	state: string;
	output: string;
	documentationOwner: string;
}

interface CaptureManifest {
	version: number;
	fields: CaptureField[];
}

const repositoryRoot = resolve(import.meta.dir, '../..');
const manifestPath = resolve(repositoryRoot, 'playground/documentation/capture-manifest.json');
const manifest = JSON.parse(readFileSync(manifestPath, 'utf8')) as CaptureManifest;
const expectedSlugs = [
	'array',
	'blocks',
	'checkbox',
	'code',
	'collapsible',
	'date',
	'email',
	'group',
	'join',
	'json',
	'number',
	'plugin',
	'point',
	'radio',
	'relationship',
	'row',
	'select',
	'slug',
	'tabs',
	'text',
	'textarea',
	'ui',
	'upload',
	'virtual'
];

describe('documentation image capture', () => {
	test('owns one complete, collision-free capture for every built-in field', () => {
		expect(manifest.version).toBe(1);
		expect(manifest.fields.map(({ slug }) => slug).sort()).toEqual(expectedSlugs);
		expect(
			new Set(manifest.fields.map(({ route, selector }) => `${route}\n${selector}`)).size
		).toBe(24);
		expect(new Set(manifest.fields.map(({ output }) => output)).size).toBe(24);

		for (const field of manifest.fields) {
			expect(field.route, `${field.slug} route`).toBe(
				`/admin/collections/${field.collection}/${field.documentId}?locale=en`
			);
			expect(field.selector, `${field.slug} selector`).toMatch(/^\[data-/);
			expect(field.state, `${field.slug} prepared state`).not.toBe('');
			expect(field.documentationOwner).toBe(`website/src/content/docs/fields/${field.slug}.md`);
		}
	});

	test('keeps every captured asset linked with useful alt text and a caption', () => {
		for (const field of manifest.fields) {
			const asset = resolve(repositoryRoot, field.output);
			const pagePath = resolve(repositoryRoot, field.documentationOwner);
			expect(existsSync(asset), `${field.slug} asset`).toBeTrue();
			expect(existsSync(pagePath), `${field.slug} page`).toBeTrue();

			const png = readFileSync(asset);
			expect(png.subarray(1, 4).toString(), `${field.slug} is PNG`).toBe('PNG');
			expect(png.readUInt32BE(16), `${field.slug} width`).toBeGreaterThan(320);
			expect(png.readUInt32BE(20), `${field.slug} height`).toBeGreaterThan(60);

			const page = readFileSync(pagePath, 'utf8');
			const expectedTarget = `../../../../../${field.output}`;
			const figure = page.match(/!\[([^\]]+)\]\(([^)]+)\)\n\n_([^\n]+)_/);
			expect(figure, `${field.slug} figure`).not.toBeNull();
			expect(figure?.[1].trim().length, `${field.slug} alt text`).toBeGreaterThan(20);
			expect(figure?.[2], `${field.slug} image target`).toBe(expectedTarget);
			expect(figure?.[3].trim().length, `${field.slug} caption`).toBeGreaterThan(20);
		}
	});

	test('has no missing or orphaned focused field image', () => {
		const captured = manifest.fields.map(({ output }) => basename(output)).sort();
		const committed = readdirSync(resolve(repositoryRoot, 'docs/assets/fields'))
			.filter((name) => name.endsWith('.png'))
			.sort();
		expect(committed).toEqual(captured);
	});

	test('documents generated playground provenance', () => {
		const provenance = readFileSync(resolve(repositoryRoot, 'docs/assets/README.md'), 'utf8');
		expect(provenance).toContain('playground/documentation/');
		expect(provenance).not.toContain('tests/contracts/admin_server');
	});
});
