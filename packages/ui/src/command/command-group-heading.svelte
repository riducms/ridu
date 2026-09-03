<script lang="ts">
	import { Command } from "bits-ui";

	import { cn } from "@ui/utils";

	let {
		ref = $bindable(null),
		class: className,
		...restProps
	}: Command.GroupHeadingProps = $props();
</script>

<Command.GroupHeading
	bind:ref
	data-slot="command-group-heading"
	class={cn(
		"px-2.5 pt-3 pb-1.5 font-mono text-[9.5px] tracking-[0.14em] text-foreground-faint uppercase",
		className
	)}
	{...restProps}
/>
