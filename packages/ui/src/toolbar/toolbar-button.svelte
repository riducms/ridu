<script lang="ts">
	import { Toolbar } from "bits-ui";

	import { cn } from "@ui/utils";

	let {
		ref = $bindable(null),
		class: className,
		active = false,
		...restProps
	}: Toolbar.ButtonProps & { active?: boolean } = $props();
</script>

<Toolbar.Button
	bind:ref
	data-slot="toolbar-button"
	data-active={active || undefined}
	class={cn(
		"inline-grid cursor-pointer place-items-center border-0 bg-transparent text-inherit font-inherit focus-visible:outline-2 focus-visible:outline-ring/60 focus-visible:outline-offset-1 disabled:pointer-events-none disabled:opacity-45",
		className
	)}
	{...restProps}
/>
