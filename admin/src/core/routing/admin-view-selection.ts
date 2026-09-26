import type { AdminCoreView, AdminCoreViewSurface } from "@riducms/plugin";
import type { ResolvedAdminConfig } from "@riducms/plugin/admin";
import type { SchemaManifest } from "@riducms/protocol";
import { matchRoutes } from "@hvniel/svelte-router";
import { adminRoutePatterns } from "@admin/core/routing/admin-paths";

/** Select the registration first: an un-loaded exact replacement suppresses a wildcard loader. */
export function resolveCoreView(
	coreViews: readonly AdminCoreView[],
	surface: AdminCoreViewSurface,
	resource?: string
) {
	const candidates = coreViews.filter((view) => view.surface === surface);
	const target = (view: AdminCoreView) =>
		view.surface === "global"
			? view.global
			: view.surface === "notFound"
				? undefined
				: view.collection;
	return (
		candidates.find((view) => target(view) === resource) ??
		candidates.find((view) => target(view) === undefined)
	);
}

const replacementSurfaces: Partial<Record<keyof typeof adminRoutePatterns, AdminCoreViewSurface>> =
	{
		collection: "collectionList",
		createDocument: "collectionCreate",
		createDocumentAPI: "collectionCreate",
		document: "collectionEdit",
		documentAPI: "collectionEdit",
		global: "global",
		globalAPI: "global",
	};

/** Use the router's ranking/decoding, including more-specific document tabs and reserved paths. */
export function preparedCustomView(
	config: ResolvedAdminConfig,
	pathname: string,
	manifest?: SchemaManifest
) {
	const routes = [
		...Object.entries(adminRoutePatterns).map(([key, path]) => ({
			path,
			surface: replacementSurfaces[key as keyof typeof adminRoutePatterns],
			view: undefined,
		})),
		...config.extensions.routes.map((view) => ({ path: view.path, surface: undefined, view })),
		...config.extensions.documentViews.flatMap((view) => [
			{
				path: `collections/${view.collection ?? ":collection"}/:document/${view.key}`,
				surface: undefined,
				view: undefined,
			},
			{
				path: `globals/${view.collection ?? ":global"}/${view.key}`,
				surface: undefined,
				view: undefined,
			},
		]),
		{ path: "*", surface: "notFound" as const, view: undefined },
	];
	const match = matchRoutes(routes, pathname)?.[0];
	if (match?.route.view !== undefined) return match.route.view;
	const surface = match?.route.surface;
	if (surface === undefined) return undefined;
	const resource = surface === "global" ? match?.params.global : match?.params.collection;
	if (
		manifest !== undefined &&
		surface !== "notFound" &&
		!(surface === "global" ? manifest.globals : manifest.collections)?.some(
			(item) => item.slug === resource
		)
	)
		return undefined;
	return resolveCoreView(config.extensions.coreViews, surface, resource);
}
