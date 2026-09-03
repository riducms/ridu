import type { ThemeRegistrationRaw } from 'shiki';

export const riduCodeTheme: ThemeRegistrationRaw = {
	name: 'ridu-dark',
	type: 'dark',
	colors: {
		'editor.background': '#0d0a0c',
		'editor.foreground': '#e9e6e1'
	},
	settings: [
		{
			settings: { foreground: '#e9e6e1', background: '#0d0a0c' }
		},
		{
			scope: ['comment', 'punctuation.definition.comment'],
			settings: { foreground: '#7d7a8c', fontStyle: 'italic' }
		},
		{
			scope: ['keyword', 'storage', 'storage.type', 'storage.modifier'],
			settings: { foreground: '#ff7eb6' }
		},
		{
			scope: ['string', 'string.quoted', 'string.template'],
			settings: { foreground: '#b5e48c' }
		},
		{
			scope: ['constant.numeric', 'constant.language', 'constant.character'],
			settings: { foreground: '#c4a5ff' }
		},
		{
			scope: ['entity.name.function', 'support.function', 'meta.function-call'],
			settings: { foreground: '#8fb4ff', fontStyle: 'bold' }
		},
		{
			scope: ['entity.name.type', 'entity.name.class', 'support.type', 'support.class'],
			settings: { foreground: '#7fdbec', fontStyle: 'italic' }
		},
		{
			scope: ['variable.parameter', 'meta.function.parameters variable'],
			settings: { foreground: '#ffb977', fontStyle: 'italic' }
		},
		{
			scope: ['variable.other.property', 'variable.other.member', 'meta.object-literal.key'],
			settings: { foreground: '#c4a5ff' }
		},
		{
			scope: ['punctuation', 'meta.brace', 'meta.delimiter'],
			settings: { foreground: '#80807e' }
		},
		{
			scope: ['variable', 'identifier'],
			settings: { foreground: '#e9e6e1' }
		}
	]
};
