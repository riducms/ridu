<script lang="ts">
	import { Tabs as TabsPrimitive } from "bits-ui";
	import "@admin/components/ui/tabs/tabs.scss";

	let {
		ref = $bindable(null),
		class: className,
		...restProps
	}: TabsPrimitive.TriggerProps = $props();
</script>

<TabsPrimitive.Trigger
	bind:ref
	data-slot="tabs-trigger"
	class={["ridu-tabs__trigger", className]}
	{...restProps}
/>
