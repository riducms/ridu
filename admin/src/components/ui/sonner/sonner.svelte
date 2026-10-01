<script lang="ts">
	import "@admin/components/ui/sonner/sonner.scss";
	import LoaderCircleIcon from "~icons/lucide/loader-circle";
	import { Toaster as Sonner, type ToasterProps } from "svelte-sonner";
	import { getAdminI18n } from "@riducms/plugin";
	import { buttonVariants } from "@riducms/ui";

	const i18n = getAdminI18n();

	let {
		theme = "dark",
		position,
		closeButton = true,
		pauseWhenPageIsHidden = true,
		visibleToasts = 4,
		offset = 32,
		mobileOffset = 16,
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
			toast: "ridu-toast",
			content: "ridu-toast__content",
			title: "ridu-toast__title",
			description: "ridu-toast__description",
			icon: "ridu-toast__icon",
			closeButton: "ridu-toast__close",
			actionButton: `${buttonVariants({ variant: "outline", size: "xs" })} ridu-toast__action`,
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
		<LoaderCircleIcon class="ridu-toast__loader" aria-hidden="true" />
	{/snippet}
</Sonner>
