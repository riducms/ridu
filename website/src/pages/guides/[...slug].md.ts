import type { APIRoute } from 'astro';
import { getDocumentationPages } from '@/config/documentation';
import { documentationPageMarkdown } from '@/documentation-markdown';

export async function getStaticPaths() {
	return (await getDocumentationPages())
		.filter((page) => page.area === 'guides')
		.map((page) => ({
			params: { slug: page.slug },
			props: {
				markdown: documentationPageMarkdown({
					title: page.title,
					description: page.description,
					href: page.href,
					body: page.entry.body ?? '',
					sourcePath: `website/src/content/guides/${page.slug}.md`
				})
			}
		}));
}

export const GET: APIRoute = ({ props }) =>
	new Response(props.markdown, {
		headers: { 'Content-Type': 'text/markdown; charset=utf-8' }
	});
