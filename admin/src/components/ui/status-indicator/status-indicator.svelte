<script lang="ts" module>
	import { tv, type VariantProps } from "@riducms/ui";

	export const statusIndicatorVariants = tv({
		base: "inline-flex items-center gap-1.75 text-[12.5px] text-foreground-muted",
		variants: {
			tone: {
				live: "text-primary",
				warning: "text-warning",
				muted: "text-foreground-muted",
				destructive: "text-destructive",
				success: "text-success",
			},
		},
		defaultVariants: { tone: "muted" },
	});

	export type StatusTone = VariantProps<typeof statusIndicatorVariants>["tone"];
</script>

<script lang="ts">
	import type { HTMLAttributes } from "svelte/elements";

	import { cn, type WithElementRef } from "@riducms/ui";

	let {
		ref = $bindable(null),
		tone = "muted",
		pulse = false,
		class: className,
		children,
		...restProps
	}: WithElementRef<HTMLAttributes<HTMLSpanElement>> & {
		tone?: StatusTone;
		pulse?: boolean;
	} = $props();
</script>

<span
	bind:this={ref}
	data-slot="status-indicator"
	class={cn(statusIndicatorVariants({ tone }), className)}
	{...restProps}
>
	<span
		class={[
			"size-1.75 shrink-0 rounded-full",
			tone === "muted" ? "border border-current bg-transparent" : "bg-current",
			pulse && "shadow-[0_0_9px_currentColor]",
		]}
		aria-hidden="true"
	></span>
	{@render children?.()}
</span>
