export const site = {
	name: 'Ridu',
	title: 'Ridu — Go backend framework & headless CMS',
	description:
		'A config-as-code backend framework for Go with a headless CMS built in. Define your data and business logic; ship the API and admin as one binary.',
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
