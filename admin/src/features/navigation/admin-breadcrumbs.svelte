<script lang="ts">
	import "@admin/features/navigation/admin-breadcrumbs.scss";
	import { Link, useLocation } from "@hvniel/svelte-router";

	import { getBreadcrumbs } from "@admin/features/navigation/breadcrumb-context.svelte";
	import RiduLogo from "@admin/components/brand/ridu-logo.svelte";
	import {
		adminPathResource,
		adminPathSegments,
		collectionPath,
		documentPath,
		documentVersionsPath,
		globalVersionsPath,
		globalPath,
		humanizeAdminPathSegment,
		parseAdminVersionRevision,
		withContentLocale,
	} from "@admin/core/routing/admin-paths";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";

	interface Breadcrumb {
		label: string;
		to?: string;
	}

	const page = getBreadcrumbs();
	const runtime = getAdminRuntime();
	const location = useLocation();

	const breadcrumbs = $derived.by<Breadcrumb[]>(() => {
		const resource = adminPathResource(location.current.pathname);
		if (resource !== undefined && runtime.isHiddenResource(resource.kind, resource.slug)) {
			return [{ label: runtime.i18n.t("errors:notFoundHeading") }];
		}

		const segments = adminPathSegments(location.current.pathname);
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
		const contentLocale = runtime.contentLocales.length === 0 ? undefined : runtime.contentLocale;

		if (segments[0] === "collections" && segments[1] !== undefined) {
			const collection = runtime.visibleCollections.find(
				(candidate) => candidate.slug === segments[1]
			);
			const label =
				collection === undefined
					? humanizeAdminPathSegment(segments[1], runtime.i18n.language)
					: runtime.i18n.text(collection.labels.plural, collection.labels.pluralTranslations);
			if (segments.length > 2 && segments[2] !== "trash" && segments[2] !== "upload") {
				const creating = segments[2] === "create";
				const versions = segments[3] === "versions";
				const versionRevision = versions ? parseAdminVersionRevision(segments[4]) : undefined;

				return [
					{ label, to: withContentLocale(collectionPath(segments[1]), contentLocale) },
					{
						label: creating
							? runtime.i18n.t("collections:createNewButton")
							: page?.document?.pathname === location.current.pathname
								? page.document.label
								: runtime.i18n.t("general:edit"),
						to: versions
							? withContentLocale(documentPath(segments[1], segments[2]!), contentLocale)
							: undefined,
					},
					...(versions
						? [
								{
									label: runtime.i18n.t("documents:versions"),
									to:
										versionRevision !== undefined
											? withContentLocale(
													documentVersionsPath(segments[1], segments[2]!),
													contentLocale
												)
											: undefined,
								},
								...(versionRevision !== undefined
									? [
											{
												label:
													page?.document?.pathname === location.current.pathname
														? (page.document.version ?? segments[4])
														: segments[4],
											},
										]
									: []),
							]
						: []),
					...(segments[3] === "api" ? [{ label: runtime.i18n.t("documents:api") }] : []),
				];
			}

			return [
				{
					label,
					to:
						segments.length > 2
							? withContentLocale(collectionPath(segments[1]), contentLocale)
							: undefined,
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
			const versionRevision =
				segments[2] === "versions" ? parseAdminVersionRevision(segments[3]) : undefined;

			return [
				{
					label:
						global === undefined
							? humanizeAdminPathSegment(segments[1], runtime.i18n.language)
							: runtime.i18n.text(global.labels.singular, global.labels.singularTranslations),
					to:
						segments.length > 2
							? withContentLocale(globalPath(segments[1]), contentLocale)
							: undefined,
				},
				...(segments[2] === "versions"
					? [
							{
								label: runtime.i18n.t("documents:versions"),
								to:
									versionRevision !== undefined
										? withContentLocale(globalVersionsPath(segments[1]), contentLocale)
										: undefined,
							},
							...(versionRevision !== undefined
								? [
										{
											label:
												page?.document?.pathname === location.current.pathname
													? (page.document.version ?? segments[3])
													: segments[3],
										},
									]
								: []),
						]
					: []),
				...(segments[2] === "api" ? [{ label: runtime.i18n.t("documents:api") }] : []),
			];
		}

		const pluginRoute = runtime.config.extensions.routes.find(
			(route) => route.path === segments.join("/")
		);

		return [
			{
				label:
					pluginRoute?.navigation?.labelKey !== undefined
						? runtime.i18n.t(pluginRoute.navigation.labelKey)
						: (pluginRoute?.navigation?.label ??
							humanizeAdminPathSegment(
								segments.at(-1) ?? runtime.i18n.t("navigation:page"),
								runtime.i18n.language
							)),
			},
		];
	});
</script>

<nav class="ridu-breadcrumbs" aria-label={runtime.i18n.t("navigation:breadcrumb")}>
	<Link class="ridu-breadcrumbs__home" to="/" aria-label={runtime.i18n.t("dashboard:heading")}>
		<RiduLogo variant="arch" class="ridu-breadcrumbs__logo" />
	</Link>
	{#each breadcrumbs as breadcrumb, index (`${breadcrumb.label}-${index}`)}
		<span class="ridu-breadcrumbs__separator" aria-hidden="true">/</span>
		{#if breadcrumb.to !== undefined}
			<Link class="ridu-breadcrumbs__link" to={breadcrumb.to}>
				{breadcrumb.label}
			</Link>
		{:else}
			<span class="ridu-breadcrumbs__current" aria-current="page">
				{breadcrumb.label}
			</span>
		{/if}
	{/each}
</nav>
