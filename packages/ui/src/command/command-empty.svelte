<script lang="ts">
	import { Command } from "bits-ui";

	import { cn } from "@ui/utils";

	let { ref = $bindable(null), class: className, ...restProps }: Command.EmptyProps = $props();
</script>

<Command.Empty
	bind:ref
	data-slot="command-empty"
	class={cn("px-6 py-10 text-center text-sm text-foreground-faint", className)}
	{...restProps}
/>
