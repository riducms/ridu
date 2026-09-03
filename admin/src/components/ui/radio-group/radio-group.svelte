<script lang="ts">
	import { RadioGroup as RadioGroupPrimitive } from "bits-ui";
	import { cn } from "@riducms/ui";

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
	class={cn("grid gap-2", className)}
	{...restProps}
/>
