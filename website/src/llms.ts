import { readdirSync, readFileSync } from 'node:fs';
import { join, relative, resolve, sep } from 'node:path';
import { normalizeDocumentationMarkdown } from './documentation-markdown';
import { referenceModules } from './reference';
import { site } from './config/site';

const websiteRoot = process.cwd();
const repositoryRoot = resolve(websiteRoot, '..');
const publicOrigin = 'https://riducms.com';

export const frameworkVersion = (
	JSON.parse(readFileSync(resolve(repositoryRoot, 'packages/cli/package.json'), 'utf8')) as {
		version?: string;
	}
).version;

if (!frameworkVersion) throw new Error('packages/cli/package.json is missing its release version');

type DocumentationPage = {
	area: 'docs' | 'guides';
	slug: string;
	href: string;
	title: string;
	description: string;
	order: number;
	body: string;
	sourcePath: string;
};

function frontmatterValue(frontmatter: string, key: string): string | undefined {
	const match = frontmatter.match(
		new RegExp(`^${key}:\\s*(?:'([^']*)'|"([^"]*)"|([^\\n]+))$`, 'm')
	);
	return match?.[1] ?? match?.[2] ?? match?.[3]?.trim();
}

function documentationPages(): DocumentationPage[] {
	const pages: DocumentationPage[] = [];
	for (const area of ['docs', 'guides'] as const) {
		const directory = resolve(websiteRoot, 'src/content', area);
		const files: string[] = [];
		const visit = (current: string) => {
			for (const entry of readdirSync(current, { withFileTypes: true })) {
				const path = join(current, entry.name);
				if (entry.isDirectory()) visit(path);
				else if (entry.isFile() && entry.name.endsWith('.md')) files.push(path);
			}
		};
		visit(directory);
		for (const file of files.sort()) {
			const source = readFileSync(file, 'utf8');
			const match = source.match(/^---\r?\n([\s\S]*?)\r?\n---\r?\n([\s\S]*)$/);
			const filename = relative(directory, file).split(sep).join('/');
			if (!match) throw new Error(`${area}/${filename} is missing YAML frontmatter`);
			const title = frontmatterValue(match[1], 'title');
			const description = frontmatterValue(match[1], 'description');
			const order = Number(frontmatterValue(match[1], 'order'));
			if (!title || !description || !Number.isSafeInteger(order)) {
				throw new Error(`${area}/${filename} has incomplete LLM documentation metadata`);
			}
			const slug = filename.replace(/\.md$/, '');
			pages.push({
				area,
				slug,
				href: `/${area}/${slug}/`,
				title,
				description,
				order,
				body: match[2].trim(),
				sourcePath: `website/src/content/${area}/${filename}`
			});
		}
	}
	return pages.sort(
		(left, right) =>
			(left.area === right.area ? 0 : left.area === 'docs' ? -1 : 1) ||
			left.order - right.order ||
			left.slug.localeCompare(right.slug)
	);
}

function referenceIndex(): string {
	const links = referenceModules
		.map(
			(module) => `- [${module.name}](${publicOrigin}/reference/${module.slug}/): ${module.summary}`
		)
		.join('\n');
	return `## API reference\n\n${links}`;
}

function referenceFull(): string {
	return referenceModules
		.map((module) => {
			const symbols = module.symbols
				.map((symbol) => {
					const details = symbol.details.length > 0 ? `\n\n${symbol.details.join('\n\n')}` : '';
					const example = symbol.example
						? `\n\n\`\`\`${module.language === 'ts' ? 'ts' : module.language}\n${symbol.example}\n\`\`\``
						: '';
					return `## ${symbol.name}\n\nCanonical URL: ${publicOrigin}/reference/${module.slug}/${symbol.slug}/\n\nKind: ${symbol.kind}\n\n${symbol.summary}\n\n\`\`\`${module.language === 'ts' ? 'ts' : module.language}\n${symbol.signature}\n\`\`\`${details}${example}`;
				})
				.join('\n\n');
			return `# API Reference: ${module.name}\n\nCanonical URL: ${publicOrigin}/reference/${module.slug}/\n\nPackage: ${module.packageName}\n\n${module.summary}\n\n${symbols}`;
		})
		.join('\n\n---\n\n');
}

function sourceMarkdown(page: DocumentationPage): string {
	return normalizeDocumentationMarkdown(page.body, page.sourcePath, publicOrigin);
}

export async function buildLLMSIndex(): Promise<string> {
	const pages = documentationPages();
	const groups = new Map<string, DocumentationPage[]>();
	for (const page of pages) {
		const label = page.area === 'guides' ? 'Guides' : 'Documentation';
		groups.set(label, [...(groups.get(label) ?? []), page]);
	}
	const sections = [...groups].map(([label, entries]) => {
		const links = entries
			.map((page) => `- [${page.title}](${publicOrigin}${page.href}): ${page.description}`)
			.join('\n');
		return `## ${label}\n\n${links}`;
	});
	return `# Ridu\n\n> ${site.description}\n\nRelease documentation version: ${frameworkVersion}\n\n- [Ridu website](${publicOrigin}/)\n- [Source code](${site.github})\n- [Documentation map](${publicOrigin}/docs/)\n- [Guides map](${publicOrigin}/guides/)\n- [Complete documentation search](${publicOrigin}/search/)\n- [Complete LLM documentation](${publicOrigin}/llms-full.txt)\n- [Versioned complete documentation](${publicOrigin}/v/${frameworkVersion}/llms-full.txt)\n\n${sections.join('\n\n')}\n\n${referenceIndex()}\n`;
}

export async function buildLLMSFull(): Promise<string> {
	const pages = documentationPages();
	const documents = pages.map((page) => {
		return `# ${page.title}\n\nCanonical URL: ${publicOrigin}${page.href}\n\n${sourceMarkdown(page)}`;
	});
	return `${await buildLLMSIndex()}\n\n${documents.join('\n\n---\n\n')}\n\n---\n\n${referenceFull()}\n`;
}
