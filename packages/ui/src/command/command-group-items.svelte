<script lang="ts">
	import { Command } from "bits-ui";

	let { ref = $bindable(null), class: className, ...restProps }: Command.GroupItemsProps = $props();
</script>

<Command.GroupItems
	bind:ref
	data-slot="command-group-items"
	class={["ridu-command__group-items", className]}
	{...restProps}
/>
