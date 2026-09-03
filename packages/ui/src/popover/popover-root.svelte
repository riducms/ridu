<script lang="ts">
	import { Popover } from "bits-ui";

	let { open = $bindable(false), ...restProps }: Popover.RootProps = $props();
</script>

<Popover.Root bind:open {...restProps} />
