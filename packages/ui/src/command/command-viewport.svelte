<script lang="ts">
	import { Command } from "bits-ui";

	let { ref = $bindable(null), ...restProps }: Command.ViewportProps = $props();
</script>

<Command.Viewport bind:ref data-slot="command-viewport" {...restProps} />
