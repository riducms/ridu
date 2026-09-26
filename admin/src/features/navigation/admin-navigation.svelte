<script lang="ts">
	import "@admin/features/navigation/admin-navigation.scss";
	import { Link, NavLink } from "@hvniel/svelte-router";
	import type { AdminNavigationComponent } from "@riducms/plugin";
	import ChevronDownIcon from "~icons/lucide/chevron-down";
	import { adminApplicationName } from "@admin/app-meta";
	import { collectionPath, globalPath, withContentLocale } from "@admin/core/routing/admin-paths";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import { groupAdminResources } from "@admin/features/navigation/admin-resource-groups";
	import LogoutButton from "@admin/features/auth/logout-button.svelte";

	let { onNavigate }: { onNavigate: () => void } = $props();

	const runtime = getAdminRuntime();
	const extensions = runtime.config.extensions;

	const contentLocale = $derived(
		runtime.contentLocales.length === 0 ? undefined : runtime.contentLocale
	);
	const applicationName = $derived(
		adminApplicationName(runtime.manifest, runtime.i18n, runtime.i18n.t("general:riduApplication"))
	);
	const resourceGroups = $derived(
		groupAdminResources(runtime.visibleCollections, runtime.visibleGlobals, {
			collections: runtime.i18n.t("navigation:collections"),
			globals: runtime.i18n.t("navigation:globals"),
		})
	);

	const navigationPluginRoutes = extensions.routes.flatMap((route) =>
		route.navigation === undefined ? [] : [{ path: route.path, navigation: route.navigation }]
	);
	const navigationReplacement = extensions.navigation.find(
		(component) => component.position === "replace"
	);
	const navigationBefore = extensions.navigation.filter(
		(component) => component.position === "before"
	);
	const navigationBeforeLinks = extensions.navigation.filter(
		(component) => component.position === "beforeLinks"
	);
	const navigationAfterLinks = extensions.navigation.filter(
		(component) => component.position === "afterLinks"
	);
	const navigationAfter = extensions.navigation.filter(
		(component) => component.position === "after"
	);

	const navigationLogo = extensions.branding.find(
		(component) => component.surface === "navigationLogo"
	);

	function pluginRouteLabel(route: (typeof navigationPluginRoutes)[number]) {
		return route.navigation.labelKey === undefined
			? route.navigation.label
			: runtime.i18n.t(route.navigation.labelKey);
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

{#snippet navigationLink(to: string, label: string)}
	<NavLink
		{to}
		class={({ isActive, isPending }) =>
			[
				"ridu-nav__link",
				isActive && "ridu-nav__link--active",
				isPending && "ridu-nav__link--pending",
			]
				.filter(Boolean)
				.join(" ")}
		onclick={onNavigate}
	>
		<span>{label}</span>
	</NavLink>
{/snippet}

{#snippet defaultNavigation()}
	{@render navigationContributions(navigationBefore)}

	<nav aria-label={runtime.i18n.t("navigation:adminNavigation")} class="ridu-nav__scroll">
		{@render navigationContributions(navigationBeforeLinks)}

		{#each resourceGroups as group (group.key)}
			<details open class="ridu-nav__group">
				<summary class="ridu-nav__group-toggle">
					<span>{runtime.i18n.text(group.label, group.translations)}</span>
					<ChevronDownIcon class="ridu-nav__chevron" aria-hidden="true" />
				</summary>
				<div class="ridu-nav__links">
					{#each group.collections as collection (collection.id)}
						{@render navigationLink(
							withContentLocale(collectionPath(collection.slug), contentLocale),
							runtime.i18n.text(collection.labels.plural, collection.labels.pluralTranslations)
						)}
					{/each}

					{#each group.globals as global (global.id)}
						{@render navigationLink(
							withContentLocale(globalPath(global.slug), contentLocale),
							runtime.i18n.text(global.labels.singular, global.labels.singularTranslations)
						)}
					{/each}
				</div>
			</details>
		{/each}

		{#if navigationPluginRoutes.length > 0}
			<details open class="ridu-nav__group">
				<summary class="ridu-nav__group-toggle">
					<span>{runtime.i18n.t("navigation:plugins")}</span>
					<ChevronDownIcon class="ridu-nav__chevron" aria-hidden="true" />
				</summary>
				<div class="ridu-nav__links">
					{#each navigationPluginRoutes as pluginRoute (pluginRoute.path)}
						{@render navigationLink(`/${pluginRoute.path}`, pluginRouteLabel(pluginRoute))}
					{/each}
				</div>
			</details>
		{/if}

		{@render navigationContributions(navigationAfterLinks)}
		{#if runtime.session !== undefined}
			<div class="ridu-nav__controls"><LogoutButton /></div>
		{/if}
	</nav>

	{@render navigationContributions(navigationAfter)}
{/snippet}

{#if navigationLogo !== undefined && runtime.manifest !== undefined}
	<Link
		class="ridu-nav__brand"
		to="/"
		title={applicationName}
		aria-label={applicationName}
		onclick={onNavigate}
	>
		<navigationLogo.component
			manifest={runtime.manifest}
			user={runtime.session?.user}
			surface="navigationLogo"
			i18n={runtime.i18n}
		/>
	</Link>
{/if}

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
