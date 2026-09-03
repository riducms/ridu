<script lang="ts">
	import { Toolbar } from "bits-ui";

	import { cn } from "@ui/utils";

	let { ref = $bindable(null), class: className, ...restProps }: Toolbar.RootProps = $props();
</script>

<Toolbar.Root
	bind:ref
	data-slot="toolbar"
	class={cn("flex items-center", className)}
	{...restProps}
/>
