/// <reference types="bun-types" />

import { describe, expect, test } from 'bun:test';
import { readFileSync, readdirSync } from 'node:fs';
import path from 'node:path';
import GithubSlugger from 'github-slugger';
import { referenceModules } from '@/reference';

interface RouteTarget {
	file: string;
	anchors: Set<string>;
}

interface InternalLink {
	file: string;
	href: string;
}

const websiteRoot = path.resolve(import.meta.dir, '../..');
const sourceRoot = path.join(websiteRoot, 'src');

describe('documentation links', () => {
	test('resolve every authored docs, guide, and reference link', () => {
		const routes = documentationRoutes();
		const links = sourceFiles(sourceRoot).flatMap((file) => internalLinks(file));

		for (const { file, href } of links) {
			const [route, fragment] = href.split('#', 2);
			const target = routes.get(route);
			expect(target, `${relative(file)} links to missing route ${href}`).toBeDefined();
			if (!target || !fragment || target.anchors.size === 0) continue;
			expect(
				target.anchors.has(fragment),
				`${relative(file)} links to missing anchor #${fragment} in ${relative(target.file)}`
			).toBeTrue();
		}
	});
});

function documentationRoutes(): Map<string, RouteTarget> {
	const routes = new Map<string, RouteTarget>();
	for (const area of ['docs', 'guides'] as const) {
		const directory = path.join(sourceRoot, 'content', area);
		for (const file of sourceFiles(directory).filter((candidate) => candidate.endsWith('.md'))) {
			const slug = path.relative(directory, file).replace(/\.md$/, '').split(path.sep).join('/');
			routes.set(`/${area}/${slug}/`, { file, anchors: markdownAnchors(file) });
		}
		const landing = path.join(sourceRoot, 'pages', area, 'index.astro');
		routes.set(`/${area}/`, { file: landing, anchors: htmlAnchors(landing) });
	}

	const projectStructure = path.join(sourceRoot, 'pages/guides/project-structure/index.astro');
	routes.set('/guides/project-structure/', {
		file: projectStructure,
		anchors: htmlAnchors(projectStructure)
	});

	routes.set('/reference/', {
		file: path.join(sourceRoot, 'pages/reference/index.astro'),
		anchors: new Set()
	});
	for (const module of referenceModules) {
		routes.set(`/reference/${module.slug}/`, { file: module.slug, anchors: new Set() });
		for (const symbol of module.symbols) {
			routes.set(`/reference/${module.slug}/${symbol.slug}/`, {
				file: `${module.slug}/${symbol.slug}`,
				anchors: new Set()
			});
		}
	}
	return routes;
}

function sourceFiles(directory: string): string[] {
	return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
		const candidate = path.join(directory, entry.name);
		if (entry.isDirectory()) return sourceFiles(candidate);
		return /\.(?:astro|md|ts)$/.test(entry.name) ? [candidate] : [];
	});
}

function internalLinks(file: string): InternalLink[] {
	const source = readFileSync(file, 'utf8');
	const patterns = [
		/\]\((\/(?:docs|guides|reference)\/[^)\s]*)\)/g,
		/\bhref=["'](\/(?:docs|guides|reference)\/[^"']*)["']/g,
		/\bhref:\s*["'](\/(?:docs|guides|reference)\/[^"']*)["']/g
	];
	return patterns.flatMap((pattern) =>
		[...source.matchAll(pattern)].map((match) => ({ file, href: match[1] }))
	);
}

function markdownAnchors(file: string): Set<string> {
	const anchors = htmlAnchors(file);
	const slugger = new GithubSlugger();
	for (const match of readFileSync(file, 'utf8').matchAll(/^(#{2,6})\s+(.+)$/gm)) {
		const explicit = match[2].match(/\s+\{#([^}]+)\}\s*$/)?.[1];
		const heading = match[2]
			.replace(/\s+\{#[^}]+\}\s*$/, '')
			.replace(/\[([^\]]+)\]\([^)]+\)/g, '$1')
			.replace(/[`*_~]/g, '')
			.trim();
		anchors.add(explicit ?? slugger.slug(heading));
	}
	return anchors;
}

function htmlAnchors(file: string): Set<string> {
	return new Set(
		[...readFileSync(file, 'utf8').matchAll(/\bid=["']([^"']+)["']/g)].map((match) => match[1])
	);
}

function relative(file: string): string {
	return path.relative(websiteRoot, file);
}
