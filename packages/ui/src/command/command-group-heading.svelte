<script lang="ts">
	import { Command } from "bits-ui";

	let {
		ref = $bindable(null),
		class: className,
		...restProps
	}: Command.GroupHeadingProps = $props();
</script>

<Command.GroupHeading
	bind:ref
	data-slot="command-group-heading"
	class={["ridu-command__group-heading", className]}
	{...restProps}
/>
