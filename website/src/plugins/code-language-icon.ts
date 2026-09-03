import { loadNodeIcon } from '@iconify/utils/lib/loader/node-loader';
import type { Element } from 'hast';
import { htmlToHast } from 'satteri';
import {
	codeLanguageIcons,
	resolveCodeLanguage,
	type CodeLanguage
} from '@/components/code/code-language';

const loadedIcons = await Promise.all(
	(
		Object.entries(codeLanguageIcons) as [CodeLanguage, (typeof codeLanguageIcons)[CodeLanguage]][]
	).map(async ([language, source]) => {
		const svg = await loadNodeIcon(source.collection, source.name, { cwd: process.cwd() });
		if (!svg) {
			throw new Error(`Unable to load the ${source.collection}:${source.name} code-language icon.`);
		}
		return [language, svg] as const;
	})
);

const iconMarkup = new Map<CodeLanguage, string>(loadedIcons);

export function createCodeLanguageIcon(language: string | null | undefined): Element {
	const resolved = resolveCodeLanguage(language);
	const svg = iconMarkup.get(resolved) ?? iconMarkup.get('text');
	if (!svg) throw new Error('The fallback code-language icon is unavailable.');

	const tree = htmlToHast(`<span class="code-language-icon" aria-hidden="true">${svg}</span>`, {
		fragment: true
	});
	if (!('children' in tree)) throw new Error('The code-language icon did not produce a tree.');
	const icon = tree.children[0];
	if (icon?.type !== 'element')
		throw new Error('The code-language icon did not produce an element.');
	return icon;
}
