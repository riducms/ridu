/// <reference types="vite/client" />

declare module "~icons/*" {
	import type { Component } from "svelte";
	const icon: Component<Record<string, unknown>>;
	export default icon;
}
