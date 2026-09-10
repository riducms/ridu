<script lang="ts">
	import type { FieldAuthoringHost } from "@riducms/plugin";
	import type { SchemaField } from "@riducms/protocol";
	import { fieldControlARIA } from "@riducms/ui";
	import FileIcon from "~icons/lucide/file";
	import PencilIcon from "~icons/lucide/pencil";
	import PlusIcon from "~icons/lucide/plus";
	import SearchIcon from "~icons/lucide/search";
	import XIcon from "~icons/lucide/x";

	import { Button } from "@admin/components/ui/button";
	import type { FormController } from "@admin/core/forms/form-controller.svelte";
	import { localizeSchemaCollection } from "@admin/core/i18n/localized-schema";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import FieldShell from "@admin/fields/field-shell.svelte";

	import { RelationshipFieldController } from "@admin/fields/relationship/relationship-field-controller.svelte";
	import RelationshipQuickPicker from "@admin/fields/relationship/relationship-quick-picker.svelte";
	import { relationshipOptionFilters } from "@admin/fields/relationship/relationship-query";

	let {
		field,
		form,
		authoring,
	}: { field: SchemaField; form: FormController; authoring?: FieldAuthoringHost } = $props();
	const runtime = getAdminRuntime();
	const ReferenceBrowser = $derived(authoring?.referenceBrowser);
	const controller = new RelationshipFieldController({
		runtime,
		get field() {
			return field;
		},
		get form() {
			return form;
		},
	});
	const {
		browserOpen,
		documents,
		hasMany,
		hydrationError,
		initialDocument,
		initialDocumentID,
		issues,
		polymorphic,
		selectedID,
		selectedIDs,
		selectedTarget,
		targetCollection,
		targets,
	} = $derived(controller);
	const {
		commit,
		initials,
		isImage,
		label,
		mediaURL,
		openBrowser,
		remove,
		selectTarget,
		setBrowserOpen,
	} = controller;
	const controlARIA = $derived(
		fieldControlARIA(field.id, field.admin.description !== undefined, issues.length > 0)
	);
	const editingBlocked = $derived(field.admin.readOnly === true || form.editingBlocked);
	const optionFilter = $derived(
		relationshipOptionFilters(field, (path) => form.get(path), targetCollection?.slug)
	);
	const quickPickerTargets = $derived(
		targets.flatMap((target) => {
			const collection = runtime.manifest?.collections.find(
				(candidate) => candidate.slug === target.collectionSlug
			);
			return collection === undefined
				? []
				: [
						{
							collection: localizeSchemaCollection(collection, runtime.i18n),
							filter: relationshipOptionFilters(field, (path) => form.get(path), collection.slug),
						},
					];
		})
	);

	function pickRelationship(target: string, id: string) {
		if (editingBlocked) return;
		selectTarget(target);
		commit([id]);
	}

	function browseRelationships(target: string) {
		if (form.editingBlocked) return;
		selectTarget(target);
		openBrowser();
	}
</script>

