<script lang="ts">
	import { useLocation, useParams } from "@hvniel/svelte-router";
	import type { AdminCoreViewHost, AdminCoreViewSurface } from "@riducms/plugin";
	import type { Component } from "svelte";

	import AdminViewHost from "@admin/core/loaders/admin-view-host.svelte";
	import { resolveCoreView } from "@admin/core/routing/admin-view-selection";
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
	const coreViews = runtime.config.extensions.coreViews;
	const params = useParams<"collection" | "document" | "global" | "view">();
	const location = useLocation();
	const manifest = $derived(runtime.manifest);
	const documentID = $derived(
		surface === "collectionEdit" ? documentIDFromAdminPath(location.current.pathname) : undefined
	);
	const collection = $derived(
		manifest?.collections.find((candidate) => candidate.slug === params.current.collection)
	);
	const global = $derived(
		manifest?.globals?.find((candidate) => candidate.slug === params.current.global)
	);
	const resource = $derived(
		surface === "global" ? params.current.global : params.current.collection
	);
	const resourceAvailable = $derived(
		surface === "notFound" ||
			(surface === "global" ? global !== undefined : collection !== undefined)
	);
	const replacement = $derived(
		resourceAvailable ? resolveCoreView(coreViews, surface, resource) : undefined
	);
	const host: AdminCoreViewHost = {
		refreshManifest: () => runtime.refreshManifest(),
		documentsChanged: () => runtime.documentsChanged(),
		notify: (tone, title, message) => notifications[tone]({ title, message }),
	};
</script>

{#snippet defaultView()}
	<DefaultView />
{/snippet}

{#if manifest !== undefined && replacement !== undefined}
	{#key replacement}
		<AdminViewHost
			view={replacement}
			ownsRoute
			props={{
				manifest,
				user: runtime.session?.user,
				surface,
				collection,
				global,
				documentID,
				defaultView,
				host,
				i18n: runtime.i18n,
			}}
		/>
	{/key}
{:else}
	{@render defaultView()}
{/if}
