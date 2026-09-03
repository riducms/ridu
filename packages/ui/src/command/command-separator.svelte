<script lang="ts">
	import { Command } from "bits-ui";

	import { cn } from "@ui/utils";

	let { ref = $bindable(null), class: className, ...restProps }: Command.SeparatorProps = $props();
</script>

<Command.Separator
	bind:ref
	data-slot="command-separator"
	class={cn("my-1.5 h-px bg-control-border", className)}
	{...restProps}
/>
