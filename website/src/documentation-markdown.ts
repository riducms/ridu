import { posix } from 'node:path';

export const publicDocumentationOrigin = 'https://riducms.com';

export function normalizeDocumentationMarkdown(
	markdown: string,
	sourcePath: string,
	origin = publicDocumentationOrigin
): string {
	const withSourceImages = markdown.replace(
		/!\[([^\]]*)\]\(([^)\s]+)\)/g,
		(_match, label: string, target: string) => {
			if (target.startsWith('https://') || target.startsWith('/')) {
				return `![${label}](${target})`;
			}
			const resolved = posix.normalize(posix.join(posix.dirname(sourcePath), target));
			if (resolved === '..' || resolved.startsWith('../')) {
				throw new Error(
					`${sourcePath} image target ${JSON.stringify(target)} escapes the repository`
				);
			}
			return `![${label}](https://raw.githubusercontent.com/riducms/ridu/main/${resolved})`;
		}
	);
	return withSourceImages.replace(/\]\((\/[^)\s]+)\)/g, `](${origin}$1)`);
}

export function documentationPageMarkdown(page: {
	title: string;
	description: string;
	href: string;
	body: string;
	sourcePath: string;
}): string {
	return `# ${page.title}\n\n> ${page.description}\n\nCanonical URL: ${publicDocumentationOrigin}${page.href}\n\n${normalizeDocumentationMarkdown(page.body, page.sourcePath)}\n`;
}
