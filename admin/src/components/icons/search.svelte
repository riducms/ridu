<script lang="ts">
	import type { SVGAttributes } from "svelte/elements";
	let { ...props }: SVGAttributes<SVGSVGElement> = $props();
</script>

<svg width="20" height="20" viewBox="0 0 20 20" fill="none" aria-hidden="true" {...props}>
	<path
		d="M16 16L13.1333 13.1333M14.6667 9.33333C14.6667 12.2789 12.2789 14.6667 9.33333 14.6667C6.38781 14.6667 4 12.2789 4 9.33333C4 6.38781 6.38781 4 9.33333 4C12.2789 4 14.6667 6.38781 14.6667 9.33333Z"
		stroke="currentColor"
		stroke-linecap="square"
	/>
</svg>
