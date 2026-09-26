<script lang="ts">
	import { Command } from "bits-ui";

	let { ref = $bindable(null), class: className, ...restProps }: Command.GroupProps = $props();
</script>

<Command.Group
	bind:ref
	data-slot="command-group"
	class={["ridu-command__group", className]}
	{...restProps}
/>
