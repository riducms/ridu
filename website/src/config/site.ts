export const site = {
	name: 'Ridu',
	title: 'Ridu: headless CMS and app framework for Go',
	description:
		'A headless CMS you configure in Go. Define your content model in code and get an admin, a REST API, migrations and TypeScript types, built into one binary.',
	url: 'https://riducms.com',
	github: 'https://github.com/riducms/ridu',
	license: 'https://opensource.org/license/mit',
	programmingLanguage: 'Go'
} as const;

export const primaryNavigation = [
	{ key: 'docs', label: 'Docs', href: '/docs/' },
	{ key: 'reference', label: 'Reference', href: '/reference/' }
] as const;

export type ProductKey =
	| 'core'
	| 'data'
	| 'admin'
	| 'sdk'
	| 'adapters'
	| 'plugins'
	| 'cli'
	| 'guides'
	| 'reference'
	| 'github';
