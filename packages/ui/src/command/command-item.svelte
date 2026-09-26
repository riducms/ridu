<script lang="ts">
	import { Command } from "bits-ui";

	let { ref = $bindable(null), class: className, ...restProps }: Command.ItemProps = $props();
</script>

<Command.Item
	bind:ref
	data-slot="command-item"
	class={["ridu-command__item", className]}
	{...restProps}
/>
