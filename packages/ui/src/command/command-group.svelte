<script lang="ts">
	import { Command } from "bits-ui";

	let { ref = $bindable(null), ...restProps }: Command.GroupProps = $props();
</script>

<Command.Group bind:ref data-slot="command-group" {...restProps} />
