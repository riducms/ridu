<script lang="ts">
	import { Command } from "bits-ui";

	import { cn } from "@ui/utils";

	let {
		ref = $bindable(null),
		value = $bindable(""),
		class: className,
		...restProps
	}: Command.RootProps = $props();
</script>

<Command.Root
	bind:ref
	bind:value
	data-slot="command"
	class={cn("min-h-0 min-w-0 flex flex-col overflow-hidden", className)}
	{...restProps}
/>
