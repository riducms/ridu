<script lang="ts">
	import { Command } from "bits-ui";

	let {
		ref = $bindable(null),
		value = $bindable(""),
		class: className,
		...restProps
	}: Command.InputProps = $props();
</script>

<Command.Input
	bind:ref
	bind:value
	data-slot="command-input"
	class={["ridu-command__input", className]}
	{...restProps}
/>
