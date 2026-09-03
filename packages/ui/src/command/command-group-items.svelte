<script lang="ts">
	import { Command } from "bits-ui";

	import { cn } from "@ui/utils";

	let { ref = $bindable(null), class: className, ...restProps }: Command.GroupItemsProps = $props();
</script>

<Command.GroupItems
	bind:ref
	data-slot="command-group-items"
	class={cn("grid gap-0.5", className)}
	{...restProps}
/>
