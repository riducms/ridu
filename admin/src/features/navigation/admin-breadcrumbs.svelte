<script lang="ts">
	import { Link, useLocation } from "@hvniel/svelte-router";

	import RiduLogo from "@admin/components/brand/ridu-logo.svelte";
	import { collectionPath, globalPath } from "@admin/core/routing/admin-paths";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";

	interface Breadcrumb {
		label: string;
		to?: string;
	}

	const runtime = getAdminRuntime();
	const location = $derived(useLocation());
	const segments = $derived(
		location.pathname
			.split("/")
			.filter(Boolean)
			.map((segment) => decodeURIComponent(segment))
	);
	const breadcrumbs = $derived.by<Breadcrumb[]>(() => {
		if (segments.length === 0) return [{ label: runtime.i18n.t("dashboard:heading") }];

		if (segments[0] === "account") {
			return [
				{
					label: runtime.i18n.t("account:account"),
					to: segments.length > 1 ? "/account" : undefined,
				},
				...(segments[1] === "security" ? [{ label: runtime.i18n.t("account:security") }] : []),
			];
		}

		if (segments[0] === "collections" && segments[1] !== undefined) {
			const collection = runtime.visibleCollections.find(
				(candidate) => candidate.slug === segments[1]
			);
			const label =
				collection === undefined
					? humanize(segments[1])
					: runtime.i18n.text(collection.labels.plural, collection.labels.pluralTranslations);
			if (segments.length > 2 && segments[2] !== "trash" && segments[2] !== "upload") {
				const creating = segments[2] === "create";
				const versions = segments[3] === "versions";
				return [
					{ label, to: collectionPath(segments[1]) },
					{
						label: creating
							? runtime.i18n.t("general:create")
							: versions
								? runtime.i18n.t("documents:versions")
								: runtime.i18n.t("general:edit"),
					},
				];
			}
			return [
				{
					label,
					to: segments.length > 2 ? collectionPath(segments[1]) : undefined,
				},
				...(segments[2] === "trash"
					? [{ label: runtime.i18n.t("collections:trash") }]
					: segments[2] === "upload"
						? [{ label: runtime.i18n.t("collections:bulkUpload") }]
						: []),
			];
		}

		if (segments[0] === "globals" && segments[1] !== undefined) {
			const global = runtime.visibleGlobals.find((candidate) => candidate.slug === segments[1]);
			return [
				{
					label:
						global === undefined
							? humanize(segments[1])
							: runtime.i18n.text(global.labels.singular, global.labels.singularTranslations),
					to: segments.length > 2 ? globalPath(segments[1]) : undefined,
				},
				...(segments[2] === "versions" ? [{ label: runtime.i18n.t("documents:versions") }] : []),
			];
		}

		const pluginRoute = runtime.pluginRoutes.find((route) => route.path === segments.join("/"));
		return [
			{
				label:
					pluginRoute?.navigation?.labelKey !== undefined
						? runtime.i18n.t(pluginRoute.navigation.labelKey)
						: (pluginRoute?.navigation?.label ??
							humanize(segments.at(-1) ?? runtime.i18n.t("navigation:page"))),
			},
		];
	});

	function humanize(value: string): string {
		const words = value.replaceAll(/[-_]+/g, " ");
		return words.charAt(0).toLocaleUpperCase(runtime.i18n.language) + words.slice(1);
	}
</script>

<nav
	class="flex min-w-0 items-center gap-3 text-[13px]"
	aria-label={runtime.i18n.t("navigation:breadcrumb")}
>
	<Link
		class="grid size-7 shrink-0 place-items-center"
		to="/"
		aria-label={runtime.i18n.t("dashboard:heading")}
	>
		<RiduLogo variant="arch" class="h-4" />
	</Link>
	{#each breadcrumbs as breadcrumb, index (`${breadcrumb.label}-${index}`)}
		<span class="shrink-0" aria-hidden="true">/</span>
		{#if breadcrumb.to !== undefined}
			<Link class="min-w-0 truncate" to={breadcrumb.to}>
				{breadcrumb.label}
			</Link>
		{:else}
			<span class="min-w-0 truncate font-medium text-foreground-strong" aria-current="page">
				{breadcrumb.label}
			</span>
		{/if}
	{/each}
</nav>
