import { presetRiduUtilities } from '@riducms/build/uno';
import { defineConfig, presetWind4, transformerVariantGroup } from 'unocss';
import { presetSiteSearch } from './src/components/chrome/site-search.preset';

const pageViewportHeight = 'calc(100vh - var(--page-header-height))';

const siteColors = {
	canvas: '#0d0a0c',
	surface: '#121012',
	'surface-raised': '#181417',
	'surface-hover': '#1d181c',
	ink: '#eaeae8',
	'ink-soft': '#aaa5a8',
	'ink-faint': '#858085',
	line: '#28282b',
	'line-strong': '#363238',
	accent: '#f34794',
	'accent-soft': 'rgba(243, 71, 148, 0.11)',
	blue: '#8fb4ff',
	cyan: '#7fdbec',
	green: '#b5e48c',
	violet: '#c4b5fd'
};

const siteFonts = {
	heading: "'Questrial', ui-sans-serif, system-ui, sans-serif",
	body: "'Geist Variable', ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif",
	mono: "'Martian Mono Variable', 'Martian Mono', ui-monospace, monospace"
};

export default defineConfig({
	// Keep reusable framework utilities separate from website component shortcuts.
	content: {
		pipeline: {
			include: [/\.(astro|html|md|mdx|[jt]sx?|svelte)($|\?)/]
		}
	},
	presets: [presetRiduUtilities(), presetSiteSearch, presetWind4()],
	// Authored CSS consumes these variables directly, so keep their generation independent of utility extraction.
	safelist: [
		...Object.keys(siteColors).map((color) => `colors:${color}`),
		...Object.keys(siteFonts).map((font) => `font:${font}`)
	],
	theme: {
		colors: siteColors,
		font: siteFonts
	},
	rules: [
		['h-header', { height: 'var(--header-height)' }],
		['h-page', { height: pageViewportHeight }],
		['min-h-page', { 'min-height': pageViewportHeight }],
		['max-h-page', { 'max-height': pageViewportHeight }],
		['w-site', { width: 'min(calc(100% - 40px), 1200px)' }],
		['w-docs-gutter', { width: 'calc(100% - 40px)' }],
		['w-docs', { width: 'min(calc(100% - 32px), 1600px)' }],
		['w-docs-wide', { width: 'min(calc(100% - 32px), 1760px)' }],
		['w-reference', { width: 'min(100%, 1600px)' }]
	],
	shortcuts: [
		{
			eyebrow: 'm-0 font-mono text-[10.5px] text-ink-faint font-[520] tracking-[0.12em] uppercase',
			'muted-link': 'text-ink-soft no-underline hover:text-ink'
		}
	],
	transformers: [transformerVariantGroup()]
});
