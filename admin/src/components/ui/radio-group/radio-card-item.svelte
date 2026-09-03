<script lang="ts">
	import { RadioGroup as RadioGroupPrimitive } from "bits-ui";

	import { cn } from "@riducms/ui";

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
	class={cn(
		"group/radio inline-flex min-h-6 cursor-pointer items-center gap-2 text-left text-[13px] text-foreground-muted outline-none transition-colors hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/70 focus-visible:ring-offset-2 focus-visible:ring-offset-background data-[state=checked]:text-foreground data-[disabled]:cursor-not-allowed data-[disabled]:text-foreground-faint",
		className
	)}
>
	{#snippet children({ checked })}
		<span
			class="grid size-5 shrink-0 place-items-center rounded-full border border-control-border bg-control transition-[border-color,background-color] group-hover/radio:border-control-border-hover data-[checked=true]:border-foreground-muted"
			data-checked={checked}
			aria-hidden="true"
		>
			{#if checked}<span class="size-2 rounded-full bg-foreground"></span>{/if}
		</span>
		<span class="min-w-0">{label}</span>
	{/snippet}
</RadioGroupPrimitive.Item>
