<script lang="ts">
	import { Link, useLocation, useNavigate, useParams } from "@hvniel/svelte-router";
	import { untrack } from "svelte";
	import { Button } from "@riducms/ui";
	import { Banner } from "@admin/components/ui/banner";
	import { Skeleton } from "@admin/components/ui/skeleton";
	import { getNotificationCenter } from "@admin/core/notifications/notification-center.svelte";
	import {
		documentIDFromAdminPath,
		documentPath,
		documentAPIPath,
		documentVersionPath,
		documentVersionsPath,
		globalPath,
		globalAPIPath,
		globalVersionPath,
		globalVersionsPath,
		parseAdminVersionRevision,
		withContentLocale,
	} from "@admin/core/routing/admin-paths";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import { getAdminBootstrapCoordinator } from "@admin/core/bootstrap/admin-bootstrap";
	import { getBreadcrumbs } from "@admin/features/navigation/breadcrumb-context.svelte";
	import { registerAdminScrollPage } from "@admin/core/routing/admin-scroll.svelte";
	import { documentLabel } from "@admin/features/documents/document-title";
	import { VersionHistoryController } from "@admin/features/versions/version-history-controller.svelte";
	import { versionDate } from "@admin/features/versions/version-history";
	import VersionTable from "@admin/features/versions/version-table.svelte";
	import VersionComparison from "@admin/features/versions/version-comparison.svelte";
	import "@admin/features/documents/document.scss";
	import "@admin/features/versions/version-history.scss";

	const runtime = getAdminRuntime();
	const i18n = runtime.i18n;
	const bootstrap = getAdminBootstrapCoordinator();
	const breadcrumbs = getBreadcrumbs();
	const controller = new VersionHistoryController(runtime, getNotificationCenter());
	registerAdminScrollPage({ ready: () => !controller.loading });
	const navigate = useNavigate();
	const location = useLocation();
	const params = useParams<"collection" | "document" | "global" | "revision">();
	const globalResource = $derived(params.current.global !== undefined);
	const slug = $derived(params.current.global ?? params.current.collection ?? "");
	let retainedID = $state(documentIDFromAdminPath(location.current.pathname) ?? "");

	$effect.pre(() => {
		const decoded = documentIDFromAdminPath(location.current.pathname);
		// Keep the outgoing owner while its parent match is clearing.
		if (decoded !== undefined) retainedID = decoded;
	});

	const documentID = $derived(globalResource ? slug : retainedID);
	const search = $derived(new URLSearchParams(location.current.search));
	const locale = $derived(runtime.resolveContentLocale(search.get("locale")));
	const requestedRevision = $derived(parseAdminVersionRevision(params.current.revision));
	const collection = $derived(
		(globalResource ? runtime.manifest?.globals : runtime.manifest?.collections)?.find(
			(item) => item.slug === slug
		)
	);
	const owner = $derived(
		JSON.stringify([globalResource, slug, documentID, requestedRevision, locale])
	);
	const historyPath = $derived(
		withContentLocale(
			globalResource ? globalVersionsPath(slug) : documentVersionsPath(slug, documentID),
			locale
		)
	);
	const editPath = $derived(
		withContentLocale(globalResource ? globalPath(slug) : documentPath(slug, documentID), locale)
	);
	const apiPath = $derived(
		withContentLocale(
			globalResource ? globalAPIPath(slug) : documentAPIPath(slug, documentID),
			locale
		)
	);
	const heading = $derived(
		controller.document
			? documentLabel(collection, controller.document)
			: collection
				? i18n.text(collection.labels.singular, collection.labels.singularTranslations)
				: slug
	);

	$effect.pre(() => {
		const target = {
			slug,
			id: documentID,
			global: globalResource,
			revision: requestedRevision,
			locale,
		};
		const revision = runtime.manifestRevision;
		if (!revision || !slug || !documentID) return;
		// Query-only changes alter presentation, not the owner of loaded history or pending restores.
		untrack(() => {
			const prepared = bootstrap.routeData(location.current.pathname, location.current.search);
			controller.sync(
				target,
				revision,
				prepared?.kind === (globalResource ? "global-versions" : "collection-versions")
					? prepared.versions
					: undefined
			);
		});
	});

	$effect(() => {
		const unregister = runtime.registerContentLocaleBlocker(() => controller.restoring);
		return () => {
			unregister();
			controller.dispose();
		};
	});

	$effect(() => {
		if (!breadcrumbs) return;
		const entry = {
			pathname: location.current.pathname,
			label: heading,
			version: controller.selectedVersion
				? versionDate(controller.selectedVersion.CreatedAt, i18n)
				: undefined,
		};
		breadcrumbs.document = entry;
		return () => {
			if (breadcrumbs.document === entry) breadcrumbs.document = undefined;
		};
	});

	function updateQuery(changes: Record<string, string>) {
		const next = new URLSearchParams(location.current.search);
		for (const [key, value] of Object.entries(changes)) next.set(key, value);
		navigate(`${location.current.pathname}?${next}`, { preventScrollReset: true });
	}

	function versionPath(version: { Revision: number }) {
		return withContentLocale(
			globalResource
				? globalVersionPath(slug, version.Revision)
				: documentVersionPath(slug, documentID, version.Revision),
			locale
		);
	}
</script>

<section class="ridu-versions" aria-busy={controller.loading}>
	<header class="ridu-document-header ridu-versions__header">
		<div class="ridu-document-heading">
			<h1 class="ridu-document-title">{heading}</h1>
			<nav class="ridu-document-tabs" aria-label={i18n.t("documents:documentViews")}>
				<Link to={editPath} class="ridu-document-tab">{i18n.t("documents:edit")}</Link>
				<Link
					to={historyPath}
					class="ridu-document-tab ridu-document-tab--active"
					aria-current="page"
				>
					{i18n.t("documents:versions")}
					{#if !controller.loading && !controller.error}
						<span class="ridu-versions__count">
							{i18n.formatNumber(controller.versions.length)}
						</span>
					{/if}
				</Link>
				<Link to={apiPath} class="ridu-document-tab">{i18n.t("documents:api")}</Link>
			</nav>
		</div>
	</header>

	{#if controller.error}
		<div class="ridu-versions__message">
			<Banner tone="destructive">{controller.error}</Banner>
			{#if controller.canRetry}
				<Button variant="outline" onclick={controller.retry}>
					{i18n.t("general:retry")}
				</Button>
			{/if}
		</div>
	{:else if controller.loading}
		<div class="ridu-versions__message" data-ridu-loading-surface="versions">
			<Skeleton class="h-12" /><Skeleton class="h-72" />
		</div>
	{:else if !collection}
		<div class="ridu-versions__message">
			<Banner tone="warning">{i18n.t("versions:resourceUnavailable")}</Banner>
		</div>
	{:else if requestedRevision !== undefined}
		{#if controller.selectedVersion}
			{#key owner}
				<VersionComparison
					{controller}
					{collection}
					{search}
					onQueryChange={updateQuery}
					onRestored={() => navigate(editPath)}
				/>
			{/key}
		{:else}
			<div class="ridu-versions__message">
				<Banner tone="warning">{i18n.t("versions:versionUnavailable")}</Banner>
			</div>
		{/if}
	{:else}
		<div class="ridu-versions__list">
			<VersionTable
				versions={controller.versions}
				document={controller.document}
				{search}
				onQueryChange={updateQuery}
				{versionPath}
			/>
		</div>
	{/if}
</section>
