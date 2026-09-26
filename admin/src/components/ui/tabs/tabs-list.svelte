<script lang="ts" module>
	import { cv, type VariantProps } from "@riducms/ui";

	export const tabsListVariants = cv({
		base: "ridu-tabs__list",
		variants: {
			variant: {
				default: "ridu-tabs__list--default",
				line: "ridu-tabs__list--line",
			},
		},
		defaultVariants: {
			variant: "default",
		},
	});

	export type TabsListVariant = VariantProps<typeof tabsListVariants>["variant"];
</script>

<script lang="ts">
	import { Tabs as TabsPrimitive } from "bits-ui";
	import "@admin/components/ui/tabs/tabs.scss";

	let {
		ref = $bindable(null),
		variant = "default",
		class: className,
		...restProps
	}: TabsPrimitive.ListProps & {
		variant?: TabsListVariant;
	} = $props();
</script>

<TabsPrimitive.List
	bind:ref
	data-slot="tabs-list"
	data-variant={variant}
	class={[tabsListVariants({ variant }), className]}
	{...restProps}
/>
