<script lang="ts">
	import { Command } from "bits-ui";

	let { ref = $bindable(null), class: className, ...restProps }: Command.EmptyProps = $props();
</script>

<Command.Empty
	bind:ref
	data-slot="command-empty"
	class={["ridu-command__empty", className]}
	{...restProps}
/>
