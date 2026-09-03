<script lang="ts">
	import { Calendar as CalendarPrimitive } from "bits-ui";
	import ChevronRightIcon from "~icons/lucide/chevron-right";

	import { buttonVariants, type ButtonVariant } from "@admin/components/ui/button";
	import { cn } from "@riducms/ui";

	let {
		ref = $bindable(null),
		class: className,
		children,
		variant = "ghost",
		...restProps
	}: CalendarPrimitive.NextButtonProps & { variant?: ButtonVariant } = $props();
</script>

{#snippet fallback()}
	<ChevronRightIcon class="size-3.5" aria-hidden="true" />
{/snippet}

<CalendarPrimitive.NextButton
	bind:ref
	class={cn(
		buttonVariants({ variant, size: "icon-sm" }),
		"size-(--calendar-cell-size) bg-transparent p-0 disabled:opacity-35 rtl:rotate-180",
		className
	)}
	children={children ?? fallback}
	{...restProps}
/>
