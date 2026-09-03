<script lang="ts">
	import { Accordion as AccordionPrimitive } from "bits-ui";
	import ChevronDownIcon from '@lucide/svelte/icons/chevron-down';
	import ChevronUpIcon from '@lucide/svelte/icons/chevron-up';
	import { cn, type WithoutChild } from "@/utils.js";

	let {
		ref = $bindable(null),
		class: className,
		level = 3,
		children,
		...restProps
	}: WithoutChild<AccordionPrimitive.TriggerProps> & {
		level?: AccordionPrimitive.HeaderProps["level"];
	} = $props();
</script>

<AccordionPrimitive.Header {level} class="flex">
	<AccordionPrimitive.Trigger
		data-slot="accordion-trigger"
		bind:ref
		class={cn(
			"gap-6 p-4 text-left text-sm font-medium hover:underline **:data-[slot=accordion-trigger-icon]:ml-auto **:data-[slot=accordion-trigger-icon]:size-4 **:data-[slot=accordion-trigger-icon]:text-muted-foreground group/accordion-trigger relative flex flex-1 items-start justify-between border border-transparent transition-all outline-none disabled:pointer-events-none disabled:opacity-50",
			className
		)}
		{...restProps}
	>
		{@render children?.()}
		<ChevronDownIcon data-slot="accordion-trigger-icon" class="cn-accordion-trigger-icon pointer-events-none shrink-0 group-aria-expanded/accordion-trigger:hidden" />
		<ChevronUpIcon data-slot="accordion-trigger-icon" class="cn-accordion-trigger-icon pointer-events-none hidden shrink-0 group-aria-expanded/accordion-trigger:inline" />
	</AccordionPrimitive.Trigger>
</AccordionPrimitive.Header>
