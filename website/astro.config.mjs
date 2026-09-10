import { defineConfig } from 'astro/config';
import { satteri } from '@astrojs/markdown-satteri';
import sitemap from '@astrojs/sitemap';
import UnoCSS from '@unocss/astro';
import Icons from 'unplugin-icons/vite';
import { riduCodeTheme } from './src/config/code-theme.ts';
import { riduCodeLineTransformer } from './src/plugins/code-line-transformer.ts';
import { riduHeadingPermalinks } from './src/plugins/heading-permalinks.ts';
import { riduMarkdownCodeMetadata, riduMarkdownComponents } from './src/plugins/ridu-markdown.ts';
import { site } from './src/config/site.ts';

export default defineConfig({
	site: process.env.SITE_URL ?? site.url,
	integrations: [
		UnoCSS(),
		sitemap({
			filter: (page) => new URL(page).pathname !== '/search/'
		})
	],
	markdown: {
		processor: satteri({
			mdastPlugins: [riduMarkdownCodeMetadata],
			hastPlugins: [riduMarkdownComponents, riduHeadingPermalinks]
		}),
		shikiConfig: {
			theme: riduCodeTheme,
			transformers: [riduCodeLineTransformer],
			wrap: false
		}
	},
	vite: {
		build: {
			// Shared chrome scripts belong in cacheable assets, not repeated in every generated page.
			assetsInlineLimit: 0
		},
		plugins: [Icons({ compiler: 'astro' })]
	}
});
