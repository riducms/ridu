<script lang="ts">
	import { Link, NavLink, Outlet, useLocation } from "@hvniel/svelte-router";
	import type { AdminNavigationComponent } from "@riducms/plugin";
	import { tick } from "svelte";
	import { MediaQuery } from "svelte/reactivity";
	import ChevronDownIcon from "~icons/lucide/chevron-down";
	import ChevronLeftIcon from "~icons/lucide/chevron-left";
	import MenuIcon from "~icons/lucide/menu";
	import SearchIcon from "~icons/lucide/search";

	import { Button, buttonVariants } from "@admin/components/ui/button";
	import {
		Sheet,
		SheetClose,
		SheetContent,
		SheetDescription,
		SheetTitle,
		SheetTrigger,
	} from "@admin/components/ui/sheet";
	import {
		getPreferenceWriteQueue,
		preferenceOwnerID,
	} from "@admin/core/preferences/preference-write-queue";
	import { collectionPath, globalPath } from "@admin/core/routing/admin-paths";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import AccountMenu from "@admin/features/auth/account-menu.svelte";
	import AdminBreadcrumbs from "@admin/features/navigation/admin-breadcrumbs.svelte";
	import { groupAdminResources } from "@admin/features/navigation/admin-resource-groups";
	import AdminCommandMenu from "@admin/features/navigation/admin-command-menu.svelte";
	import ContentLocaleSwitcher from "@admin/features/localization/content-locale-switcher.svelte";
	import { cn } from "@riducms/ui";

	const navigationPreferenceKey = "navigation";
	const runtime = getAdminRuntime();
	const location = $derived(useLocation());
	const routeSegments = $derived(location.pathname.split("/").filter(Boolean));
	const routeOwnsViewport = $derived(
		(routeSegments[0] === "collections" &&
			routeSegments.length > 2 &&
			routeSegments[2] !== "trash" &&
			routeSegments[2] !== "upload" &&
			routeSegments[3] !== "versions") ||
			(routeSegments[0] === "globals" &&
				routeSegments.length > 1 &&
				routeSegments[2] !== "versions")
	);
	const narrowNavigation = new MediaQuery("(max-width: 1023px)");
	const applicationName = $derived(
		runtime.manifest === undefined
			? runtime.i18n.t("general:riduApplication")
			: runtime.i18n.text(
					runtime.manifest.application.name,
					runtime.manifest.application.nameTranslations
				)
	);
	const resourceGroups = $derived(
		groupAdminResources(runtime.visibleCollections, runtime.visibleGlobals, {
			collections: runtime.i18n.t("navigation:collections"),
			globals: runtime.i18n.t("navigation:globals"),
		})
	);
	const navigationPluginRoutes = $derived(
		runtime.pluginRoutes.filter((route) => route.navigation !== undefined)
	);
	let commandOpen = $state(false);
	let desktopNavigationOpen = $state(true);
	let mobileNavigationOpen = $state(false);
	let desktopToggle = $state<HTMLButtonElement | null>(null);
	let preferenceGeneration = 0;
	const navigationReplacement = $derived(
		runtime.navigationComponents.find((component) => component.position === "replace")
	);
	const navigationBefore = $derived(
		runtime.navigationComponents.filter((component) => component.position === "before")
	);
	const navigationBeforeLinks = $derived(
		runtime.navigationComponents.filter((component) => component.position === "beforeLinks")
	);
	const navigationAfterLinks = $derived(
		runtime.navigationComponents.filter((component) => component.position === "afterLinks")
	);
	const navigationAfter = $derived(
		runtime.navigationComponents.filter((component) => component.position === "after")
	);
	const navigationLogo = $derived(
		runtime.brandComponents.find((component) => component.surface === "navigationLogo")
	);
	const shellHeaders = $derived(
		runtime.shellComponents.filter((component) => component.position === "header")
	);
	const shellActions = $derived(
		runtime.shellComponents.filter((component) => component.position === "actions")
	);
	const hasShellHeader = $derived(runtime.manifest !== undefined && shellHeaders.length > 0);

	$effect(() => {
		const owner = preferenceOwnerID(runtime.session);
		if (owner === "") return;
		const generation = ++preferenceGeneration;
		const request = new AbortController();
		void runtime.client
			.preference<unknown>(navigationPreferenceKey, { signal: request.signal })
			.then((preference) => {
				if (
					request.signal.aborted ||
					generation !== preferenceGeneration ||
					typeof preference !== "object" ||
					preference === null ||
					typeof (preference as { open?: unknown }).open !== "boolean"
				) {
					return;
				}
				desktopNavigationOpen = (preference as { open: boolean }).open;
			})
			.catch(() => undefined);
		return () => request.abort();
	});

	async function toggleDesktopNavigation() {
		preferenceGeneration += 1;
		const next = !desktopNavigationOpen;
		desktopNavigationOpen = next;
		const owner = preferenceOwnerID(runtime.session);
		if (owner !== "") {
			void getPreferenceWriteQueue()
				.enqueue(
					navigationPreferenceKey,
					owner,
					() => preferenceOwnerID(runtime.session) === owner,
					() => runtime.client.setPreference(navigationPreferenceKey, { open: next })
				)
				.catch(() => undefined);
		}
		await tick();
		desktopToggle?.focus();
	}

	function closeMobileNavigation() {
		if (narrowNavigation.current) mobileNavigationOpen = false;
	}

	async function openCommandMenu() {
		if (narrowNavigation.current) {
			mobileNavigationOpen = false;
			await tick();
		}
		commandOpen = true;
	}
