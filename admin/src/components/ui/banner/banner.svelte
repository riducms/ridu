<script lang="ts" module>
	import "@admin/components/ui/banner/banner.scss";
	import { cv, type VariantProps } from "@riducms/ui";

	const bannerVariants = cv({
		base: "ridu-banner",
		variants: {
			tone: {
				warning: "ridu-banner--warning",
				destructive: "ridu-banner--destructive",
				neutral: "ridu-banner--neutral",
			},
		},
		defaultVariants: { tone: "neutral" },
	});

	export type BannerTone = VariantProps<typeof bannerVariants>["tone"];
</script>

<script lang="ts">
	import type { HTMLAttributes } from "svelte/elements";

	import type { WithElementRef } from "@riducms/ui";

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
	class={[bannerVariants({ tone }), className]}
	role={tone === "destructive" ? "alert" : "status"}
	{...restProps}
>
	{@render children?.()}
</div>
