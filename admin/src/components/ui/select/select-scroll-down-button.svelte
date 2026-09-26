<script lang="ts">
	import { Select as SelectPrimitive } from "bits-ui";
	import ChevronDownIcon from "~icons/lucide/chevron-down";
	import { type WithoutChildrenOrChild } from "@riducms/ui";
	import "@admin/components/ui/select/select.scss";

	let {
		ref = $bindable(null),
		class: className,
		...restProps
	}: WithoutChildrenOrChild<SelectPrimitive.ScrollDownButtonProps> = $props();
</script>

<SelectPrimitive.ScrollDownButton
	bind:ref
	data-slot="select-scroll-down-button"
	class={["ridu-select__scroll", className]}
	{...restProps}
>
	<ChevronDownIcon />
</SelectPrimitive.ScrollDownButton>
