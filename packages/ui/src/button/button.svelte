<script lang="ts" module>
	import "@ui/button/button.scss";
	import type { WithElementRef } from "@ui/utils";
	import { cv, type VariantProps } from "@ui/variants";
	import type { HTMLAnchorAttributes, HTMLButtonAttributes } from "svelte/elements";

	export const buttonVariants = cv({
		base: "ridu-button",
		variants: {
			variant: {
				default: "ridu-button--primary",
				outline: "ridu-button--outline",
				secondary: "ridu-button--secondary",
				ghost: "ridu-button--ghost",
				destructive: "ridu-button--destructive",
				link: "ridu-button--link",
			},
			size: {
				default: "ridu-button--medium",
				xs: "ridu-button--xs",
				sm: "ridu-button--small",
				lg: "ridu-button--large",
				icon: "ridu-button--icon",
				"icon-xs": "ridu-button--icon-xs",
				"icon-sm": "ridu-button--icon-small",
				"icon-lg": "ridu-button--icon-large",
			},
		},
		defaultVariants: { variant: "default", size: "default" },
	});

	export type ButtonVariant = VariantProps<typeof buttonVariants>["variant"];
	export type ButtonSize = VariantProps<typeof buttonVariants>["size"];

	export type ButtonProps = WithElementRef<HTMLButtonAttributes> &
		WithElementRef<HTMLAnchorAttributes> & {
			variant?: ButtonVariant;
			size?: ButtonSize;
			tooltip?: string;
			tooltipSide?: "top" | "right" | "bottom" | "left";
		};
</script>

<script lang="ts">
	import TooltipContent from "@ui/tooltip/tooltip-content.svelte";
	import TooltipRoot from "@ui/tooltip/tooltip-root.svelte";
	import TooltipTrigger from "@ui/tooltip/tooltip-trigger.svelte";

	function mergeTooltipTriggerProps<
		TTrigger extends Record<string, unknown>,
		TControl extends Record<string, unknown>,
	>(triggerProps: TTrigger, controlProps: TControl): TTrigger & TControl {
		const merged = { ...triggerProps, ...controlProps } as Record<string, unknown>;

		for (const key of Object.keys(triggerProps)) {
			const triggerHandler = triggerProps[key];
			const controlHandler = controlProps[key];
			if (
				!key.startsWith("on") ||
				typeof triggerHandler !== "function" ||
				typeof controlHandler !== "function"
			) {
				continue;
			}

			merged[key] = (...args: unknown[]) => {
				triggerHandler(...args);
				controlHandler(...args);
			};
		}

		return merged as TTrigger & TControl;
	}

	let {
		class: className,
		variant = "default",
		size = "default",
		ref = $bindable(null),
		href = undefined,
		type = "button",
		disabled,
		children,
		tooltip,
		tooltipSide = "top",
		...restProps
	}: ButtonProps = $props();
</script>

{#snippet control(triggerProps: Record<string, unknown> | undefined)}
	{#if href}
		<a
			bind:this={ref}
			data-slot="button"
			class={[buttonVariants({ variant, size }), className]}
			href={disabled ? undefined : href}
			aria-disabled={disabled}
			role={disabled ? "link" : undefined}
			tabindex={disabled ? -1 : undefined}
			{...triggerProps === undefined
				? restProps
				: mergeTooltipTriggerProps(triggerProps, restProps)}
		>
			{@render children?.()}
		</a>
	{:else}
		<button
			bind:this={ref}
			data-slot="button"
			class={[buttonVariants({ variant, size }), className]}
			{type}
			{disabled}
			{...triggerProps === undefined
				? restProps
				: mergeTooltipTriggerProps(triggerProps, restProps)}
		>
			{@render children?.()}
		</button>
	{/if}
{/snippet}

{#if tooltip !== undefined}
	<TooltipRoot>
		<TooltipTrigger>
			{#snippet child({ props })}
				{@render control(props)}
			{/snippet}
		</TooltipTrigger>
		<TooltipContent side={tooltipSide}>{tooltip}</TooltipContent>
	</TooltipRoot>
{:else}
	{@render control(undefined)}
{/if}
