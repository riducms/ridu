/// <reference types="svelte" />
/// <reference types="vite/client" />

declare module "~icons/*" {
	import type { Component } from "svelte";
	import type { SvelteHTMLElements } from "svelte/elements";
	const icon: Component<SvelteHTMLElements["svg"]>;
	export default icon;
}
