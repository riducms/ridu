export const site = {
	name: 'Ridu',
	title: 'Ridu — a config-as-code CMS for Go',
	description:
		'Ridu is a config-as-code headless CMS for Go. Define access rules, hooks and blocks in Go; deploy the API and embedded admin as one binary.',
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
