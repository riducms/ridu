<script lang="ts">
	import { Checkbox as CheckboxPrimitive } from "bits-ui";
	import CheckIcon from "~icons/lucide/check";
	import MinusIcon from "~icons/lucide/minus";

	import { type WithoutChildrenOrChild } from "@ui/utils";

	import "@ui/checkbox/checkbox.scss";

	let {
		ref = $bindable(null),
		checked = $bindable(false),
		indeterminate = $bindable(false),
		class: className,
		...restProps
	}: WithoutChildrenOrChild<CheckboxPrimitive.RootProps> = $props();
</script>

<CheckboxPrimitive.Root
	bind:ref
	bind:checked
	bind:indeterminate
	data-slot="checkbox"
	class={["ridu-checkbox", className]}
	{...restProps}
>
	{#snippet children({ checked, indeterminate })}
		{#if indeterminate}
			<MinusIcon class="ridu-checkbox__mark" aria-hidden="true" />
		{:else if checked}
			<CheckIcon class="ridu-checkbox__mark" aria-hidden="true" />
		{/if}
	{/snippet}
</CheckboxPrimitive.Root>