</script>

{#snippet navigationContributions(components: readonly AdminNavigationComponent[])}
	{#if runtime.manifest !== undefined}
		{#each components as component (component.key)}
			<component.component
				manifest={runtime.manifest}
				user={runtime.session?.user}
				defaultView={defaultNavigation}
				i18n={runtime.i18n}
			/>
		{/each}
	{/if}
{/snippet}

{#snippet defaultNavigation()}
	{@render navigationContributions(navigationBefore)}

	<nav
		aria-label={runtime.i18n.t("navigation:adminNavigation")}
		class="grid flex-1 content-start overflow-y-auto px-5 pb-5"
	>
		{@render navigationContributions(navigationBeforeLinks)}

		{#each resourceGroups as group (group.key)}
			<details open class="group/resource mb-3">
				<summary
					class="flex cursor-pointer list-none items-center justify-between gap-3 py-1 text-[12.5px] text-foreground-faint transition-colors hover:text-foreground-strong [&::-webkit-details-marker]:hidden"
				>
					<span>{runtime.i18n.text(group.label, group.translations)}</span>
					<ChevronDownIcon
						class="size-2.5 rotate-[-90deg] text-foreground-soft transition-transform group-open/resource:rotate-0"
						aria-hidden="true"
					/>
				</summary>
				<div class="grid">
					{#each group.collections as collection (collection.id)}
						<NavLink
							to={collectionPath(collection.slug)}
							class={({ isActive, isPending }) =>
								cn(
									"relative flex min-h-7 items-center text-[13px] transition-colors after:absolute after:top-1 after:bottom-1 after:-start-5 after:w-0.5",
									isActive
										? "font-medium text-foreground-strong after:bg-foreground-strong"
										: "text-sidebar-foreground hover:text-foreground",
									isPending && "opacity-60"
								)}
							onclick={closeMobileNavigation}
						>
							<span class="truncate">
								{runtime.i18n.text(collection.labels.plural, collection.labels.pluralTranslations)}
							</span>
						</NavLink>
					{/each}
					{#each group.globals as global (global.id)}
						<NavLink
							to={globalPath(global.slug)}
							class={({ isActive, isPending }) =>
								cn(
									"relative flex min-h-7 items-center text-[13px] transition-colors after:absolute after:top-1 after:bottom-1 after:-start-5 after:w-0.5",
									isActive
										? "font-medium text-foreground-strong after:bg-foreground-strong"
										: "text-sidebar-foreground hover:text-foreground",
									isPending && "opacity-60"
								)}
							onclick={closeMobileNavigation}
						>
							<span class="truncate">
								{runtime.i18n.text(global.labels.singular, global.labels.singularTranslations)}
							</span>
						</NavLink>
					{/each}
				</div>
			</details>
		{/each}

		{#if navigationPluginRoutes.length > 0}
			<details open class="group/resource mb-3">
				<summary
					class="flex cursor-pointer list-none items-center justify-between gap-3 py-1 text-[12.5px] text-foreground-faint transition-colors hover:text-foreground-strong [&::-webkit-details-marker]:hidden"
				>
					<span>{runtime.i18n.t("navigation:plugins")}</span>
					<ChevronDownIcon
						class="size-2.5 rotate-[-90deg] text-foreground-soft transition-transform group-open/resource:rotate-0"
						aria-hidden="true"
					/>
				</summary>
				<div class="grid">
					{#each navigationPluginRoutes as pluginRoute (pluginRoute.path)}
						<NavLink
							to={`/${pluginRoute.path}`}
							class={({ isActive }) =>
								cn(
									"relative flex min-h-7 items-center text-[13px] transition-colors after:absolute after:top-1 after:bottom-1 after:-start-5 after:w-0.5",
									isActive
										? "font-medium text-foreground-strong after:bg-foreground-strong"
										: "text-sidebar-foreground hover:text-foreground"
								)}
							onclick={closeMobileNavigation}
						>
							{pluginRoute.navigation?.labelKey === undefined
								? pluginRoute.navigation?.label
								: runtime.i18n.t(pluginRoute.navigation.labelKey)}
						</NavLink>
					{/each}
				</div>
			</details>
		{/if}

		{@render navigationContributions(navigationAfterLinks)}
	</nav>

	{@render navigationContributions(navigationAfter)}

	{#if runtime.session !== undefined}
		<div class="mt-auto px-5 py-4">
			<AccountMenu compact />
		</div>
	{/if}
{/snippet}

{#snippet navigationSurface(mobile = false)}
	<div class={["flex h-14 shrink-0 items-center gap-2 pe-4", mobile ? "ps-4" : "ps-16"]}>
		{#if mobile}
			<SheetClose
				class={buttonVariants({ variant: "outline", size: "icon-sm" })}
				aria-label={runtime.i18n.t("navigation:closeMenu")}
			>
				<ChevronLeftIcon class="size-4 rtl:rotate-180" aria-hidden="true" />
			</SheetClose>
		{/if}

		<Link
			class="ms-1 min-w-0 flex-1 truncate text-[13px] font-medium text-foreground"
			to="/"
			title={applicationName}
			aria-label={applicationName}
			onclick={closeMobileNavigation}
		>
			{#if navigationLogo !== undefined && runtime.manifest !== undefined}
				<navigationLogo.component
					manifest={runtime.manifest}
					user={runtime.session?.user}
					surface="navigationLogo"
					i18n={runtime.i18n}
				/>
			{/if}
		</Link>

		<Button
			variant="ghost"
			size="icon-sm"
			onclick={openCommandMenu}
			aria-label={runtime.i18n.t("navigation:searchAndNavigate")}
			tooltip={runtime.i18n.t("navigation:searchAndNavigateShortcut")}
		>
			<SearchIcon class="size-3.5" aria-hidden="true" />
		</Button>
	</div>

	{#if runtime.manifest !== undefined && navigationReplacement !== undefined}
		<navigationReplacement.component
			manifest={runtime.manifest}
			user={runtime.session?.user}
			defaultView={defaultNavigation}
			i18n={runtime.i18n}
		/>
	{:else}
		{@render defaultNavigation()}
	{/if}
{/snippet}

{#snippet adminHeaderActions()}
	{#if runtime.manifest !== undefined}
		{#each shellActions as component (component.key)}
			<component.component
				manifest={runtime.manifest}
				user={runtime.session?.user}
				position="actions"
				i18n={runtime.i18n}
			/>
		{/each}
	{/if}
	<ContentLocaleSwitcher />
	{#if runtime.session !== undefined}
		<div class="lg:hidden">
			<AccountMenu compact />
		</div>
	{/if}
{/snippet}

{#if hasShellHeader && runtime.manifest !== undefined}
	<header
		class="flex min-h-5 items-center gap-3 bg-background-surface px-5 text-[12px] sm:px-8 lg:px-15"
		aria-label={runtime.i18n.t("navigation:pluginShellHeader")}
	>
		{#each shellHeaders as component (component.key)}
			<component.component
				manifest={runtime.manifest}
				user={runtime.session?.user}
				position="header"
				i18n={runtime.i18n}
			/>
		{/each}
	</header>
{/if}

<div
	class={[
		"transition-[grid-template-columns] duration-300 ease-out motion-reduce:transition-none md:grid md:overflow-hidden",
		hasShellHeader
			? "min-h-[calc(100vh-1.25rem)] md:h-[calc(100vh-1.25rem)]"
			: "min-h-screen md:h-screen",
		desktopNavigationOpen ? "lg:grid-cols-[275px_minmax(0,1fr)]" : "lg:grid-cols-[0_minmax(0,1fr)]",
	]}
>
	{#if narrowNavigation.current}
		<Sheet bind:open={mobileNavigationOpen}>
			<SheetTrigger
				class={buttonVariants({
					variant: "outline",
					size: "icon-sm",
					class: hasShellHeader ? "fixed top-7 start-2 z-30" : "fixed top-2.5 start-2 z-30",
				})}
				aria-label={runtime.i18n.t("navigation:openMenu")}
				title={runtime.i18n.t("navigation:openMenu")}
			>
				<MenuIcon class="size-4" aria-hidden="true" />
			</SheetTrigger>
			<SheetContent
				side={runtime.i18n.direction === "rtl" ? "right" : "left"}
				showCloseButton={false}
				class="w-[min(275px,calc(100vw-40px))] border-sidebar-border bg-sidebar p-0 text-sidebar-foreground sm:max-w-none"
			>
				<SheetTitle class="sr-only">
					{runtime.i18n.t("navigation:adminNavigation")}
				</SheetTitle>
				<SheetDescription class="sr-only">
					{runtime.i18n.t("navigation:adminNavigationDescription")}
				</SheetDescription>
				<aside class="flex h-full min-h-0 flex-col">
					{@render navigationSurface(true)}
				</aside>
			</SheetContent>
		</Sheet>
	{:else}
		<div class="relative h-full min-h-0 bg-sidebar text-sidebar-foreground">
			<aside
				id="ridu-admin-navigation"
				inert={!desktopNavigationOpen}
				aria-hidden={!desktopNavigationOpen}
				class={[
					"absolute inset-y-0 start-0 flex w-[275px] min-h-0 flex-col border-e border-sidebar-border bg-sidebar transition-[transform,opacity] duration-300 ease-out motion-reduce:transition-none",
					desktopNavigationOpen
						? "translate-x-0 opacity-100"
						: runtime.i18n.direction === "rtl"
							? "pointer-events-none translate-x-full opacity-0"
							: "pointer-events-none -translate-x-full opacity-0",
				]}
			>
				{@render navigationSurface()}
			</aside>
		</div>

		<Button
			bind:ref={desktopToggle}
			variant="outline"
			size="icon-sm"
			class={cn(
				"fixed z-40",
				hasShellHeader ? "top-[34px]" : "top-3.5",
				desktopNavigationOpen ? "start-4" : "start-2"
			)}
			onclick={toggleDesktopNavigation}
			aria-label={runtime.i18n.t(
				desktopNavigationOpen ? "navigation:closeMenu" : "navigation:openMenu"
			)}
			aria-controls="ridu-admin-navigation"
			aria-expanded={desktopNavigationOpen}
			title={runtime.i18n.t(desktopNavigationOpen ? "navigation:closeMenu" : "navigation:openMenu")}
		>
			{#if desktopNavigationOpen}
				<ChevronLeftIcon class="size-4 rtl:rotate-180" aria-hidden="true" />
			{:else}
				<MenuIcon class="size-4" aria-hidden="true" />
			{/if}
		</Button>
	{/if}

	<main
		class={[
			"relative min-w-0 bg-background max-lg:ps-11 md:flex md:h-full md:flex-col",
			routeOwnsViewport ? "md:overflow-hidden" : "md:overflow-y-auto",
		]}
	>
		<header
			class="flex h-14 shrink-0 items-center justify-between gap-4 bg-background px-5 sm:px-8 lg:px-15"
			aria-label={runtime.i18n.t("navigation:adminHeader")}
		>
			<AdminBreadcrumbs />
			<div class="flex shrink-0 items-center gap-2">
				{@render adminHeaderActions()}
			</div>
		</header>
		<Outlet />
	</main>
</div>

<AdminCommandMenu bind:open={commandOpen} collections={runtime.visibleCollections} />
