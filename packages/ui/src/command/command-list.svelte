<script lang="ts">
	import { Command } from "bits-ui";

	import { cn } from "@ui/utils";

	let { ref = $bindable(null), class: className, ...restProps }: Command.ListProps = $props();
</script>

<Command.List
	bind:ref
	data-slot="command-list"
	class={cn("max-h-[330px] overflow-x-hidden overflow-y-auto p-1.5", className)}
	{...restProps}
/>
