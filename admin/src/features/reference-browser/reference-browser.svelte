<script lang="ts">
	import LiveValidationFeedback from "@admin/fields/live-validation-feedback.svelte";
	import type { FieldReferenceBrowserProps } from "@riducms/plugin";
	import { fieldControlARIA } from "@riducms/ui";
	import { tick } from "svelte";
	import ArrowLeftIcon from "~icons/lucide/arrow-left";
	import ChevronLeftIcon from "~icons/lucide/chevron-left";
	import ChevronRightIcon from "~icons/lucide/chevron-right";
	import FileIcon from "~icons/lucide/file";
	import FileUpIcon from "~icons/lucide/file-up";
	import PencilIcon from "~icons/lucide/pencil";
	import PlusIcon from "~icons/lucide/plus";
	import SearchIcon from "~icons/lucide/search";
	import XIcon from "~icons/lucide/x";

	import { Banner } from "@admin/components/ui/banner";
	import { Button } from "@admin/components/ui/button";
	import {
		Dialog,
		DialogContent,
		DialogDescription,
		DialogFooter,
		DialogHeader,
		DialogTitle,
	} from "@admin/components/ui/dialog";
	import { Input } from "@admin/components/ui/input";
	import { RadioGroup, RadioGroupItem } from "@admin/components/ui/radio-group";
	import JsonViewer from "@admin/components/json-tree/json-viewer.svelte";
	import { Sheet, SheetContent, SheetDescription, SheetTitle } from "@admin/components/ui/sheet";
	import { Skeleton } from "@admin/components/ui/skeleton";
	import { getNotificationCenter } from "@admin/core/notifications/notification-center.svelte";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import FieldLayout from "@admin/fields/field-layout.svelte";
	import FieldMessages from "@admin/fields/field-messages.svelte";

	import { ReferenceBrowserWorkflow } from "@admin/features/reference-browser/reference-browser-workflow.svelte";

	let {
		open = $bindable(false),
		field,
		collection,
		hasMany,
		selectedIDs,
		readOnly = false,
		initialDocument,
		initialDocumentID,
		optionFilter,
		defaultValues,
		allowCreate = true,
		locale,
		onCommit,
		onClose,
	}: FieldReferenceBrowserProps = $props();

	const runtime = getAdminRuntime();
	const notifications = getNotificationCenter();
	const fieldLabel = $derived(runtime.i18n.text(field.admin.label, field.admin.labelTranslations));
	const singularLabel = $derived(
		runtime.i18n.text(collection.labels.singular, collection.labels.singularTranslations)
	);
	const pluralLabel = $derived(
		runtime.i18n.text(collection.labels.plural, collection.labels.pluralTranslations)
	);
	const lowerSingularLabel = $derived(singularLabel.toLocaleLowerCase(runtime.i18n.language));
	const lowerPluralLabel = $derived(pluralLabel.toLocaleLowerCase(runtime.i18n.language));
	let contentElement = $state<HTMLElement | null>(null);
	let searchElement = $state<HTMLInputElement | null>(null);
	// The field mounts a fresh browser for each drawer session, so workflow inputs other than the
	// controlled open/editability state intentionally describe that session rather than later prop replacements.
	// svelte-ignore state_referenced_locally
	const controller = new ReferenceBrowserWorkflow({
		runtime,
		notifications,
		field,
		collection,
		hasMany,
		selectedIDs,
		get readOnly() {
			return readOnly;
		},
		initialDocument,
		initialDocumentID,
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
	const { editorFields, filters, form, headingField } = controller;
	const {
		canCommitSelection,
		canCreateDocument,
		canSave,
		committing,
		creating,
		docs,
		editorDocument,
		editorError,
		editorLoading,
		editorTitle,
		editorView,
		error,
		filterKey,
		headingIssues,
		pagination,
		query,
		rangeEnd,
		rangeStart,
		screen,
		selectedFile,
		status,
		workingSelection,
	} = $derived(controller);
	const {
		chooseFilter,
		commitSelection,
		discardChanges,
		documentLabel,
		documentSubtitle,
		formatBytes,
		handleSearch,
		isImage,
		mediaURL,
		openKnownDocument,
		openNewDocument,
		requestBack,
		requestOpenChange,
		saveEditor,
		toggleSelection,
	} = controller;
	const visibleSingleSelection = $derived(
		hasMany || !docs.some((document) => document.id === workingSelection[0])
			? ""
			: (workingSelection[0] ?? "")
	);
	const visibleResultIdentity = $derived(docs.map((document) => document.id).join("\u001f"));

	function focusBrowserSearch(event: Event) {
		if (screen !== "list" || searchElement === null) return;
		event.preventDefault();
		searchElement.focus();
	}

	async function handleSave(event: Event) {
		event.preventDefault();
		if (await saveEditor()) return;
		if (controller.credentialIssue !== undefined) {
			await tick();
			contentElement?.querySelector<HTMLElement>("#ridu-reference-new-user-password")?.focus();
			return;
		}
		const issue = form.issues[0];
		if (issue === undefined) return;
		await tick();
		const segments = issue.path.split(".");
		while (segments.length > 0) {
			const container = contentElement?.querySelector<HTMLElement>(
				`[data-field-path="${CSS.escape(segments.join("."))}"]`
			);
			if (container !== null && container !== undefined) {
				container.scrollIntoView({ behavior: "smooth", block: "center" });
				container
					.querySelector<HTMLElement>("input, textarea, button, [tabindex]:not([tabindex='-1'])")
					?.focus({ preventScroll: true });
				return;
			}
			segments.pop();
		}
	}
</script>

{#snippet uploadResults()}
	{#each docs as document (document.id)}
		<div class="group min-w-0">
			<div
				class={[
					"relative aspect-[4/3] overflow-hidden rounded-[4px] border bg-control",
					workingSelection.includes(document.id) ? "border-primary/60" : "border-control-border",
				]}
			>
				{#if isImage(document) && mediaURL(document) !== undefined}
					<img
						class="size-full object-cover"
						src={mediaURL(document)}
						alt={typeof document.alt === "string" ? document.alt : ""}
					/>
				{:else}
					<div class="grid size-full place-items-center">
						<FileIcon class="size-6 text-foreground-faint" />
					</div>
				{/if}
				{#if hasMany}
					<button
						type="button"
						class="absolute inset-0 outline-none focus-visible:ring-2 focus-visible:ring-primary/70 focus-visible:ring-inset disabled:cursor-not-allowed"
						role="checkbox"
						aria-checked={workingSelection.includes(document.id)}
						aria-label={runtime.i18n.t(
							workingSelection.includes(document.id)
								? "reference:deselectLabel"
								: "reference:selectLabel",
							{ label: documentLabel(document) }
						)}
						disabled={readOnly || committing}
						onclick={() => toggleSelection(document.id)}
					></button>
				{:else}
					<RadioGroupItem
						value={document.id}
						class="absolute inset-0 rounded-none focus-visible:ring-primary/70 focus-visible:ring-inset"
						aria-label={runtime.i18n.t("reference:selectLabel", {
							label: documentLabel(document),
						})}
					/>
				{/if}
				<span
					class={[
						"pointer-events-none absolute top-2 start-2 grid size-4 place-items-center rounded-[4px] border text-[9px] font-bold",
						workingSelection.includes(document.id)
							? "border-primary bg-primary text-primary-foreground"
							: "border-media-foreground/25 bg-media-overlay",
					]}
					aria-hidden="true"
				>
					{workingSelection.includes(document.id) ? "✓" : ""}
				</span>
				<Button
					variant="secondary"
					size="icon-xs"
					class="absolute end-2 bottom-2"
					disabled={committing}
					onclick={() => openKnownDocument(document)}
					aria-label={runtime.i18n.t("reference:editLabel", {
						label: documentLabel(document),
					})}
				>
					<PencilIcon class="size-3" />
				</Button>
			</div>
			<p class="mt-1.5 truncate text-[11.5px] text-foreground-muted">
				{documentLabel(document)}
			</p>
		</div>
	{/each}
{/snippet}

{#snippet documentResults()}
	{#each docs as document (document.id)}
		<div
			class={[
				"flex items-center gap-2 rounded-[9px] border px-2 py-1.5",
				workingSelection.includes(document.id)
					? "border-primary/20 bg-primary/[0.055]"
					: "border-transparent hover:bg-control",
			]}
		>
			{#if hasMany}
				<button
					type="button"
					class="flex min-w-0 flex-1 items-center gap-3 rounded-[7px] px-1.5 py-1 text-start outline-none focus-visible:ring-2 focus-visible:ring-primary/60 disabled:cursor-not-allowed"
					role="checkbox"
					aria-checked={workingSelection.includes(document.id)}
					disabled={readOnly || committing}
					onclick={() => toggleSelection(document.id)}
				>
					{@render documentChoiceContent(document)}
				</button>
			{:else}
				<RadioGroupItem
					value={document.id}
					class="flex min-w-0 flex-1 items-center gap-3 rounded-[7px] px-1.5 py-1 text-start"
				>
					{@render documentChoiceContent(document)}
				</RadioGroupItem>
			{/if}
			<Button
				variant="outline"
				size="icon-sm"
				disabled={committing}
				onclick={() => openKnownDocument(document)}
				aria-label={runtime.i18n.t("reference:editLabel", {
					label: documentLabel(document),
				})}
			>
				<PencilIcon class="size-3" />
			</Button>
		</div>
	{/each}
{/snippet}

{#snippet documentChoiceContent(document: Record<string, unknown> & { id: string })}
	<span
		class={[
			"grid size-4 shrink-0 place-items-center border text-[9px] font-bold",
			hasMany ? "rounded-[4px]" : "rounded-full",
			workingSelection.includes(document.id)
				? "border-primary bg-primary text-primary-foreground"
				: "border-control-border-hover",
		]}
		aria-hidden="true"
	>
		{workingSelection.includes(document.id) ? "✓" : ""}
	</span>
	<span
		class="grid size-7 shrink-0 place-items-center rounded-full bg-control text-[9px] font-semibold text-foreground-muted"
	>
		{documentLabel(document).slice(0, 2).toLocaleUpperCase(runtime.i18n.language)}
	</span>
	<span class="min-w-0 flex-1">
		<span class="block truncate text-[13.5px] text-foreground-strong">
			{documentLabel(document)}
		</span>
		<span class="mt-0.5 block truncate text-[11.5px] text-foreground-faint">
			{documentSubtitle(document)}
		</span>
	</span>
{/snippet}

<Sheet bind:open={() => open, requestOpenChange}>
	<SheetContent
		bind:ref={contentElement}
		class="border-control-border bg-background-surface p-0"
		style="width: min(760px, 96vw); max-width: 760px"
		showCloseButton={false}
		onOpenAutoFocus={focusBrowserSearch}
		aria-label={screen === "list"
			? runtime.i18n.t("reference:selectLabel", { label: fieldLabel })
			: editorTitle}
	>
		<header
			class="flex min-h-16 shrink-0 items-center gap-3 border-b border-control-border px-4 sm:px-5"
		>
			{#if screen === "document"}
				<Button
					variant="outline"
					size="icon-sm"
					disabled={form.submitting}
					onclick={requestBack}
					aria-label={runtime.i18n.t("reference:backToResults")}
				>
					<ArrowLeftIcon class="size-3.5 rtl:rotate-180" />
				</Button>
			{/if}
			<div class="min-w-0 flex-1">
				<p class="font-mono truncate text-[9px] tracking-[0.13em] text-foreground-faint uppercase">
					{pluralLabel} / {screen === "document"
						? runtime.i18n.t("reference:document")
						: fieldLabel}
				</p>
				<SheetTitle class="font-serif mt-1 truncate text-[24px] leading-none text-foreground">
					{screen === "list"
						? runtime.i18n.t(hasMany ? "reference:addLabel" : "reference:selectLabel", {
								label: fieldLabel.toLocaleLowerCase(runtime.i18n.language),
							})
						: creating
							? runtime.i18n.t("reference:newLabel", { label: lowerSingularLabel })
							: editorTitle}
				</SheetTitle>
				<SheetDescription class="sr-only">
					{runtime.i18n.t("reference:browserDescription", { label: pluralLabel })}
				</SheetDescription>
			</div>
			<Button
				variant="ghost"
				size="icon-sm"
				disabled={committing || form.submitting}
				onclick={() => requestOpenChange(false)}
				aria-label={runtime.i18n.t("reference:closeBrowser")}
				tooltip={runtime.i18n.t("reference:closeBrowser")}
			>
				<XIcon class="size-4" />
			</Button>
		</header>

		{#if screen === "list"}
			<div class="shrink-0 border-b border-control-border px-4 py-3.5 sm:px-5">
				<div class="flex items-center gap-3">
					<div class="relative min-w-0 flex-1">
						<SearchIcon
							class="pointer-events-none absolute top-1/2 start-3.5 size-3.5 -translate-y-1/2 text-foreground-faint"
							aria-hidden="true"
						/>
						<label class="sr-only" for="{field.id}-relationship-search">
							{runtime.i18n.t("reference:search", { label: pluralLabel })}
						</label>
						<Input
							bind:ref={searchElement}
							id="{field.id}-relationship-search"
							type="search"
							disabled={committing}
							class="h-9 bg-control ps-9"
							value={query}
							oninput={handleSearch}
							placeholder={runtime.i18n.t("reference:search", { label: lowerPluralLabel })}
						/>
					</div>
					<p class="font-mono shrink-0 text-[9.5px] text-foreground-faint">
						{runtime.i18n.formatNumber(pagination.totalDocs)}
						{lowerPluralLabel}
					</p>
				</div>
				{#if filters.length > 1}
					<div
						class="mt-3 flex flex-wrap gap-1.5"
						aria-label={runtime.i18n.t("reference:collectionFilters")}
					>
						{#each filters as option (option.key)}
							<Button
								variant={filterKey === option.key ? "secondary" : "ghost"}
								size="sm"
								disabled={committing}
								class="h-7 rounded-full px-3 text-[11.5px]"
								onclick={() => chooseFilter(option.key)}
								aria-pressed={filterKey === option.key}
							>
								{option.label}
							</Button>
						{/each}
					</div>
				{/if}
			</div>

			<div class="min-h-0 flex-1 overflow-y-auto px-4 py-3 sm:px-5">
				{#if error !== undefined}
					<Banner tone="destructive">{error}</Banner>
				{/if}

				{#if status === "loading" && docs.length === 0}
					<div
						class={collection.capabilities.upload
							? "grid grid-cols-2 gap-3 sm:grid-cols-3"
							: "grid gap-2"}
						aria-label={runtime.i18n.t("reference:loadingRelated")}
					>
						{#each Array(collection.capabilities.upload ? 6 : 5) as _, index (index)}
							<Skeleton class={collection.capabilities.upload ? "aspect-[4/3]" : "h-15"} />
						{/each}
					</div>
				{:else if docs.length === 0}
					<div class="grid min-h-60 place-items-center px-5 text-center">
						<div>
							<p class="font-serif text-[23px] italic text-foreground-muted">
								{runtime.i18n.t("reference:nothingMatches")}
							</p>
							<p class="mt-2 text-[13px] text-foreground-faint">
								{runtime.i18n.t("reference:searchOrCreate", { label: lowerSingularLabel })}
							</p>
						</div>
					</div>
				{:else if collection.capabilities.upload}
					{#if hasMany}
						<div
							class="grid grid-cols-2 gap-3 sm:grid-cols-3"
							role="group"
							aria-label={runtime.i18n.t("reference:results")}
						>
							{@render uploadResults()}
						</div>
					{:else}
						{#key visibleResultIdentity}
							<RadioGroup
								class="grid grid-cols-2 gap-3 sm:grid-cols-3"
								aria-label={runtime.i18n.t("reference:results")}
								value={visibleSingleSelection}
								readonly={readOnly || committing}
								onValueChange={toggleSelection}
							>
								{@render uploadResults()}
							</RadioGroup>
						{/key}
					{/if}
				{:else}
					{#if hasMany}
						<div class="grid gap-1" role="group" aria-label={runtime.i18n.t("reference:results")}>
							{@render documentResults()}
						</div>
					{:else}
						{#key visibleResultIdentity}
							<RadioGroup
								class="grid gap-1"
								aria-label={runtime.i18n.t("reference:results")}
								value={visibleSingleSelection}
								readonly={readOnly || committing}
								onValueChange={toggleSelection}
							>
								{@render documentResults()}
							</RadioGroup>
						{/key}
					{/if}
				{/if}
			</div>

			<footer class="shrink-0 border-t border-control-border px-4 py-3.5 sm:px-5">
				<div class="mb-3 flex items-center justify-between gap-3">
					<p class="font-mono text-[10px] text-foreground-faint">
						{runtime.i18n.t("reference:range", {
							start: runtime.i18n.formatNumber(rangeStart),
							end: runtime.i18n.formatNumber(rangeEnd),
							total: runtime.i18n.formatNumber(pagination.totalDocs),
						})}
					</p>
					{#if pagination.totalPages > 1}
						<div class="flex items-center gap-1.5">
							<Button
								variant="outline"
								size="icon-sm"
								disabled={!pagination.hasPrevPage || status === "loading" || committing}
								onclick={() => (controller.page -= 1)}
								aria-label={runtime.i18n.t("reference:previousPage")}
							>
								<ChevronLeftIcon class="size-3.5 rtl:rotate-180" />
							</Button>
							<span class="font-mono min-w-10 text-center text-[10px] text-foreground-faint">
								{runtime.i18n.formatNumber(pagination.page)} / {runtime.i18n.formatNumber(
									pagination.totalPages
								)}
							</span>
							<Button
								variant="outline"
								size="icon-sm"
								disabled={!pagination.hasNextPage || status === "loading" || committing}
								onclick={() => (controller.page += 1)}
								aria-label={runtime.i18n.t("reference:nextPage")}
							>
								<ChevronRightIcon class="size-3.5 rtl:rotate-180" />
							</Button>
						</div>
					{/if}
				</div>
				<div class="flex flex-wrap items-center gap-2">
					{#if canCreateDocument}
						<Button variant="outline" disabled={committing} onclick={openNewDocument}>
							{#if collection.capabilities.upload}<FileUpIcon class="size-3.5" />{:else}<PlusIcon
									class="size-3.5"
								/>{/if}
							{collection.capabilities.upload
								? runtime.i18n.t("reference:uploadNew")
								: runtime.i18n.t("reference:createLabel", { label: lowerSingularLabel })}
						</Button>
					{/if}
					<span class="flex-1"></span>
					<Button variant="outline" disabled={committing} onclick={() => requestOpenChange(false)}>
						{runtime.i18n.t("reference:cancel")}
					</Button>
					{#if !readOnly}
						<Button disabled={!canCommitSelection || committing} onclick={commitSelection}>
							{committing
								? runtime.i18n.t("reference:applying")
								: hasMany
									? runtime.i18n.t("reference:addSelected", {
											count: runtime.i18n.formatNumber(workingSelection.length),
										})
									: runtime.i18n.t("reference:select")}
						</Button>
					{/if}
				</div>
			</footer>
		{:else}
			<div class="flex min-h-0 flex-1 flex-col">
				<div
					class="flex h-12 shrink-0 items-center gap-2 border-b border-control-border px-4 sm:px-5"
				>
					<div class="flex rounded-[3px] border border-control-border bg-control p-0.5">
						<Button
							variant="ghost"
							size="sm"
							class={[
								"h-6 rounded-[6px] px-3 text-[11.5px]",
								editorView === "edit" && "bg-background text-foreground",
							]}
							onclick={() => (controller.editorView = "edit")}
						>
							{runtime.i18n.t("reference:edit")}
						</Button>
						<Button
							variant="ghost"
							size="sm"
							class={[
								"font-mono h-6 rounded-[6px] px-3 text-[10.5px]",
								editorView === "api" && "bg-background text-foreground",
							]}
							onclick={() => (controller.editorView = "api")}
						>
							{runtime.i18n.t("reference:api")}
						</Button>
					</div>
					<span class="flex-1"></span>
					<span class="font-mono text-[9.5px] text-foreground-faint">
						{form.submitting
							? runtime.i18n.t("reference:saving")
							: controller.editorDirty
								? runtime.i18n.t("reference:unsavedChanges")
								: runtime.i18n.t("reference:saved")}
					</span>
					{#if !readOnly && editorView === "edit"}
						<Button size="sm" disabled={!canSave} onclick={handleSave}>
							{creating
								? runtime.i18n.t("reference:createLabel", { label: lowerSingularLabel })
								: runtime.i18n.t("reference:saveLabel", { label: lowerSingularLabel })}
						</Button>
					{/if}
				</div>

				<div class="min-h-0 flex-1 overflow-y-auto">
					{#if editorLoading}
						<div class="mx-auto grid max-w-[600px] gap-5 px-6 py-8">
							<Skeleton class="h-10 w-2/3" />
							<Skeleton class="mt-5 h-10" />
							<Skeleton class="h-28" />
						</div>
					{:else if editorView === "api"}
						<div class="mx-auto max-w-[650px] px-6 py-8">
							<p class="font-mono text-[9.5px] tracking-[0.14em] text-foreground-faint uppercase">
								{runtime.i18n.t("reference:currentPayload")}
							</p>
							<JsonViewer class="mt-4" value={form.values} />
						</div>
					{:else}
						<form class="mx-auto max-w-[620px] px-6 py-8" onsubmit={handleSave}>
							<fieldset class="contents" disabled={form.submitting}>
								{#if readOnly}
									<Banner class="mb-6" tone="warning">
										{runtime.i18n.t("reference:readOnlyDescription")}
									</Banner>
								{/if}
								{#if editorError !== undefined}
									<Banner class="mb-6" tone="destructive">{editorError}</Banner>
								{/if}
								{#if !readOnly && form.access !== undefined && !form.access.operations[creating ? "create" : "update"]}
									<Banner class="mb-6" tone="warning">
										{runtime.i18n.t(
											creating ? "reference:cannotCreate" : "reference:cannotUpdate",
											{
												label: lowerSingularLabel,
											}
										)}
									</Banner>
								{/if}

								{#if collection.capabilities.upload && editorDocument !== undefined}
									<div
										class="mb-7 overflow-hidden rounded-[4px] border border-control-border bg-control"
									>
										<div class="grid min-h-48 place-items-center bg-background-layer">
											{#if isImage(editorDocument) && mediaURL(editorDocument) !== undefined}
												<img
													class="max-h-72 w-full object-contain"
													src={mediaURL(editorDocument)}
													alt={typeof editorDocument.alt === "string" ? editorDocument.alt : ""}
												/>
											{:else}
												<FileIcon class="size-9 text-foreground-faint" />
											{/if}
										</div>
										<dl
											class="grid grid-cols-[90px_1fr] gap-x-4 gap-y-2 border-t border-control-border px-4 py-3 text-[11.5px]"
										>
											<dt class="text-foreground-faint">{runtime.i18n.t("uploads:filename")}</dt>
											<dd class="min-w-0 truncate text-foreground-muted">
												{String(editorDocument.filename ?? "—")}
											</dd>
											<dt class="text-foreground-faint">{runtime.i18n.t("uploads:type")}</dt>
											<dd class="font-mono text-[10.5px] text-foreground-muted">
												{String(editorDocument.mimeType ?? "—")}
											</dd>
											<dt class="text-foreground-faint">{runtime.i18n.t("uploads:size")}</dt>
											<dd class="font-mono text-[10.5px] text-foreground-muted">
												{formatBytes(editorDocument.filesize) || "—"}
											</dd>
											<dt class="text-foreground-faint">
												{runtime.i18n.t("uploads:dimensions")}
											</dt>
											<dd class="font-mono text-[10.5px] text-foreground-muted">
												{typeof editorDocument.width === "number" &&
												typeof editorDocument.height === "number"
													? `${runtime.i18n.formatNumber(editorDocument.width)} × ${runtime.i18n.formatNumber(editorDocument.height)}`
													: "—"}
											</dd>
										</dl>
									</div>
								{/if}

								{#if creating && collection.capabilities.upload}
									<label
										class="mb-7 grid min-h-44 cursor-pointer place-items-center rounded-[4px] border border-dashed border-control-border-hover bg-control px-6 py-8 text-center transition-colors hover:border-primary/45 hover:bg-primary/[0.025]"
									>
										<input
											class="sr-only"
											type="file"
											accept={collection.uploadSettings?.mimeTypes.join(",")}
											required
											disabled={form.submitting || form.access?.operations.create !== true}
											bind:files={controller.selectedFiles}
										/>
										<span>
											<FileUpIcon class="mx-auto size-6 text-foreground-faint" />
											<span class="mt-3 block text-[13.5px] font-medium text-foreground-muted">
												{selectedFile?.name ?? runtime.i18n.t("uploads:chooseFileToUpload")}
											</span>
											<span class="font-mono mt-1.5 block text-[9.5px] text-foreground-faint">
												{runtime.i18n.formatList(collection.uploadSettings?.mimeTypes ?? [], {
													style: "short",
													type: "disjunction",
												})}
											</span>
										</span>
									</label>
								{/if}

								{#if controller.creatingAuthUser}
									<fieldset
										class="mb-7 grid gap-4 rounded-[4px] border border-control-border bg-control p-4.5"
									>
										<legend class="px-1 text-[13px] font-semibold text-foreground">
											{runtime.i18n.t("reference:credentials")}
										</legend>
										<div class="grid gap-4 sm:grid-cols-2">
											<label class="grid gap-2" for="ridu-reference-new-user-password">
												<span class="ridu-field-label">{runtime.i18n.t("reference:password")}</span>
												<input
													id="ridu-reference-new-user-password"
													type="password"
													autocomplete="new-password"
													required
													disabled={form.submitting || form.access?.operations.create !== true}
													aria-invalid={controller.credentialIssue !== undefined}
													aria-describedby="ridu-reference-new-user-password-help"
													bind:value={controller.newUserPassword}
													oninput={() => (controller.credentialIssue = undefined)}
												/>
											</label>
											<label class="grid gap-2" for="ridu-reference-new-user-password-confirmation">
												<span class="ridu-field-label">
													{runtime.i18n.t("reference:confirmPassword")}
												</span>
												<input
													id="ridu-reference-new-user-password-confirmation"
													type="password"
													autocomplete="new-password"
													required
													disabled={form.submitting || form.access?.operations.create !== true}
													aria-invalid={controller.credentialIssue !== undefined}
													aria-describedby="ridu-reference-new-user-password-help"
													bind:value={controller.newUserPasswordConfirmation}
													oninput={() => (controller.credentialIssue = undefined)}
												/>
											</label>
										</div>
										<p
											id="ridu-reference-new-user-password-help"
											class={controller.credentialIssue === undefined
												? "ridu-field-help"
												: "ridu-field-error"}
											role={controller.credentialIssue === undefined ? undefined : "alert"}
										>
											{controller.credentialIssue ??
												runtime.i18n.t("reference:passwordMinimum", {
													minimum: runtime.i18n.formatNumber(
														collection.authSettings?.passwordMinLength ?? 8
													),
												})}
										</p>
									</fieldset>
								{/if}

								<p class="font-mono text-[9.5px] tracking-[0.13em] text-foreground-faint uppercase">
									{creating
										? runtime.i18n.t("reference:newMetadata", { label: singularLabel })
										: `${singularLabel} · ${editorDocument?.id ?? ""}`}
								</p>
								{#if headingField !== undefined}
									<div data-field-path={headingField.path}>
										<label class="sr-only" for={headingField.id}>
											{runtime.i18n.text(
												headingField.admin.label,
												headingField.admin.labelTranslations
											)}
										</label>
										<FieldMessages
											controlID={headingField.id}
											issues={headingIssues}
											description={headingField.admin.description}
											class="mt-3"
										>
											<input
												id={headingField.id}
												class="font-serif w-full border-x-0 border-t-0 border-b border-b-transparent bg-transparent p-0 pb-1 text-[34px] leading-[1.08] text-foreground caret-primary outline-none placeholder:text-foreground-soft aria-invalid:!border-b-destructive/65"
												value={String(form.get(headingField.path) ?? "")}
												placeholder={runtime.i18n.t("reference:untitledLabel", {
													label: lowerSingularLabel,
												})}
												required={headingField.required &&
													(!headingField.dynamicDefault ||
														form.get(headingField.path) !== undefined)}
												readonly={readOnly || !form.canWrite(headingField.path)}
												{...fieldControlARIA(
													headingField.id,
													headingField.admin.description !== undefined,
													headingIssues.length > 0
												)}
												oninput={(event) => form.set(headingField.path, event.currentTarget.value)}
												onblur={() => form.liveValidation.flush(headingField.path)}
											/>
										</FieldMessages>
										<LiveValidationFeedback
											feedback={form.liveValidation.forField(headingField.path)}
											i18n={runtime.i18n}
											path={headingField.path}
										/>
									</div>
								{/if}

								<div class="mt-7">
									<FieldLayout
										fields={editorFields.map((candidate) =>
											readOnly
												? { ...candidate, admin: { ...candidate.admin, readOnly: true } }
												: candidate
										)}
										{form}
									/>
								</div>

								{#if !readOnly}
									<div class="mt-8 flex justify-end">
										<Button type="submit" disabled={!canSave}>
											{creating
												? runtime.i18n.t("reference:createLabel", { label: lowerSingularLabel })
												: runtime.i18n.t("reference:saveLabel", { label: lowerSingularLabel })}
										</Button>
									</div>
								{/if}
							</fieldset>
						</form>
					{/if}
				</div>
			</div>
		{/if}
	</SheetContent>
</Sheet>

<Dialog bind:open={controller.confirmDiscard}>
	<DialogContent showCloseButton={false}>
		<DialogHeader>
			<DialogTitle>{runtime.i18n.t("reference:discardChanges")}</DialogTitle>
			<DialogDescription>
				{runtime.i18n.t("reference:discardDescription")}
			</DialogDescription>
		</DialogHeader>
		<DialogFooter>
			<Button
				variant="outline"
				disabled={form.submitting}
				onclick={() => (controller.confirmDiscard = false)}
			>
				{runtime.i18n.t("reference:keepEditing")}
			</Button>
			<Button variant="destructive" disabled={form.submitting} onclick={discardChanges}>
				{runtime.i18n.t("reference:discard")}
			</Button>
		</DialogFooter>
	</DialogContent>
</Dialog>
