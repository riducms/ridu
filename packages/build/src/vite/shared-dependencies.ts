/**
 * Packages whose instances must be shared between the admin and statically imported plugins.
 * Svelte and Bits UI own component context; CodeMirror checks editor state and extensions by
 * identity, so a second copy breaks editors that the admin and a plugin both create.
 */
export const sharedDependencies = [
	"bits-ui",
	"svelte",
	"@codemirror/autocomplete",
	"@codemirror/language",
	"@codemirror/state",
	"@codemirror/view",
] as const;
