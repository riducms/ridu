<script lang="ts">
	import APIReference from "@admin/features/api-reference/api-reference.svelte";
	import "@admin/features/collections/collection-list.scss";
	import "@admin/components/data/list-table.scss";
	import {
		Link,
		useHref,
		useLocation,
		useNavigation,
		useParams,
		useSearchParams,
	} from "@hvniel/svelte-router";
	import { Button } from "@riducms/ui";
	import { Banner } from "@admin/components/ui/banner";
	import {
		collectionPath,
		collectionTrashPath,
		withContentLocale,
	} from "@admin/core/routing/admin-paths";
	import { registerAdminScrollPage } from "@admin/core/routing/admin-scroll.svelte";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import { getAdminBootstrapCoordinator } from "@admin/core/bootstrap/admin-bootstrap";
	import { getNotificationCenter } from "@admin/core/notifications/notification-center.svelte";
	import {
		CollectionList,
		setCollectionList,
	} from "@admin/features/collections/collection-list.svelte";
	import { CollectionListQuery } from "@admin/features/collections/collection-list-query";
	import CollectionListResults from "@admin/features/collections/collection-list-results.svelte";
	import ListToolbar from "@admin/features/collections/controls/list-toolbar.svelte";
	import CollectionListBulkActions from "@admin/features/collections/bulk/bulk-actions.svelte";
	import CollectionListBulkEditorLoader from "@admin/features/collections/bulk/bulk-editor-loader.svelte";
	import BulkConfirmation, {
		type BulkConfirmationAction,
	} from "@admin/features/collections/bulk/bulk-confirmation.svelte";

	const runtime = getAdminRuntime();
	const bootstrap = getAdminBootstrapCoordinator();
	const navigation = useNavigation();
	const params = useParams<"collection">();
	const location = useLocation();
	const currentPath = useHref(() => location.current.pathname);
	const [searchParams, setSearchParams] = useSearchParams();

	const list = new CollectionList({
		runtime,
		notifications: getNotificationCenter(),
		query: new CollectionListQuery(searchParams, setSearchParams, () => {
			const destination = navigation.current.location;
			if (
				destination?.pathname === currentPath.current &&
				new URLSearchParams(destination.search).get("locale") === searchParams.current.get("locale")
			) {
				return destination.search;
			}
		}),
		get slug() {
			return params.current.collection ?? "";
		},
		get trashOnly() {
			return location.current.pathname.endsWith("/trash");
		},
		get navigationIdle() {
			return navigation.current.state === "idle";
		},
		prepared() {
			// A retained route may only adopt a seed for its exact pathname and query.
			const route = bootstrap.routeData(location.current.pathname, location.current.search);

			return route?.kind === "collection-list" || route?.kind === "collection-trash"
				? route.data
				: undefined;
		},
	});

	setCollectionList(list);
	registerAdminScrollPage({ ready: () => list.ready });

	const { controller } = list;
	const { slug, collection, trashOnly, contentLocale, filtered } = $derived(list);
	const { canCreate, error, pagination, bulkPending } = $derived(controller);
	const { retry } = controller;

	const collectionViews = $derived([
		{ trash: false, label: runtime.i18n.t("collections:allDocuments", { label: list.label }) },
		{ trash: true, label: runtime.i18n.t("collections:trash") },
	]);

	let bulkEditOpen = $state(false);
	let confirmationOpen = $state(false);
	let confirmationAction = $state<BulkConfirmationAction>("delete-selected");

	let searchInput = $state<HTMLInputElement | null>(null);
	let focusSearchAfterNavigation = $state(false);

	$effect(() => {
		if (list.navigationIdle && focusSearchAfterNavigation) {
			focusSearchAfterNavigation = false;
			searchInput?.focus();
		}
	});

	function clearFiltersAndFocusSearch() {
		focusSearchAfterNavigation = true;
		return list.query.clearFilters();
	}

	function contentPath(path: string) {
		return withContentLocale(path, contentLocale);
	}

	function requestConfirmation(action: BulkConfirmationAction) {
		confirmationAction = action;
		confirmationOpen = true;
	}

	function openBulkEdit() {
		bulkEditOpen = true;
	}
</script>

<section class="ridu-list">
	<header class="ridu-list__header" inert={!list.navigationIdle}>
		<div class="ridu-list__title">
			<h1>{collection?.labels.plural ?? slug}</h1>
			{#if !trashOnly && canCreate}
				<Link
					class="ridu-list__pill"
					aria-label={runtime.i18n.t("collections:createNew", {
						label: collection?.labels.singular ?? slug,
					})}
					to={list.createHref}
				>
					{runtime.i18n.t("collections:createNewButton")}
				</Link>
			{/if}
			{#if list.uploadHref !== undefined}
				<Link class="ridu-list__pill" to={list.uploadHref}>
					{runtime.i18n.t("collections:bulkUpload")}
				</Link>
			{/if}
			{#if trashOnly}
				<button
					type="button"
					class="ridu-list__pill"
					disabled={bulkPending || (pagination.totalDocs === 0 && !filtered)}
					onclick={() => requestConfirmation("empty-trash")}
				>
					{runtime.i18n.t("collections:emptyTrash")}
				</button>
			{/if}
		</div>

		<div class="ridu-list__header-actions">
			{#if collection}
				{#key collection.slug}
					<APIReference {collection} />
				{/key}
			{/if}
			<CollectionListBulkActions
				onEdit={openBulkEdit}
				onRequestPublish={() => requestConfirmation("publish-selected")}
				onRequestUnpublish={() => requestConfirmation("unpublish-selected")}
				onRequestDelete={() => requestConfirmation("delete-selected")}
				onRequestRestore={() => requestConfirmation("restore-selected")}
				onRequestDeletePermanent={() => requestConfirmation("delete-selected-permanently")}
			/>

			{#if collection?.capabilities.trash}
				<nav class="ridu-list__views" aria-label={runtime.i18n.t("collections:collectionViews")}>
					{#each collectionViews as view}
						{#if trashOnly === view.trash}
							<button type="button" class="ridu-list__view" aria-current="page" disabled>
								{view.label}
							</button>
						{:else}
							<Link
								class="ridu-list__view"
								to={contentPath(view.trash ? collectionTrashPath(slug) : collectionPath(slug))}
							>
								{view.label}
							</Link>
						{/if}
					{/each}
				</nav>
			{/if}
		</div>
	</header>

	{#if error !== undefined}
		<Banner class="ridu-list__error" tone="destructive">
			<span class="ridu-list__error-marker" aria-hidden="true"></span>
			<span class="ridu-list__error-message">{error}</span>
			<Button variant="outline" size="sm" onclick={retry}>
				{runtime.i18n.t("general:retry")}
			</Button>
		</Banner>
	{/if}

	{#key slug}
		<ListToolbar bind:searchInput />
	{/key}

	<CollectionListResults onClearFilters={clearFiltersAndFocusSearch} />
</section>

<BulkConfirmation bind:open={confirmationOpen} action={confirmationAction} />

<CollectionListBulkEditorLoader bind:open={bulkEditOpen} />
