<script lang="ts">
	import { Select as SelectPrimitive } from "bits-ui";
	import ChevronDownIcon from "@admin/components/icons/chevron.svelte";
	import { type WithoutChild } from "@riducms/ui";

	import "@admin/components/ui/select/select.scss";

	let {
		ref = $bindable(null),
		class: className,
		children,
		size = "field",
		...restProps
	}: WithoutChild<SelectPrimitive.TriggerProps> & {
		size?: "field" | "compact" | "toolbar";
	} = $props();
</script>

<SelectPrimitive.Trigger
	bind:ref
	data-slot="select-trigger"
	data-size={size}
	class={["ridu-select__trigger", className]}
	{...restProps}
>
	{@render children?.()}
	<ChevronDownIcon class="ridu-select__chevron" />
</SelectPrimitive.Trigger>