<FieldShell {field} {issues}>
	{#if targetCollection === undefined}
		<div
			class="rounded-[4px] border border-destructive/25 bg-destructive/7 p-3 text-[12.5px] text-destructive"
			role="alert"
		>
			{runtime.i18n.t("errors:relationshipTargetUnavailable")}
		</div>
	{:else if field.upload !== undefined && !hasMany && selectedID !== undefined}
		{const document = documents[selectedID]}
		<div
			class="flex min-h-17 items-center gap-2.5 rounded-[4px] border border-control-border bg-control p-2 aria-invalid:!border-destructive/65"
			aria-invalid={issues.length > 0}
		>
			<button
				id={field.id}
				type="button"
				disabled={form.editingBlocked}
				class="group flex min-w-0 flex-1 items-center gap-2.5 rounded-[3px] text-start outline-none focus-visible:ring-2 focus-visible:ring-ring/70"
				onclick={() => openBrowser(selectedID)}
				aria-label={runtime.i18n.t("fields:edit", { label: label(selectedID) })}
				{...controlARIA}
			>
				<span
					class="grid size-14 shrink-0 place-items-center overflow-hidden rounded-[3px] border border-control-border bg-background"
				>
					{#if isImage(document) && mediaURL(document) !== undefined}
						<img
							class="size-full object-cover"
							src={mediaURL(document)}
							alt={typeof document?.alt === "string" ? document.alt : ""}
						/>
					{:else}
						<FileIcon class="size-5 text-foreground-faint" />
					{/if}
				</span>
				<span class="min-w-0 flex-1">
					<span class="block truncate text-[13px] text-foreground-strong group-hover:text-primary">
						{label(selectedID)}
					</span>
					<span class="font-mono mt-0.5 block truncate text-[9.5px] text-foreground-faint">
						{selectedID}
					</span>
					<span class="mt-1.5 flex items-center gap-1 text-[11px] text-foreground-sub">
						<PencilIcon class="size-3" />
						{runtime.i18n.t("fields:inspectEdit")}
					</span>
				</span>
			</button>
			{#if !field.admin.readOnly}
				<div class="flex shrink-0 flex-wrap justify-end gap-1">
					<Button
						variant="outline"
						size="xs"
						disabled={form.editingBlocked}
						onclick={() => openBrowser()}
					>
						<SearchIcon class="size-3" />
						{runtime.i18n.t("general:replace")}
					</Button>
					<Button variant="ghost" size="xs" disabled={editingBlocked} onclick={() => commit([])}>
						<XIcon class="size-3" />
						{runtime.i18n.t("general:remove")}
					</Button>
				</div>
			{/if}
		</div>
	{:else}
		{#if !hasMany}
			<RelationshipQuickPicker
				id={field.id}
				label={field.admin.label}
				targets={quickPickerTargets}
				{selectedTarget}
				{selectedID}
				selectedLabel={selectedID === undefined ? undefined : label(selectedID)}
				selectedInitials={selectedID === undefined ? undefined : initials(selectedID)}
				placeholder={field.admin.placeholder}
				readOnly={field.admin.readOnly}
				blocked={form.editingBlocked}
				locale={form.contentLocale}
				upload={field.upload !== undefined}
				invalid={issues.length > 0}
				hasDescription={field.admin.description !== undefined}
				onPick={pickRelationship}
				onRemove={() => {
					if (!editingBlocked) commit([]);
				}}
				onBrowse={browseRelationships}
				onEdit={selectedID === undefined ? undefined : () => openBrowser(selectedID)}
			/>
		{:else}
			<div
				class="flex min-h-10 min-w-0 rounded-[3px] border border-control-border bg-control transition-colors hover:border-control-border-hover aria-invalid:!border-destructive/65"
				aria-invalid={issues.length > 0}
			>
				{#if selectedIDs.length === 0}
					<button
						id={field.id}
						type="button"
						disabled={form.editingBlocked}
						class="flex min-h-10 min-w-0 flex-1 items-center px-3 text-start text-[13px] text-foreground-placeholder outline-none hover:text-foreground focus-visible:text-primary"
						onclick={() => openBrowser()}
						{...controlARIA}
					>
						<span class="px-1.5 text-[13px] text-foreground-placeholder">
							{field.admin.placeholder ??
								runtime.i18n.t("fields:select", {
									label: targetCollection.labels.singular.toLocaleLowerCase(runtime.i18n.language),
								})}
						</span>
					</button>
				{:else}
					<div class="flex min-w-0 flex-1 flex-wrap items-center gap-1.5 p-1.5">
						{#each selectedIDs as id (id)}
							<div
								class="inline-flex max-w-full items-center gap-1 rounded-[3px] border border-control-border bg-background py-0.5 pe-0.5 ps-2 text-[12px] text-foreground-muted"
							>
								<button
									type="button"
									disabled={form.editingBlocked}
									class="min-w-0 flex-1 truncate text-start outline-none hover:text-foreground focus-visible:text-primary"
									onclick={() => openBrowser(id)}
									aria-label={runtime.i18n.t("fields:edit", { label: label(id) })}
								>
									{label(id)}
								</button>
								{#if !field.admin.readOnly}
									<Button
										variant="ghost"
										size="icon-xs"
										disabled={editingBlocked}
										onclick={() => remove(id)}
										aria-label={runtime.i18n.t("fields:remove", { label: label(id) })}
									>
										<XIcon class="size-3" />
									</Button>
								{/if}
							</div>
						{/each}
					</div>
				{/if}
				<Button
					variant="ghost"
					size="icon"
					class="h-auto min-h-10 w-10 rounded-s-none border-s border-control-border"
					disabled={form.editingBlocked}
					onclick={() => openBrowser()}
					{...controlARIA}
					aria-label={runtime.i18n.t("fields:browse", {
						label: targetCollection.labels.plural.toLocaleLowerCase(runtime.i18n.language),
					})}
				>
					{#if selectedIDs.length === 0}<PlusIcon class="size-3.5" />{:else}<SearchIcon
							class="size-3.5"
						/>{/if}
				</Button>
			</div>
		{/if}
	{/if}

	{#if hydrationError !== undefined}
		<p class="ridu-field-error" role="alert">{hydrationError}</p>
	{/if}
</FieldShell>

{#if browserOpen && targetCollection !== undefined && ReferenceBrowser !== undefined}
	<ReferenceBrowser
		bind:open={() => browserOpen, setBrowserOpen}
		{field}
		collection={targetCollection}
		{hasMany}
		{selectedIDs}
		readOnly={field.admin.readOnly}
		{initialDocument}
		{initialDocumentID}
		{optionFilter}
		locale={form.contentLocale}
		onCommit={(ids) => {
			if (!editingBlocked) commit(ids);
		}}
		onClose={() => setBrowserOpen(false)}
	/>
{/if}
