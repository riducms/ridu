<script lang="ts">
	import { Select as SelectPrimitive } from "bits-ui";
	import ChevronUpIcon from "~icons/lucide/chevron-up";
	import { type WithoutChildrenOrChild } from "@riducms/ui";
	import "@admin/components/ui/select/select.scss";

	let {
		ref = $bindable(null),
		class: className,
		...restProps
	}: WithoutChildrenOrChild<SelectPrimitive.ScrollUpButtonProps> = $props();
</script>

<SelectPrimitive.ScrollUpButton
	bind:ref
	data-slot="select-scroll-up-button"
	class={["ridu-select__scroll", className]}
	{...restProps}
>
	<ChevronUpIcon />
</SelectPrimitive.ScrollUpButton>
