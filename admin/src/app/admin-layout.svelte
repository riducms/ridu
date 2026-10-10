<script lang="ts">
	import { Outlet, useHref, useLocation, useNavigation } from "@hvniel/svelte-router";
	import { MediaQuery } from "svelte/reactivity";
	import ChevronLeftIcon from "~icons/lucide/chevron-left";
	import MenuIcon from "~icons/lucide/menu";
	import SearchIcon from "~icons/lucide/search";

	import NavigationDrawer from "@admin/components/ui/navigation-drawer/navigation-drawer.svelte";
	import "@admin/app/admin-layout.scss";
	import AdminCoreViewRoute from "@admin/app/admin-core-view-route.svelte";
	import NotFoundRoute from "@admin/app/not-found-route.svelte";
	import { adminLayoutRouteBehavior, adminPathResource } from "@admin/core/routing/admin-paths";
	import { useAdminScrollRestoration } from "@admin/core/routing/admin-scroll.svelte";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import AccountMenu from "@admin/features/account/account-menu.svelte";
	import AdminNavigation from "@admin/features/navigation/admin-navigation.svelte";
	import { createNavigationPreference } from "@admin/features/navigation/navigation-preference.svelte";
	import { provideBreadcrumbs } from "@admin/features/navigation/breadcrumb-context.svelte";
	import AdminBreadcrumbs from "@admin/features/navigation/admin-breadcrumbs.svelte";
	import AdminCommandMenu from "@admin/features/navigation/admin-command-menu.svelte";
	import ContentLocaleSwitcher from "@admin/features/localization/content-locale-switcher.svelte";

	provideBreadcrumbs();
	const runtime = getAdminRuntime();
	const extensions = runtime.config.extensions;
	const location = useLocation();
	const navigation = useNavigation();
	const currentPath = useHref(() => location.current.pathname);

	const routePending = $derived(
		navigation.current.state !== "idle" || runtime.contentLocaleTransitioning
	);
	const routeChanging = $derived.by(() => {
		if (runtime.contentLocaleTransitioning) return true;

		const destination = navigation.current.location;
		if (destination === undefined) return false;

		// Query controls stay usable within the same page and locale. The feature owns
		// disabling stale results; leaving the page still disables its whole surface.
		return (
			destination.pathname !== currentPath.current ||
			new URLSearchParams(destination.search).get("locale") !==
				new URLSearchParams(location.current.search).get("locale")
		);
	});

	let viewport = $state<HTMLElement | null>(null);
	const containedViewport = new MediaQuery("(min-width: 769px)");
	// A hidden collection or global has no pages of its own in the admin.
	const hiddenResource = $derived.by(() => {
		const resource = adminPathResource(location.current.pathname);
		return resource !== undefined && runtime.isHiddenResource(resource.kind, resource.slug);
	});
	const routeBehavior = $derived(
		hiddenResource
			? { ownsViewport: false, waitsForPage: false }
			: adminLayoutRouteBehavior(location.current.pathname)
	);
	useAdminScrollRestoration(
		() => (containedViewport.current ? viewport : window),
		() => routeBehavior.waitsForPage
	);

	const narrowNavigation = new MediaQuery("(max-width: 768px)");
	let commandOpen = $state(false);

	const navigationPreference = createNavigationPreference(runtime);

	let mobileNavigationRoute = $state<string>();
	const mobileNavigationOpen = $derived(
		narrowNavigation.current && mobileNavigationRoute === location.current.key
	);

	$effect(() => {
		if (!narrowNavigation.current) mobileNavigationRoute = undefined;
	});

	const shellHeaders = extensions.shellSlots.filter((component) => component.position === "header");
	const shellActions = extensions.shellSlots.filter(
		(component) => component.position === "actions"
	);
	const hasShellHeader = $derived(runtime.manifest !== undefined && shellHeaders.length > 0);

	function closeMobileNavigation() {
		if (narrowNavigation.current) mobileNavigationRoute = undefined;
	}

	function openCommandMenu() {
		closeMobileNavigation();
		commandOpen = true;
	}
</script>

<div class={["ridu-shell", { "ridu-shell--nav-open": navigationPreference.open }]}>
	{#if hasShellHeader && runtime.manifest !== undefined}
		<header
			class="ridu-shell__extension-header"
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

	<div class="ridu-shell__body">
		{#if !narrowNavigation.current}
			<div class="ridu-shell__sidebar">
				<aside
					id="ridu-admin-navigation"
					inert={!navigationPreference.open}
					aria-hidden={!navigationPreference.open}
					class="ridu-nav"
				>
					<AdminNavigation onNavigate={closeMobileNavigation} />
				</aside>
			</div>

			<div class="ridu-shell__toggle">
				<button
					type="button"
					class="ridu-nav-toggle"
					onclick={navigationPreference.toggle}
					aria-label={runtime.i18n.t(
						navigationPreference.open ? "navigation:closeMenu" : "navigation:openMenu"
					)}
					aria-controls="ridu-admin-navigation"
					aria-expanded={navigationPreference.open}
				>
					{#if navigationPreference.open}
						<ChevronLeftIcon class="ridu-nav-toggle__collapse" aria-hidden="true" />
					{:else}
						<MenuIcon aria-hidden="true" />
					{/if}
				</button>
			</div>
		{/if}

		<main
			bind:this={viewport}
			inert={routeChanging}
			aria-busy={routePending}
			class={["ridu-shell__main", { "ridu-shell__main--contained": routeBehavior.ownsViewport }]}
		>
			<header class="ridu-shell__header" aria-label={runtime.i18n.t("navigation:adminHeader")}>
				{#if narrowNavigation.current}
					<NavigationDrawer
						bind:open={
							() => mobileNavigationOpen,
							(open) => (mobileNavigationRoute = open ? location.current.key : undefined)
						}
					>
						<AdminNavigation onNavigate={closeMobileNavigation} />
					</NavigationDrawer>
				{/if}

				<AdminBreadcrumbs />

				<div class="ridu-shell__actions">
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
					<button
						type="button"
						class="ridu-shell__search"
						onclick={openCommandMenu}
						aria-label={runtime.i18n.t("navigation:searchAndNavigate")}
						title={runtime.i18n.t("navigation:searchAndNavigateShortcut")}
					>
						<SearchIcon aria-hidden="true" />
					</button>
					<ContentLocaleSwitcher />
					{#if runtime.session !== undefined}
						<AccountMenu />
					{/if}
				</div>
			</header>

			{#if hiddenResource}
				<AdminCoreViewRoute surface="notFound" DefaultView={NotFoundRoute} />
			{:else}
				<Outlet />
			{/if}
		</main>
	</div>
</div>

<AdminCommandMenu bind:open={commandOpen} collections={runtime.navigableCollections} />
