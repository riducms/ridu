<script lang="ts" module>
	import { tv, type VariantProps } from "@riducms/ui";

	export const bannerVariants = tv({
		base: "flex min-w-0 items-start gap-3 rounded-[3px] border px-3.5 py-3 text-[13.5px] leading-5",
		variants: {
			tone: {
				warning: "border-warning/30 bg-warning/8 text-warning",
				destructive: "border-destructive/30 bg-destructive/8 text-destructive",
				neutral: "border-border bg-muted/35 text-foreground-muted",
			},
		},
		defaultVariants: { tone: "neutral" },
	});

	export type BannerTone = VariantProps<typeof bannerVariants>["tone"];
</script>

<script lang="ts">
	import type { HTMLAttributes } from "svelte/elements";

	import { cn, type WithElementRef } from "@riducms/ui";

	let {
		ref = $bindable(null),
		tone = "neutral",
		class: className,
		children,
		...restProps
	}: WithElementRef<HTMLAttributes<HTMLDivElement>> & { tone?: BannerTone } = $props();
</script>

<div
	bind:this={ref}
	data-slot="banner"
	class={cn(bannerVariants({ tone }), className)}
	role={tone === "destructive" ? "alert" : "status"}
	{...restProps}
>
	{@render children?.()}
</div>
