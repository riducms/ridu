<script lang="ts">
	import type { SVGAttributes } from "svelte/elements";
	let { ...props }: SVGAttributes<SVGSVGElement> = $props();
</script>

<svg width="20" height="20" viewBox="0 0 20 20" fill="none" aria-hidden="true" {...props}>
	<path d="M14 6L6 14M6 6L14 14" stroke="currentColor" stroke-linecap="square" />
</svg>
