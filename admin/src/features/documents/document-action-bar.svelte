<script lang="ts">
	import type { AdminDocumentExtensionHost } from "@riducms/plugin";
	import type { DocumentController } from "@admin/features/documents/document-controller.svelte";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import {
		Button,
		Dialog,
		DialogContent,
		DialogDescription,
		DialogFooter,
		DialogHeader,
		DialogTitle,
		buttonVariants,
	} from "@riducms/ui";
	import { Popover, PopoverContent, PopoverTrigger } from "@admin/components/ui/popover";
	import { Select, SelectContent, SelectItem, SelectTrigger } from "@admin/components/ui/select";
	import { Link } from "@hvniel/svelte-router";
	import { createDocumentPath, withContentLocale } from "@admin/core/routing/admin-paths";
	import DocumentSchedule from "@admin/features/documents/document-schedule.svelte";
	import ChevronDownIcon from "~icons/lucide/chevron-down";
	import "@admin/features/documents/document-schedule.scss";
	import KeyRoundIcon from "~icons/lucide/key-round";
	import EyeIcon from "~icons/lucide/eye";
	import MoreVerticalIcon from "~icons/lucide/more-vertical";
	import LockKeyholeIcon from "~icons/lucide/lock-keyhole";

	const runtime = getAdminRuntime();
	let {
		controller,
		extensionHost,
		localeEnabled,
		activeLocale,
		livePreviewOpen,
		onTogglePreview,
		onSave,
		onPublish,
		height = $bindable(0),
	}: {
		controller: DocumentController;
		extensionHost: AdminDocumentExtensionHost;
		localeEnabled: boolean;
		activeLocale: string | undefined;
		livePreviewOpen: boolean;
		onTogglePreview: () => void;
		onSave: (event: Event) => Promise<void>;
		onPublish: (event: Event) => Promise<void>;
		/** The bar's rendered height, so content can stick below it. */
		height?: number;
	} = $props();
	// The document controller owns one form for its full lifetime.
	// svelte-ignore state_referenced_locally
	const form = controller.form;
	const localization = $derived(runtime.manifest?.application.localization);
	const livePreview = $derived(controller.collection?.admin.livePreview);
	let moreActionsOpen = $state(false);
	let publishMenuOpen = $state(false);
	let scheduleOpen = $state(false);
	let discardDraftOpen = $state(false);
	let now = $state.raw(Date.now());
	$effect(() => {
		if (
			controller.lastSavedAt === undefined ||
			!controller.collection?.versionSettings?.autosaveIntervalSeconds
		)
			return;
		now = Date.now();
		const timer = window.setInterval(() => (now = Date.now()), 30_000);
		return () => window.clearInterval(timer);
	});
	const lastSavedDistance = $derived.by(() => {
		if (controller.lastSavedAt === undefined) return undefined;
		const elapsedMinutes = Math.floor(Math.max(0, now - controller.lastSavedAt) / 60_000);
		if (elapsedMinutes === 0) return runtime.i18n.t("documents:lessThanMinuteAgo");
		const relative = new Intl.RelativeTimeFormat(runtime.i18n.language, { numeric: "always" });
		if (elapsedMinutes < 60) return relative.format(-elapsedMinutes, "minute");
		const elapsedHours = Math.floor(elapsedMinutes / 60);
		if (elapsedHours < 24) return relative.format(-elapsedHours, "hour");
		return relative.format(-Math.floor(elapsedHours / 24), "day");
	});
	const showCopyLocale = $derived(
		localeEnabled &&
			activeLocale !== undefined &&
			localization !== undefined &&
			!controller.creating &&
			controller.currentDocument !== undefined &&
			localization.locales.length > 1
	);
	const showSchedule = $derived(
		!controller.globalResource &&
			!controller.creating &&
			controller.versionedCollection &&
			(controller.canPublish ||
				(controller.currentStatus === "published" && controller.canUnpublish))
	);
	const customDocumentActions = $derived(
		controller.globalResource
			? []
			: runtime.config.extensions.documentActions.filter(
					(action) =>
						(action.collection === undefined || action.collection === controller.collectionSlug) &&
						(action.requires === undefined || form.access?.operations[action.requires] === true)
				)
	);
	const showDuplicate = $derived(
		!controller.globalResource && !controller.creating && controller.canDuplicate
	);
	const showDownload = $derived(
		!controller.globalResource &&
			!controller.creating &&
			controller.uploadCollection &&
			controller.assetURL !== undefined
	);
	const showUnpublish = $derived(
		!controller.creating &&
			controller.versionedCollection &&
			controller.currentStatus === "published" &&
			controller.canUnpublish
	);
	const showCreate = $derived(
		!controller.globalResource &&
			!controller.creating &&
			runtime.collectionOperations[controller.collectionSlug]?.create === true
	);
	const showDelete = $derived(
		!controller.globalResource && !controller.creating && controller.canDelete
	);
	const showDiscardDraft = $derived(controller.canDiscardSavedDraft);

	const primarySave = $derived.by(() => {
		const publishesOnCreate =
			controller.creating &&
			controller.versionedCollection &&
			(!controller.draftsCollection || controller.canPublish);
		if (publishesOnCreate)
			return {
				label: runtime.i18n.t("documents:publish"),
				disabled: !controller.canSave || !controller.canPublish,
				onclick: onPublish,
			};
		if (!controller.creating && controller.versionedCollection && controller.canPublish) {
			const publishingSavedDraft =
				!controller.hasUnsavedChanges &&
				(controller.currentStatus === "draft" || controller.hasSavedDraftChanges);
			const publishBlocked = publishingSavedDraft
				? controller.currentDocument === undefined ||
					controller.publicationOperation ||
					controller.serverSaveConflict ||
					controller.saveOutcomeUncertain ||
					controller.upload.busy ||
					form.submitting ||
					controller.loading
				: !controller.canSave || !controller.hasUnsavedChanges;
			return {
				label: runtime.i18n.t("documents:publishChanges"),
				disabled: !controller.canPublish || publishBlocked,
				onclick: onPublish,
			};
		}
		return {
			label: runtime.i18n.t(
				controller.versionedCollection && controller.draftsCollection
					? "documents:saveDraft"
					: "documents:save"
			),
			disabled: !controller.canSave,
			onclick: onSave,
		};
	});
