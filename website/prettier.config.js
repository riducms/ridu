/** @type {import('prettier').Config} */
export default {
	useTabs: true,
	singleQuote: true,
	trailingComma: 'none',
	printWidth: 100,
	plugins: ['prettier-plugin-astro', 'prettier-plugin-svelte'],
	overrides: [
		{ files: '*.astro', options: { parser: 'astro' } },
		// Keep embedded examples narrow and their markup readable on separate lines.
		{
			files: '*.md',
			options: { printWidth: 70, htmlWhitespaceSensitivity: 'ignore' }
		}
	]
};
