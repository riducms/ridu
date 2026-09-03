<script lang="ts">
	import { useLocation, useNavigate } from "@hvniel/svelte-router";
	import CheckIcon from "~icons/lucide/check";
	import ChevronDownIcon from "~icons/lucide/chevron-down";

	import { buttonVariants } from "@admin/components/ui/button";
	import {
		DropdownMenu,
		DropdownMenuContent,
		DropdownMenuItem,
		DropdownMenuTrigger,
	} from "@admin/components/ui/dropdown-menu";
	import { getNotificationCenter } from "@admin/core/notifications/notification-center.svelte";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";

	const runtime = getAdminRuntime();
	const notifications = getNotificationCenter();
	const navigate = useNavigate();
	const location = $derived(useLocation());
	const localization = $derived(runtime.manifest?.application.localization);
	const locale = $derived(
		localization?.locales.find((candidate) => candidate.code === runtime.contentLocale)
	);
	const disabled = $derived(runtime.contentLocaleSwitchBlocked);

	$effect(() => {
		const requested = new URLSearchParams(location.search).get("locale");
		if (
			requested !== null &&
			localization?.locales.some((candidate) => candidate.code === requested) === true &&
			requested !== runtime.contentLocale
		) {
			if (!runtime.adoptContentLocale(requested)) updateRouteLocale(runtime.contentLocale);
		}
	});

	async function selectLocale(nextLocale: string) {
		if (disabled || nextLocale === runtime.contentLocale) return;
		const previousLocale = runtime.contentLocale;
		updateRouteLocale(nextLocale);
		try {
			await runtime.setContentLocale(nextLocale);
		} catch (cause) {
			updateRouteLocale(runtime.contentLocale ?? previousLocale);
			notifications.error({
				title: runtime.i18n.t("errors:save"),
				message: cause instanceof Error ? cause.message : undefined,
			});
		}
	}

	function updateRouteLocale(nextLocale: string | undefined) {
		const search = new URLSearchParams(location.search);
		if (nextLocale === undefined) search.delete("locale");
		else search.set("locale", nextLocale);
		navigate(
			{
				pathname: location.pathname,
				search: search.size === 0 ? "" : `?${search}`,
				hash: location.hash,
			},
			{ replace: true }
		);
	}
</script>

{#if localization !== undefined && localization.locales.length > 1 && locale !== undefined}
	<DropdownMenu>
		<DropdownMenuTrigger
			class={buttonVariants({ variant: "ghost", size: "sm", class: "gap-1.5 px-2.5" })}
			{disabled}
			aria-label={runtime.i18n.t("documents:contentLocale")}
			title={disabled ? runtime.i18n.t("documents:saveBeforeLocaleSwitch") : undefined}
		>
			<span class="hidden text-foreground-faint sm:inline">
				{runtime.i18n.t("documents:locale")}:
			</span>
			<span>{locale.label}</span>
			<ChevronDownIcon class="size-3 text-foreground-faint" aria-hidden="true" />
		</DropdownMenuTrigger>
		<DropdownMenuContent class="min-w-48">
			{#each localization.locales as candidate (candidate.code)}
				<DropdownMenuItem onSelect={() => selectLocale(candidate.code)}>
					<span class="min-w-0 flex-1 truncate">{candidate.label}</span>
					<span class="text-foreground-faint">({candidate.code})</span>
					{#if candidate.code === locale.code}
						<CheckIcon class="size-3.5 text-foreground" aria-hidden="true" />
					{/if}
				</DropdownMenuItem>
			{/each}
		</DropdownMenuContent>
	</DropdownMenu>
{/if}
