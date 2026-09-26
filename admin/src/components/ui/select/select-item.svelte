<script lang="ts">
	import { Select as SelectPrimitive } from "bits-ui";
	import CheckIcon from "~icons/lucide/check";
	import { type WithoutChild } from "@riducms/ui";

	import "@admin/components/ui/select/select.scss";

	let {
		ref = $bindable(null),
		class: className,
		value,
		label,
		children: childrenProp,
		...restProps
	}: WithoutChild<SelectPrimitive.ItemProps> = $props();
</script>

<SelectPrimitive.Item
	bind:ref
	{value}
	{label}
	data-slot="select-item"
	class={["ridu-select__item", className]}
	{...restProps}
>
	{#snippet children({ selected, highlighted })}
		<span class="ridu-select__indicator">
			{#if selected}
				<CheckIcon class="ridu-select__item-check" />
			{/if}
		</span>
		<span class="ridu-select__label">
			{#if childrenProp}
				{@render childrenProp({ selected, highlighted })}
			{:else}
				{label || value}
			{/if}
		</span>
	{/snippet}
</SelectPrimitive.Item>
