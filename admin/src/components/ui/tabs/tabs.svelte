<script lang="ts">
	import { Tabs as TabsPrimitive } from "bits-ui";
	import "@admin/components/ui/tabs/tabs.scss";

	let {
		ref = $bindable(null),
		value = $bindable(""),
		class: className,
		...restProps
	}: TabsPrimitive.RootProps = $props();
</script>

<TabsPrimitive.Root
	bind:ref
	bind:value
	data-slot="tabs"
	class={["ridu-tabs", className]}
	{...restProps}
/>
