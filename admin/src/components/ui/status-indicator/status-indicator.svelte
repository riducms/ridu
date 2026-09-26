<script lang="ts" module>
	import { cv, type VariantProps } from "@riducms/ui";

	export const statusIndicatorVariants = cv({
		base: "ridu-status-indicator",
		variants: {
			tone: {
				live: "ridu-status-indicator--live",
				warning: "ridu-status-indicator--warning",
				muted: "ridu-status-indicator--muted",
				destructive: "ridu-status-indicator--destructive",
				success: "ridu-status-indicator--success",
			},
		},
		defaultVariants: { tone: "muted" },
	});

	export type StatusTone = VariantProps<typeof statusIndicatorVariants>["tone"];
</script>

<script lang="ts">
	import type { HTMLAttributes } from "svelte/elements";
	import type { WithElementRef } from "@riducms/ui";
	import "@admin/components/ui/status-indicator/status-indicator.scss";

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
	class={[statusIndicatorVariants({ tone }), className]}
	{...restProps}
>
	<span
		class={[
			"ridu-status-indicator__dot",
			{ "ridu-status-indicator__dot--muted": tone === "muted" },
			{ "ridu-status-indicator__dot--pulse": pulse },
		]}
		aria-hidden="true"
	></span>
	{@render children?.()}
</span>
