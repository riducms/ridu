<script lang="ts">
	import { Dialog } from "bits-ui";
	import { Link, useNavigate } from "@hvniel/svelte-router";
	import type { FieldReferenceBrowserProps } from "@riducms/plugin";
	import XIcon from "~icons/lucide/x";
	import ArrowLeftIcon from "~icons/lucide/arrow-left";

	import { ConfirmationDialog } from "@admin/components/ui/confirmation-dialog";
	import { Banner } from "@admin/components/ui/banner";
	import { getNotificationCenter } from "@admin/core/notifications/notification-center.svelte";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import { documentPath, withContentLocale } from "@admin/core/routing/admin-paths";
	import { ReferenceBrowserWorkflow } from "@admin/features/reference-browser/reference-browser-workflow.svelte";
	import { provideDrawerDepth } from "@admin/components/ui/drawer/drawer-depth";
	import ReferenceCollectionPicker from "@admin/features/reference-browser/reference-collection-picker.svelte";
	import ReferenceBrowserEditor from "@admin/features/reference-browser/reference-browser-editor.svelte";
	import ReferenceBrowserList from "@admin/features/reference-browser/reference-browser-list.svelte";
	import "@admin/features/reference-browser/reference-browser.scss";

	let {
		open = $bindable(false),
		field,
		collection: initialCollection,
		collections,
		hasMany,
		selectedIDs,
		readOnly = false,
		initialDocument,
		initialDocumentID,
		initialCreate,
		initialFile,
		optionFilter,
		defaultValues,
		allowCreate = true,
		locale,
		onCommit,
		onClose,
	}: FieldReferenceBrowserProps = $props();

	const runtime = getAdminRuntime();
	const navigate = useNavigate();
	const notifications = getNotificationCenter();
	const depth = provideDrawerDepth();
	const i18n = runtime.i18n;
	// Only the focus handler reads this DOM binding.
	// svelte-ignore non_reactive_update
	let contentElement: HTMLElement | null = null;
	let searchElement = $state.raw<HTMLInputElement | null>(null);

	// A field mounts one workflow per drawer session. Only open/editability stay live.
	// svelte-ignore state_referenced_locally
	const controller = new ReferenceBrowserWorkflow({
		runtime,
		notifications,
		field,
		collection: initialCollection,
		collections,
		hasMany,
		selectedIDs,
		get readOnly() {
			return readOnly;
		},
		initialDocument,
		initialDocumentID,
		initialCreate,
		initialFile,
		optionFilter,
		defaultValues,
		allowCreate,
		locale,
		onCommit,
		onClose,
		get open() {
			return open;
		},
		setOpen(value) {
			open = value;
		},
	});

	const { availableCollections } = controller;
	const {
		collection,
		browsingUnavailable,
		screen,
		creating,
		editorTitle,
		editorDocument,
		editorLoading,
		committing,
	} = $derived(controller);
	const pluralLabel = $derived(
		i18n.text(collection.labels.plural, collection.labels.pluralTranslations)
	);
	const singularLabel = $derived(
		i18n.text(collection.labels.singular, collection.labels.singularTranslations)
	);
	const fieldLabel = $derived(i18n.text(field.admin.label, field.admin.labelTranslations));
	const dialogLabel = $derived(
		screen === "list"
			? i18n.t(hasMany ? "reference:addLabel" : "reference:selectLabel", {
					label: (collections === undefined ? fieldLabel : singularLabel).toLocaleLowerCase(
						i18n.language
					),
				})
			: creating
				? i18n.t("reference:newLabel", { label: singularLabel.toLocaleLowerCase(i18n.language) })
				: editorTitle
	);
	const busy = $derived(committing || controller.form.submitting);

	$effect(() => {
		if (screen === "list") searchElement?.focus();
	});

	function focusBrowserSearch(event: Event) {
		event.preventDefault();
		if (screen === "list") searchElement?.focus();
		else contentElement?.focus();
	}
</script>

<Dialog.Root bind:open={() => open, controller.requestOpenChange}>
	<Dialog.Portal>
		<Dialog.Overlay class="ridu-reference-overlay" />
		<Dialog.Content
			bind:ref={contentElement}
			class="ridu-reference-drawer"
			style="--reference-depth: {depth}"
			dir={i18n.direction}
			aria-label={dialogLabel}
			onOpenAutoFocus={focusBrowserSearch}
		>
			<header class="ridu-reference-header" data-document={screen === "document"}>
				<div class="ridu-reference-heading">
					{#if screen === "document" && !controller.directEntry}
						<button
							type="button"
							class="ridu-reference-icon"
							disabled={busy}
							onclick={controller.requestBack}
							aria-label={i18n.t("reference:backToResults")}
						>
							<ArrowLeftIcon />
						</button>
					{/if}
					<h2 class="ridu-reference-title">
						{screen === "list" ? pluralLabel : editorLoading ? singularLabel : editorTitle}
					</h2>
					{#if screen === "list" && controller.canCreateDocument}
						<button
							type="button"
							class="ridu-list__pill"
							disabled={busy}
							onclick={controller.openNewDocument}
						>
							{i18n.t("collections:createNewButton")}
						</button>
					{/if}
					<button
						type="button"
						class="ridu-reference-close"
						disabled={busy}
						onclick={() => controller.requestOpenChange(false)}
						aria-label={i18n.t("reference:closeBrowser")}
					>
						<XIcon />
					</button>
				</div>
				{#if screen === "list" && availableCollections.length > 1}
					<ReferenceCollectionPicker
						collections={availableCollections}
						value={collection.slug}
						disabled={busy}
						onchange={controller.selectCollection}
					/>
				{/if}
				{#if screen === "document" && editorDocument}
					<div class="ridu-reference-id">
						{const destination = withContentLocale(
							documentPath(collection.slug, editorDocument.id),
							locale
						)}
						{i18n.t("fields:id")}:
						{#if collection.admin.hidden === true}
							<!-- A hidden collection has no document page to open. -->
							{editorDocument.id}
						{:else}
							<Link
								to={destination}
								onclick={(event) => {
									if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
									event.preventDefault();
									controller.requestNavigation(() => navigate(destination));
								}}
							>
								{editorDocument.id}
							</Link>
						{/if}
					</div>
				{/if}
				<Dialog.Description class="sr-only">
					{i18n.t("reference:browserDescription", { label: pluralLabel })}
				</Dialog.Description>
			</header>

			{#if screen === "list"}
				{#if browsingUnavailable}
					<div class="ridu-reference-fields">
						<Banner tone="warning">{i18n.t("reference:noCollectionsAvailable")}</Banner>
					</div>
				{:else}
					{#key collection.slug}
						<ReferenceBrowserList {controller} bind:searchElement />
					{/key}
				{/if}
			{:else}
				<ReferenceBrowserEditor {controller} />
			{/if}
		</Dialog.Content>
	</Dialog.Portal>
</Dialog.Root>

<ConfirmationDialog
	bind:open={controller.confirmDiscard}
	title={i18n.t("reference:discardChanges")}
	description={i18n.t("reference:discardDescription")}
	confirmLabel={i18n.t("reference:discard")}
	cancelLabel={i18n.t("reference:keepEditing")}
	destructive
	disabled={controller.form.submitting}
	onconfirm={controller.discardChanges}
/>
