/// <reference types="bun-types" />

import { describe, expect, test } from 'bun:test';
import { existsSync, readFileSync, readdirSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { referenceModules } from '@/reference';
import { capabilities } from './registry';

const websiteRoot = resolve(import.meta.dir, '../..');
const repositoryRoot = resolve(websiteRoot, '..');

function documentationSource(href: string): string | undefined {
	const url = new URL(href, 'https://ridu.invalid');
	const segments = url.pathname.split('/').filter(Boolean);
	if (segments.length < 2 || !['docs', 'guides'].includes(segments[0])) return;
	const file = resolve(
		websiteRoot,
		'src/content',
		segments[0],
		`${segments.slice(1).join('/')}.md`
	);
	return existsSync(file) ? readFileSync(file, 'utf8') : undefined;
}

function markdownFiles(directory: string): string[] {
	return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
		const path = join(directory, entry.name);
		if (entry.isDirectory()) return markdownFiles(path);
		return entry.isFile() && entry.name.endsWith('.md') ? [path] : [];
	});
}

describe('capability registry', () => {
	test('uses stable unique IDs and deterministic ordering', () => {
		const ids = capabilities.map((capability) => capability.id);
		expect(new Set(ids).size).toBe(ids.length);
		expect(ids).toEqual([...ids].sort((left, right) => left.localeCompare(right)));
		for (const id of ids) expect(id).toMatch(/^[a-z][a-z0-9]*(?:[.-][a-z0-9]+)*$/);
	});

	test('backs public claims with repository evidence and adoption pages', () => {
		const publicSymbols = new Set(
			referenceModules.flatMap((module) => module.symbols.map((symbol) => symbol.id))
		);
		for (const capability of capabilities) {
			expect(capability.owners.length, `${capability.id} owners`).toBeGreaterThan(0);
			expect(capability.evidence.length, `${capability.id} evidence`).toBeGreaterThan(0);
			expect(capability.symbols.length, `${capability.id} symbols`).toBeGreaterThan(0);
			for (const evidence of capability.evidence) {
				expect(
					existsSync(resolve(repositoryRoot, evidence)),
					`${capability.id}: ${evidence}`
				).toBeTrue();
			}
			if (capability.status === 'available' || capability.status === 'limited') {
				expect(capability.guides.length, `${capability.id} adoption guides`).toBeGreaterThan(0);
			}
			for (const guide of capability.guides) {
				expect(documentationSource(guide), `${capability.id}: ${guide}`).toBeDefined();
			}
			for (const symbol of capability.symbols) {
				expect(publicSymbols.has(symbol), `${capability.id}: ${symbol}`).toBeTrue();
			}
		}
	});

	test('requires complete new-project and existing-project setup for adapters and plugins', () => {
		for (const capability of capabilities.filter(
			(candidate) => candidate.id.startsWith('adapter.') || candidate.id.startsWith('plugin.')
		)) {
			expect(capability.setupCoverage, capability.id).toBeDefined();
			if (!capability.setupCoverage) continue;
			for (const href of Object.values(capability.setupCoverage)) {
				const source = documentationSource(href);
				expect(source, `${capability.id}: ${href}`).toBeDefined();
				const fragment = new URL(href, 'https://ridu.invalid').hash.slice(1);
				expect(source, `${capability.id}: ${href}`).toContain(`{#${fragment}}`);
			}
		}
	});

	test('rejects unknown page capability IDs and contradictory single-capability status labels', () => {
		const known = new Map(capabilities.map((capability) => [capability.id, capability]));
		const contentRoot = resolve(websiteRoot, 'src/content');
		for (const file of markdownFiles(contentRoot)) {
			const source = readFileSync(file, 'utf8');
			const ids =
				source
					.match(/^capabilities:\s*\[([^\]]*)\]/m)?.[1]
					.split(',')
					.map((value) => value.trim().replace(/^['"]|['"]$/g, ''))
					.filter(Boolean) ?? [];
			for (const id of ids) expect(known.has(id), `${file}: ${id}`).toBeTrue();
			if (ids.length !== 1) continue;
			const authoredStatus = source.match(/^\s{2}status:\s*([a-z]+)/m)?.[1];
			const registered = known.get(ids[0]!);
			if (authoredStatus && registered) expect(authoredStatus, file).toBe(registered.status);
		}
	});
});
