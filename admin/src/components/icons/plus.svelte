<script lang="ts">
	import type { SVGAttributes } from "svelte/elements";
	let { ...props }: SVGAttributes<SVGSVGElement> = $props();
</script>

<svg width="20" height="20" viewBox="0 0 20 20" fill="none" aria-hidden="true" {...props}>
	<path
		d="M5.33333 9.99998H14.6667M9.99999 5.33331V14.6667"
		stroke="currentColor"
		stroke-linecap="square"
	/>
</svg>
