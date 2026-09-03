<script lang="ts">
	import { Checkbox as CheckboxPrimitive } from "bits-ui";
	import CheckIcon from "~icons/lucide/check";
	import MinusIcon from "~icons/lucide/minus";

	import { cn, type WithoutChildrenOrChild } from "@riducms/ui";

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
	class={cn(
		"grid size-4 shrink-0 place-items-center rounded-[4px] border border-control-border bg-control text-primary-foreground transition-[background-color,border-color,opacity] duration-150 outline-none focus-visible:outline-2 focus-visible:outline-primary/60 focus-visible:outline-offset-2 data-[state=checked]:border-primary data-[state=checked]:bg-primary data-[state=indeterminate]:border-primary data-[state=indeterminate]:bg-primary data-[disabled]:cursor-not-allowed data-[disabled]:border-control-border-disabled data-[disabled]:bg-control-disabled data-[disabled]:opacity-50",
		className
	)}
	{...restProps}
>
	{#snippet children({ checked, indeterminate })}
		{#if indeterminate}
			<MinusIcon class="size-3 stroke-[2.5]" aria-hidden="true" />
		{:else if checked}
			<CheckIcon class="size-3 stroke-[2.5]" aria-hidden="true" />
		{/if}
	{/snippet}
</CheckboxPrimitive.Root>
