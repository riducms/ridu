import { lexicalPreprocess } from "@hvniel/lexical-svelte/preprocess";
import { sveltekit } from "@sveltejs/kit/vite";
import { vitePreprocess } from "@sveltejs/vite-plugin-svelte";
import { defineConfig } from "vite";

export default defineConfig({
	plugins: [
		sveltekit({
			// svelte-package applies these when it packages the components.
			preprocess: [lexicalPreprocess(), vitePreprocess()],
			compilerOptions: { runes: true },
		}),
	],
});
