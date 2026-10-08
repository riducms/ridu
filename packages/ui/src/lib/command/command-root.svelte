<script lang="ts">
	import { Command } from "bits-ui";

	import "#lib/command/command.scss";

	let {
		ref = $bindable(null),
		value = $bindable(""),
		class: className,
		...restProps
	}: Command.RootProps = $props();
</script>

<Command.Root
	bind:ref
	bind:value
	data-slot="command"
	class={["ridu-command", className]}
	{...restProps}
/>
