<script lang="ts" module>
	import { type ClassValue, type VariantProps, type WithElementRef, tv } from "@ui/utils";
	import type { HTMLAnchorAttributes, HTMLButtonAttributes } from "svelte/elements";

	export const buttonVariants = tv({
		base: "rounded-[3px] border border-transparent bg-clip-padding text-[13px] font-medium focus-visible:outline-2 focus-visible:outline-ring/70 focus-visible:outline-offset-2 active:not-aria-[haspopup]:translate-y-px aria-invalid:border-destructive aria-busy:cursor-wait aria-busy:opacity-85 [&_svg:not([class*='size-'])]:size-3.5 group/button inline-flex shrink-0 items-center justify-center whitespace-nowrap transition-[background-color,border-color,color,opacity,transform] duration-150 outline-none select-none disabled:pointer-events-none disabled:cursor-not-allowed [&_svg]:pointer-events-none [&_svg]:shrink-0",
		variants: {
			variant: {
				default:
					"bg-primary text-primary-foreground font-semibold hover:bg-primary-hover disabled:bg-primary/28 disabled:text-primary-foreground/70",
				outline:
					"border-control-border bg-transparent text-secondary-foreground hover:border-control-border-hover hover:bg-control hover:text-foreground aria-expanded:bg-control-hover aria-expanded:text-foreground disabled:border-control-border-disabled disabled:text-foreground-faint",
				secondary:
					"border-control-border bg-transparent text-secondary-foreground hover:bg-control hover:text-foreground aria-expanded:bg-control-hover aria-expanded:text-foreground disabled:border-control-border-disabled disabled:text-foreground-faint",
				ghost:
					"text-foreground-muted hover:bg-control-hover hover:text-foreground aria-expanded:bg-control-hover aria-expanded:text-foreground disabled:text-foreground-faint",
				destructive:
					"bg-destructive text-destructive-foreground font-semibold hover:bg-destructive-hover focus-visible:outline-destructive/60 disabled:bg-destructive/28 disabled:text-destructive-foreground/70",
				link: "text-primary underline-offset-4 hover:text-primary-hover hover:underline",
			},
			size: {
				default:
					"h-[31px] gap-2 px-3.5 has-data-[icon=inline-end]:pr-3 has-data-[icon=inline-start]:pl-3",
				xs: "h-6 gap-1 px-2.5 text-xs has-data-[icon=inline-end]:pr-2 has-data-[icon=inline-start]:pl-2 [&_svg:not([class*='size-'])]:size-3",
				sm: "h-7.5 gap-1.5 px-3 text-[13px] has-data-[icon=inline-end]:pr-2 has-data-[icon=inline-start]:pl-2",
				lg: "h-9 gap-2 px-5 text-[13.5px] has-data-[icon=inline-end]:pr-4 has-data-[icon=inline-start]:pl-4",
				icon: "size-[31px]",
				"icon-xs": "size-6 [&_svg:not([class*='size-'])]:size-3",
				"icon-sm": "size-7",
				"icon-lg": "size-9",
			},
		},
		defaultVariants: {
			variant: "default",
			size: "default",
		},
	});

	export type ButtonVariant = VariantProps<typeof buttonVariants>["variant"];
	export type ButtonSize = VariantProps<typeof buttonVariants>["size"];

	export type ButtonProps = WithElementRef<HTMLButtonAttributes> &
		WithElementRef<HTMLAnchorAttributes> & {
			variant?: ButtonVariant;
			size?: ButtonSize;
			class?: ClassValue;
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
	{const controlProps =
		triggerProps === undefined ? restProps : mergeTooltipTriggerProps(triggerProps, restProps)}
	{#if href}
		<a
			bind:this={ref}
			data-slot="button"
			class={buttonVariants({ variant, size, class: className })}
			href={disabled ? undefined : href}
			aria-disabled={disabled}
			role={disabled ? "link" : undefined}
			tabindex={disabled ? -1 : undefined}
			{...controlProps}
		>
			{@render children?.()}
		</a>
	{:else}
		<button
			bind:this={ref}
			data-slot="button"
			class={buttonVariants({ variant, size, class: className })}
			{type}
			{disabled}
			{...controlProps}
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