</script>

<div class="ridu-document-bar" bind:offsetHeight={height}>
	<div class="ridu-document-metadata">
		{#if controller.creating}
			<span>
				{runtime.i18n.t("documents:creatingLabel", { label: controller.collectionSingularLabel })}
			</span>
		{:else if controller.versionedCollection}
			<span>
				{runtime.i18n.t("documents:status")}:
				<strong class="ridu-document-metadata-value">
					{runtime.i18n.t(
						controller.currentStatus === "draft" ? "documents:draft" : "documents:published"
					)}
				</strong>
				{#if controller.currentStatus === "published" && controller.hasSavedDraftChanges}
					<span class="ridu-document-metadata__saved-draft">
						{runtime.i18n.t("documents:savedDraftChanges")}
					</span>
				{/if}
			</span>
		{/if}
		{#if controller.lastSavedAt !== undefined && lastSavedDistance !== undefined && controller.collection?.versionSettings?.autosaveIntervalSeconds}
			<span class="ridu-document-metadata__saved">
				{runtime.i18n.t("documents:lastSavedAgo", { distance: lastSavedDistance })}
			</span>
		{/if}
		{#if !controller.creating && controller.currentDocument !== undefined}
			<span>
				{runtime.i18n.t("documents:lastModifiedLabel")}:
				<strong class="ridu-document-metadata-value">
					{controller.documentDate(controller.currentDocument.updatedAt)}
				</strong>
			</span>
			<span>
				{runtime.i18n.t("documents:createdLabelPlain")}:
				<strong class="ridu-document-metadata-value">
					{controller.documentDate(controller.currentDocument.createdAt)}
				</strong>
			</span>
		{/if}
	</div>

	<div class="ridu-document-actions">
		{#if controller.collection !== undefined && controller.currentDocument !== undefined && !controller.lock.lockedByAnotherEditor}
			{#each customDocumentActions as action (action.key)}
				<action.component
					collection={controller.collection}
					document={controller.currentDocument}
					host={extensionHost}
					i18n={runtime.i18n}
				/>
			{/each}
		{/if}

		{#if !controller.creating && controller.currentDocument !== undefined && livePreview !== undefined}
			<Button
				variant="outline"
				size="icon-sm"
				onclick={onTogglePreview}
				aria-label={runtime.i18n.t("documents:livePreview")}
				aria-pressed={livePreviewOpen}
			>
				<EyeIcon aria-hidden="true" />
			</Button>
		{/if}
		{#if controller.lock.canTakeOver}
			<Button
				variant="outline"
				size="sm"
				disabled={controller.lock.operation}
				onclick={controller.lock.takeOver}
			>
				<LockKeyholeIcon aria-hidden="true" />{controller.lock.operation
					? runtime.i18n.t("documents:takingOver")
					: runtime.i18n.t("documents:takeOver")}
			</Button>
		{/if}

		{const offersDraftChoice = $derived(
			controller.draftsCollection &&
				controller.canPublish &&
				!controller.creatingAuthUser &&
				(controller.creating || controller.hasUnsavedChanges)
		)}
		{#if offersDraftChoice}
			<Button
				variant="outline"
				size="sm"
				disabled={!controller.canSave}
				aria-busy={form.submitting}
				onclick={onSave}
			>
				{runtime.i18n.t("documents:saveDraft")}
			</Button>
		{/if}
		<div class={{ "ridu-document-publish": showSchedule }}>
			<Button
				size="sm"
				disabled={primarySave.disabled}
				aria-busy={form.submitting}
				onclick={primarySave.onclick}
			>
				{primarySave.label}
			</Button>
			{#if showSchedule}
				<Popover bind:open={publishMenuOpen}>
					<PopoverTrigger
						class={[
							buttonVariants({ size: "sm", class: "ridu-document-publish__toggle" }),
							{ "ridu-document-publish__toggle--inactive": primarySave.disabled },
						]}
						aria-label={runtime.i18n.t("documents:schedule")}
					>
						<ChevronDownIcon />
					</PopoverTrigger>
					<PopoverContent align="end" class="ridu-document-menu">
						<Button
							variant="ghost"
							class="ridu-document-menu__item"
							onclick={() => {
								publishMenuOpen = false;
								scheduleOpen = true;
							}}
						>
							{runtime.i18n.t("versions:schedulePublication")}
						</Button>
					</PopoverContent>
				</Popover>
			{/if}
		</div>

		<div class="ridu-document-actions__menu">
			{#if showDuplicate || showDownload || showUnpublish || showDiscardDraft || showDelete || controller.canForceUnlock || showCopyLocale || showCreate}
				<Popover bind:open={moreActionsOpen}>
					<PopoverTrigger
						class={buttonVariants({
							variant: "outline",
							size: "sm",
							class: "ridu-document-actions__more",
						})}
						aria-label={runtime.i18n.t("documents:moreActions")}
					>
						<MoreVerticalIcon aria-hidden="true" />
					</PopoverTrigger>
					<PopoverContent align="end" class="ridu-document-menu">
						{#if showCopyLocale && localization}
							<div class="ridu-document-menu__locale">
								<Select
									type="single"
									value=""
									disabled={controller.hasUnsavedChanges ||
										controller.loading ||
										form.submitting ||
										controller.copyLocaleOperation}
									onValueChange={controller.copyFromLocale}
								>
									<SelectTrigger
										size="compact"
										class="ridu-document-menu__locale-trigger"
										aria-label={runtime.i18n.t("documents:copyFromLocale")}
									>
										<span>
											{controller.copyLocaleOperation
												? runtime.i18n.t("documents:copying")
												: runtime.i18n.t("documents:copyFrom")}
										</span>
									</SelectTrigger>
									<SelectContent>
										{#each localization.locales.filter((locale) => locale.code !== activeLocale) as locale (locale.code)}
											<SelectItem value={locale.code} label={locale.label}>
												<span>{locale.label}</span>
												<span class="ridu-document-menu__locale-code">{locale.code}</span>
											</SelectItem>
										{/each}
									</SelectContent>
								</Select>
							</div>
						{/if}

						{#if showCreate}
							<Link
								class="ridu-document-menu__item ridu-document-menu__link"
								to={withContentLocale(createDocumentPath(controller.collectionSlug), activeLocale)}
							>
								{runtime.i18n.t("collections:createNewButton")}
							</Link>
						{/if}
						{#if showDuplicate}
							<Button
								variant="ghost"
								class="ridu-document-menu__item"
								disabled={controller.duplicateOperation}
								onclick={() => {
									moreActionsOpen = false;
									controller.duplicate();
								}}
							>
								{controller.duplicateOperation
									? runtime.i18n.t("documents:duplicating")
									: runtime.i18n.t("documents:duplicate")}
							</Button>
						{/if}
						{#if showDownload}
							<Button
								variant="ghost"
								class="ridu-document-menu__item"
								href={controller.assetURL}
								download={controller.assetFilename}
							>
								{runtime.i18n.t("documents:download")}
							</Button>
						{/if}
						{#if showUnpublish}
							<Button
								variant="ghost"
								class="ridu-document-menu__item"
								disabled={controller.publicationOperation ||
									controller.serverSaveConflict ||
									controller.saveOutcomeUncertain ||
									controller.hasUnsavedChanges ||
									form.submitting}
								onclick={() => {
									moreActionsOpen = false;
									controller.changePublication("draft");
								}}
							>
								{runtime.i18n.t("documents:unpublish")}
							</Button>
						{/if}
						{#if showDiscardDraft}
							<Button
								variant="ghost"
								class="ridu-document-menu__item"
								onclick={() => {
									moreActionsOpen = false;
									discardDraftOpen = true;
								}}
							>
								{runtime.i18n.t("documents:discardSavedDraft")}
							</Button>
						{/if}
						{#if showDelete}
							<Button
								variant="ghost"
								class="ridu-document-menu__item"
								onclick={() => {
									moreActionsOpen = false;
									controller.deleteDialogOpen = true;
								}}
							>
								{runtime.i18n.t(
									controller.uploadCollection ? "documents:deleteAsset" : "documents:deleteDocument"
								)}
							</Button>
						{/if}
						{#if controller.canForceUnlock}
							<Button
								variant="ghost"
								class="ridu-document-menu__item"
								disabled={controller.unlockOperation}
								onclick={() => {
									moreActionsOpen = false;
									controller.forceUnlock();
								}}
							>
								<KeyRoundIcon aria-hidden="true" />{controller.unlockOperation
									? runtime.i18n.t("documents:unlocking")
									: runtime.i18n.t("documents:forceUnlock")}
							</Button>
						{/if}
					</PopoverContent>
				</Popover>
			{/if}
		</div>
	</div>
</div>

<Dialog bind:open={discardDraftOpen}>
	<DialogContent variant="confirmation">
		<DialogHeader>
			<DialogTitle>{runtime.i18n.t("documents:discardSavedDraftQuestion")}</DialogTitle>
			<DialogDescription>
				{runtime.i18n.t("documents:discardSavedDraftDescription")}
			</DialogDescription>
		</DialogHeader>
		<DialogFooter>
			<Button variant="outline" onclick={() => (discardDraftOpen = false)}>
				{runtime.i18n.t("documents:cancel")}
			</Button>
			<Button
				disabled={!showDiscardDraft}
				onclick={async () => {
					discardDraftOpen = false;
					await controller.discardSavedDraft();
				}}
			>
				{runtime.i18n.t("documents:discardSavedDraft")}
			</Button>
		</DialogFooter>
	</DialogContent>
</Dialog>

{#if scheduleOpen && showSchedule && controller.documentID}
	{#key `${controller.collectionSlug}:${controller.documentID}:${activeLocale}`}
		<DocumentSchedule
			slug={controller.collectionSlug}
			documentID={controller.documentID}
			title={controller.documentHeading}
			revision={controller.currentRevision}
			editable={!controller.lock.lockedByAnotherEditor && !form.submitting}
			canPublish={controller.canPublish}
			canUnpublish={controller.canUnpublish}
			status={controller.currentStatus}
			dirty={controller.hasUnsavedChanges}
			onClose={() => (scheduleOpen = false)}
		/>
	{/key}
{/if}
