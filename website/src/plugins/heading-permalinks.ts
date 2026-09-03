import type { SatteriProcessorOptions } from '@astrojs/markdown-satteri';
import GithubSlugger from 'github-slugger';
import type { Element } from 'hast';

type HastPlugin = NonNullable<SatteriProcessorOptions['hastPlugins']>[number];

const headingTags = ['h2', 'h3', 'h4'];
const sluggerDataKey = 'riduHeadingPermalinkSlugger';

function getSlugger(data: Record<string, unknown>): GithubSlugger {
	const current = data[sluggerDataKey];
	if (current instanceof GithubSlugger) return current;

	const slugger = new GithubSlugger();
	data[sluggerDataKey] = slugger;
	return slugger;
}

function hasPermalink(node: Readonly<Element>): boolean {
	return node.children.some((child) => {
		if (child.type !== 'element' || child.tagName !== 'a') return false;
		const className = child.properties.className;
		return Array.isArray(className) && className.includes('heading-permalink');
	});
}

/** Creates the visible, keyboard-focusable section link appended to a heading. */
export function createHeadingPermalink(id: string, headingText: string): Element {
	return {
		type: 'element',
		tagName: 'a',
		properties: {
			href: `#${id}`,
			className: ['heading-permalink'],
			'aria-label': `Link to “${headingText}” section`,
			title: 'Link to this section'
		},
		children: []
	};
}

/**
 * Gives level-two through level-four document headings stable IDs and appends
 * an accessible permalink. Existing authored IDs always take precedence.
 *
 * The plugin owns implicit IDs because Astro's heading-ID pass runs after
 * user-supplied Sätteri plugins. `github-slugger` keeps these IDs identical to
 * Astro's generated heading IDs, including duplicate suffixes.
 */
export const riduHeadingPermalinks = {
	name: 'ridu-heading-permalinks',
	element: {
		filter: headingTags,
		visit(node, context) {
			if (hasPermalink(node)) return;

			const headingText = context.textContent(node);
			const authoredId = node.properties.id;
			const id =
				typeof authoredId === 'string' ? authoredId : getSlugger(context.data).slug(headingText);

			if (typeof authoredId !== 'string') context.setProperty(node, 'id', id);
			context.appendChild(node, createHeadingPermalink(id, headingText));
		}
	}
} satisfies HastPlugin;
