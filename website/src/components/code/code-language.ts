export const codeLanguageIcons = {
	bash: { collection: 'simple-icons', name: 'gnubash' },
	css: { collection: 'simple-icons', name: 'css' },
	go: { collection: 'simple-icons', name: 'go' },
	graphql: { collection: 'simple-icons', name: 'graphql' },
	html: { collection: 'simple-icons', name: 'html5' },
	js: { collection: 'simple-icons', name: 'javascript' },
	json: { collection: 'simple-icons', name: 'json' },
	markdown: { collection: 'simple-icons', name: 'markdown' },
	md: { collection: 'simple-icons', name: 'markdown' },
	svelte: { collection: 'simple-icons', name: 'svelte' },
	text: { collection: 'mdi', name: 'text-box-outline' },
	toml: { collection: 'simple-icons', name: 'toml' },
	ts: { collection: 'simple-icons', name: 'typescript' }
} as const;

export type CodeLanguage = keyof typeof codeLanguageIcons;

const codeLanguageAliases: Record<string, CodeLanguage> = {
	cjs: 'js',
	javascript: 'js',
	jsx: 'js',
	mjs: 'js',
	plaintext: 'text',
	sh: 'bash',
	shell: 'bash',
	tsx: 'ts',
	typescript: 'ts'
};

export function resolveCodeLanguage(language: string | null | undefined): CodeLanguage {
	const requested = (language ?? 'text').toLowerCase();
	const resolved = codeLanguageAliases[requested] ?? requested;
	return resolved in codeLanguageIcons ? (resolved as CodeLanguage) : 'text';
}
