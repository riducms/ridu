export const site = {
	name: 'Ridu',
	title: 'Ridu — a CMS that ships like Go software',
	description:
		'Define content and application behaviour in Go. Generate the contracts and embedded admin, then ship one application binary.',
	url: 'https://riducms.com',
	github: 'https://github.com/riducms/ridu'
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
