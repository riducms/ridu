<script lang="ts">
	import { RadioGroup as RadioGroupPrimitive } from "bits-ui";

	import "@admin/components/ui/radio-group/radio.scss";

	let {
		value,
		label,
		disabled = false,
		class: className,
	}: {
		value: string;
		label: string;
		disabled?: boolean;
		class?: string;
	} = $props();
</script>

<RadioGroupPrimitive.Item
	{value}
	{disabled}
	data-slot="radio-card-item"
	class={["ridu-radio-option", className]}
>
	{#snippet children({ checked })}
		<span class="ridu-radio-option__control" data-checked={checked} aria-hidden="true">
			{#if checked}
				<span class="ridu-radio-option__dot"></span>
			{/if}
		</span>
		<span>{label}</span>
	{/snippet}
</RadioGroupPrimitive.Item>
