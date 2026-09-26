<script lang="ts">
	import { Command } from "bits-ui";

	let { ref = $bindable(null), class: className, ...restProps }: Command.ListProps = $props();
</script>

<Command.List
	bind:ref
	data-slot="command-list"
	class={["ridu-command__list", className]}
	{...restProps}
/>
