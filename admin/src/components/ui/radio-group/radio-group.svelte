<script lang="ts">
	import { RadioGroup as RadioGroupPrimitive } from "bits-ui";
	import "@admin/components/ui/radio-group/radio.scss";

	let {
		ref = $bindable(null),
		value = $bindable(""),
		class: className,
		...restProps
	}: RadioGroupPrimitive.RootProps = $props();
</script>

<RadioGroupPrimitive.Root
	bind:ref
	bind:value
	data-slot="radio-group"
	class={className ?? "ridu-radio-group"}
	{...restProps}
/>
