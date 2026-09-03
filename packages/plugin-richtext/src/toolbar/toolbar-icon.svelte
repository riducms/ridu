<script lang="ts">
	type IconName =
		"bold" | "code" | "italic" | "link" | "strike" | "subscript" | "superscript" | "underline";

	let { name, size = 15 }: { name: IconName; size?: number } = $props();
</script>

<svg
	width={size}
	height={size}
	viewBox="0 0 24 24"
	fill="none"
	stroke="currentColor"
	stroke-width="1.8"
	stroke-linecap="round"
	stroke-linejoin="round"
	aria-hidden="true"
>
	{#if name === "bold"}
		<path d="M7 5h6a4 4 0 0 1 0 8H7z" />
		<path d="M7 13h7a3.5 3.5 0 0 1 0 7H7z" />
	{:else if name === "italic"}
		<path d="M10 5h7" />
		<path d="M7 19h7" />
		<path d="m14 5-4 14" />
	{:else if name === "link"}
		<path d="M10 13a5 5 0 0 0 7.1.1l2-2a5 5 0 0 0-7.1-7.1l-1.1 1.1" />
		<path d="M14 11a5 5 0 0 0-7.1-.1l-2 2A5 5 0 0 0 12 20l1.1-1.1" />
	{:else if name === "strike"}
		<path d="M16 5.5A5.5 5.5 0 0 0 12 4c-2.2 0-4 1.2-4 3s1.6 2.5 4 3" />
		<path d="M8 18.5a6 6 0 0 0 4 1.5c2.2 0 4-1.2 4-3 0-1.4-1-2.2-2.6-2.6" />
		<path d="M4 12h16" />
	{:else if name === "underline"}
		<path d="M7 4v6a5 5 0 0 0 10 0V4" />
		<path d="M5 21h14" />
	{:else if name === "subscript"}
		<path d="m5 6 8 10M13 6 5 16" />
		<path d="M16 17c.5-1 3-1 3 .5 0 1-3 2-3 3h3" />
	{:else if name === "superscript"}
		<path d="m5 8 8 10M13 8 5 18" />
		<path d="M16 4c.5-1 3-1 3 .5 0 1-3 2-3 3h3" />
	{:else if name === "code"}
		<path d="m8 9-3 3 3 3" />
		<path d="m16 9 3 3-3 3" />
		<path d="m14 5-4 14" />
	{/if}
</svg>
