<script lang="ts">
	import { Slider as SliderPrimitive } from "bits-ui";

	import { cn } from "@riducms/ui";

	let {
		id,
		value,
		min = 0,
		max = 100,
		step = 1,
		disabled = false,
		label,
		describedBy,
		class: className,
		onValueChange,
	}: {
		id?: string;
		value: number;
		min?: number;
		max?: number;
		step?: number;
		disabled?: boolean;
		label: string;
		describedBy?: string;
		class?: string;
		onValueChange: (value: number) => void;
	} = $props();
</script>

<SliderPrimitive.Root
	{id}
	type="single"
	{value}
	{min}
	{max}
	{step}
	{disabled}
	{onValueChange}
	data-slot="slider"
	class={cn(
		"relative flex h-5 w-full touch-none items-center select-none data-[disabled]:cursor-not-allowed data-[disabled]:opacity-45",
		className
	)}
>
	{#snippet children({ thumbItems })}
		<span class="relative h-1.5 w-full grow overflow-hidden rounded-full bg-control-disabled">
			<SliderPrimitive.Range class="absolute h-full rounded-full bg-primary" />
		</span>
		{#each thumbItems as thumb (thumb.index)}
			<SliderPrimitive.Thumb
				index={thumb.index}
				aria-label={label}
				aria-describedby={describedBy}
				class="block size-4 rounded-full border-2 border-primary-foreground bg-primary shadow-sm outline-none transition-[box-shadow,transform] hover:scale-110 focus-visible:scale-110 focus-visible:ring-2 focus-visible:ring-primary/45 focus-visible:ring-offset-2 focus-visible:ring-offset-background disabled:pointer-events-none"
			/>
		{/each}
	{/snippet}
</SliderPrimitive.Root>
