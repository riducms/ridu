<script lang="ts">
	import { Command } from "bits-ui";

	import { cn } from "@ui/utils";

	let { ref = $bindable(null), class: className, ...restProps }: Command.ItemProps = $props();
</script>

<Command.Item
	bind:ref
	data-slot="command-item"
	class={cn(
		"min-h-9 cursor-pointer gap-2.5 rounded-[3px] px-2.5 py-1.5 text-[12.5px] text-foreground-muted outline-none transition-[background-color,color] duration-100 flex items-center hover:bg-control-hover hover:text-foreground data-[selected]:bg-control-hover data-[selected]:text-foreground data-[disabled]:pointer-events-none data-[disabled]:opacity-45",
		className
	)}
	{...restProps}
/>
