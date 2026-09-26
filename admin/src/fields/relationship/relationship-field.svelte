<script lang="ts">
	import { isSortable } from "@dnd-kit-svelte/svelte/sortable";
	import type { FieldAuthoringHost } from "@riducms/plugin";
	import type { SchemaField } from "@riducms/protocol";
	import { fieldControlARIA, Button } from "@riducms/ui";
	import { DragDropProvider, type DragDropEvents } from "@dnd-kit-svelte/svelte";
	import RelationshipSelection from "@admin/fields/relationship/relationship-selection.svelte";
	import {
		parseRelationshipKey,
		relationshipKey,
	} from "@admin/fields/relationship/relationship-value";
	import "@admin/fields/relationship/relationship.scss";

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
		hasMany,
		hydrationError,
		initialDocument,
		initialDocumentID,
		initialCreate,
		initialFile,
		selections,
		issues,
		selectedID,
		selectedIDs,
		selectedTarget,
		targetCollection,
		targets,
	} = $derived(controller);
	const { commit, label, openBrowser, selectTarget, setBrowserOpen } = controller;
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
	let dragging = $state(false);
	const canCreate = $derived(
		!editingBlocked &&
			targetCollection !== undefined &&
			runtime.collectionOperations[targetCollection.slug]?.create === true
	);

	function reorder(event: Parameters<DragDropEvents["dragend"]>[0]) {
		const source = event.operation.source;
		if (event.canceled || editingBlocked || !source || !isSortable(source)) return;
		controller.move(source.sortable.initialIndex, source.sortable.index);
	}

	function setSelectedValues(keys: string[]) {
		if (editingBlocked) return;
		controller.setReferences(
			keys.flatMap((key) => {
				const reference = parseRelationshipKey(key);
				return reference !== undefined &&
					targets.some((candidate) => candidate.collectionSlug === reference.relationTo)
					? [reference]
					: [];
			})
		);
	}

	function drop(event: DragEvent) {
		event.preventDefault();
		dragging = false;
		const file = event.dataTransfer?.files[0];
		if (canCreate && file) openBrowser(undefined, true, selectedTarget, file);
	}
</script>

{#snippet selectedItems(upload = false)}
	{#each selections as selection, index (selection.key)}
		<RelationshipSelection
			id={selection.key}
			{index}
			label={selection.label}
			document={selection.document}
			{upload}
			sortable={hasMany}
			readOnly={field.admin.readOnly}
			blocked={form.editingBlocked}
			onEdit={() => openBrowser(selection.id, false, selection.relationTo)}
			onRemove={() => controller.removeReference(selection)}
		/>
	{/each}
{/snippet}

<FieldShell {field} {issues}>
	{#if targetCollection === undefined}
		<p class="ridu-field-error" role="alert">
			{runtime.i18n.t("errors:relationshipTargetUnavailable")}
		</p>
	{:else}
		<DragDropProvider onDragEnd={reorder}>
			{#if field.upload !== undefined}
				<div class="ridu-upload-references" id={field.id} {...controlARIA}>
					{@render selectedItems(true)}
					{#if !field.admin.readOnly && (hasMany || selectedID === undefined)}
						<!-- File drops supplement the keyboard-accessible create and browse actions. -->
						<!-- svelte-ignore a11y_no_static_element_interactions -->
						<div
							class="ridu-upload-intake"
							role="region"
							aria-label={field.admin.label}
							data-dragging={dragging}
							ondragover={(event) => {
								event.preventDefault();
								if (canCreate) dragging = true;
							}}
							ondragleave={(event) => {
								if (!event.currentTarget.contains(event.relatedTarget as Node | null))
									dragging = false;
							}}
							ondrop={drop}
						>
							{#if canCreate}
								<Button variant="secondary" size="sm" onclick={() => openBrowser(undefined, true)}>
									{runtime.i18n.t("collections:createNewButton")}
								</Button>
								<span>{runtime.i18n.t("general:or")}</span>
							{/if}
							<Button
								variant="secondary"
								size="sm"
								disabled={editingBlocked}
								onclick={() => openBrowser()}
							>
								{runtime.i18n.t("fields:chooseExisting")}
							</Button>
							{#if canCreate}
								<span class="ridu-upload-intake__drop-label">
									{runtime.i18n.t("uploads:dropFile")}
								</span>
							{/if}
						</div>
					{/if}
				</div>
			{:else}
				<RelationshipQuickPicker
					id={field.id}
					label={field.admin.label}
					targets={quickPickerTargets}
					{selectedTarget}
					selectedID={hasMany ? undefined : selectedID}
					selectedLabel={selectedID === undefined ? undefined : label(selectedID)}
					placeholder={field.admin.placeholder}
					readOnly={field.admin.readOnly}
					blocked={form.editingBlocked}
					locale={form.contentLocale}
					invalid={issues.length > 0}
					hasDescription={field.admin.description !== undefined}
					multiple={hasMany}
					values={controller.references.map(relationshipKey)}
					onValuesChange={setSelectedValues}
					onPick={pickRelationship}
					onRemove={() => {
						if (!editingBlocked) commit([]);
					}}
					onBrowse={browseRelationships}
					onCreate={(target) => openBrowser(undefined, true, target)}
					onEdit={selectedID === undefined ? undefined : () => openBrowser(selectedID)}
				>
					{#snippet selections()}
						{#if hasMany}
							{@render selectedItems()}
						{/if}
					{/snippet}
				</RelationshipQuickPicker>
			{/if}
		</DragDropProvider>
	{/if}
	{#if hydrationError !== undefined}
		<p class="ridu-field-error" role="alert">
			{hydrationError}
		</p>
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
		{initialCreate}
		{initialFile}
		{optionFilter}
		locale={form.contentLocale}
		onCommit={(ids) => {
			if (!editingBlocked) commit(ids);
		}}
		onClose={() => setBrowserOpen(false)}
	/>
{/if}
