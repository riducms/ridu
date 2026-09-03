<script lang="ts">
	import { Command } from "bits-ui";

	import { cn } from "@ui/utils";

	let {
		ref = $bindable(null),
		value = $bindable(""),
		class: className,
		...restProps
	}: Command.InputProps = $props();
</script>

<Command.Input
	bind:ref
	bind:value
	data-slot="command-input"
	class={cn(
		"h-12 w-full border-0 bg-transparent text-[15.5px] text-foreground caret-primary outline-none placeholder:text-foreground-faint",
		className
	)}
	{...restProps}
/>
