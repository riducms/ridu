<script lang="ts">
	import { Tabs as TabsPrimitive } from "bits-ui";
	import "@admin/components/ui/tabs/tabs.scss";

	let {
		ref = $bindable(null),
		class: className,
		...restProps
	}: TabsPrimitive.ContentProps = $props();
</script>

<TabsPrimitive.Content
	bind:ref
	data-slot="tabs-content"
	class={["ridu-tabs__content", className]}
	{...restProps}
/>
