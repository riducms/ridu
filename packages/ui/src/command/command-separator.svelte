<script lang="ts">
	import { Command } from "bits-ui";

	let { ref = $bindable(null), class: className, ...restProps }: Command.SeparatorProps = $props();
</script>

<Command.Separator
	bind:ref
	data-slot="command-separator"
	class={["ridu-command__separator", className]}
	{...restProps}
/>
