<script lang="ts">
	import { useLocation, useParams } from "@hvniel/svelte-router";
	import type { AdminCoreView, AdminCoreViewHost, AdminCoreViewSurface } from "@riducms/plugin";
	import { untrack, type Component } from "svelte";

	import { getNotificationCenter } from "@admin/core/notifications/notification-center.svelte";
	import { documentIDFromAdminPath } from "@admin/core/routing/admin-paths";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";

	interface Props {
		surface: AdminCoreViewSurface;
		DefaultView: Component;
	}

	let { surface, DefaultView }: Props = $props();
	const runtime = getAdminRuntime();
	const notifications = getNotificationCenter();
	const params = $derived(useParams<"collection" | "document" | "global" | "view">());
	const location = $derived(useLocation());
	let documentID = $state(
		untrack(() =>
			params.document === undefined ? undefined : documentIDFromAdminPath(location.pathname)
		)
	);
	$effect(() => {
		if (params.document === undefined) {
			documentID = undefined;
			return;
		}
		const decoded = documentIDFromAdminPath(location.pathname);
		if (decoded !== undefined) documentID = decoded;
	});
	const collection = $derived(
		runtime.manifest?.collections.find((candidate) => candidate.slug === params.collection)
	);
	const global = $derived(
		runtime.manifest?.globals?.find((candidate) => candidate.slug === params.global)
	);
	const replacement = $derived(
		resolveCoreView(runtime.coreViews, surface, params.collection, params.global)
	);
	const host: AdminCoreViewHost = {
		refreshManifest: () => runtime.refreshManifest(),
		documentsChanged: () => runtime.documentsChanged(),
		notify: (tone, title, message) => notifications[tone]({ title, message }),
	};

	function resolveCoreView(
		views: readonly AdminCoreView[],
		viewSurface: AdminCoreViewSurface,
		collectionSlug: string | undefined,
		globalSlug: string | undefined
	) {
		const candidates = views.filter((view) => view.surface === viewSurface);
		if (viewSurface === "global") {
			return (
				candidates.find((view) => "global" in view && view.global === globalSlug) ??
				candidates.find((view) => "global" in view && view.global === undefined)
			);
		}
		if (viewSurface !== "notFound") {
			return (
				candidates.find((view) => "collection" in view && view.collection === collectionSlug) ??
				candidates.find((view) => "collection" in view && view.collection === undefined)
			);
		}
		return candidates[0];
	}
</script>

{#snippet defaultView()}
	<DefaultView />
{/snippet}

{#if runtime.manifest !== undefined && replacement !== undefined}
	<replacement.component
		manifest={runtime.manifest}
		user={runtime.session?.user}
		{surface}
		{collection}
		{global}
		{documentID}
		{defaultView}
		{host}
		i18n={runtime.i18n}
	/>
{:else}
	{@render defaultView()}
{/if}
