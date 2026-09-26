<script lang="ts">
	import { Command } from "bits-ui";

	let { ref = $bindable(null), class: className, ...restProps }: Command.ViewportProps = $props();
</script>

<Command.Viewport
	bind:ref
	data-slot="command-viewport"
	class={["ridu-command__viewport", className]}
	{...restProps}
/>
