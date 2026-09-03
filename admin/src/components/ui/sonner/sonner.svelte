<script lang="ts">
	import LoaderCircleIcon from "~icons/lucide/loader-circle";
	import { Toaster as Sonner, type ToasterProps } from "svelte-sonner";
	import { getAdminI18n } from "@riducms/plugin";

	const i18n = getAdminI18n();

	let {
		theme = "dark",
		position,
		closeButton = true,
		pauseWhenPageIsHidden = true,
		visibleToasts = 4,
		offset = 16,
		mobileOffset = 12,
		toastOptions,
		...restProps
	}: ToasterProps = $props();
	const resolvedPosition = $derived(
		position ?? (i18n.direction === "rtl" ? "bottom-left" : "bottom-right")
	);

	const options = $derived({
		unstyled: true,
		...toastOptions,
		classes: {
			toast:
				"ridu-toast flex w-[min(calc(100vw-2rem),390px)] items-start gap-2.5 rounded-[10px] py-3 pe-3.5 ps-5 shadow-[var(--shadow-popover)]",
			content: "min-w-0 flex-1",
			title: "text-[13.5px] leading-5 font-semibold",
			description: "mt-0.5 text-[12.5px] leading-5",
			icon: "mt-0.75 shrink-0",
			closeButton: "outline-none focus-visible:outline-2 focus-visible:outline-primary/60",
			...toastOptions?.classes,
		},
	});
</script>

<Sonner
	{theme}
	position={resolvedPosition}
	{closeButton}
	{pauseWhenPageIsHidden}
	{visibleToasts}
	{offset}
	{mobileOffset}
	toastOptions={options}
	containerAriaLabel={i18n.t("general:notifications")}
	closeButtonAriaLabel={i18n.t("general:dismissNotification")}
	{...restProps}
>
	{#snippet loadingIcon()}
		<LoaderCircleIcon class="size-3.5 animate-spin" aria-hidden="true" />
	{/snippet}
</Sonner>
