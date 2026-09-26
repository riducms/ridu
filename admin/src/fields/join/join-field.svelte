<script lang="ts">
	import type { FieldAuthoringHost } from "@riducms/plugin";
	import type { SchemaField } from "@riducms/protocol";
	import ChevronDownIcon from "~icons/lucide/chevron-down";
	import ListPlusIcon from "~icons/lucide/list-plus";
	import PencilIcon from "~icons/lucide/pencil";
	import { FieldFeedback } from "@riducms/ui";
	import { CollapsiblePanel } from "@admin/components/ui/collapsible-panel";
	import type { FormController } from "@admin/core/forms/form-controller.svelte";
	import { getNotificationCenter } from "@admin/core/notifications/notification-center.svelte";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import ColumnPicker from "@admin/features/collections/controls/column-picker.svelte";
	import SortHeading from "@admin/features/collections/controls/sort-heading.svelte";
	import { documentLabel } from "@admin/features/documents/document-title";
	import { JoinFieldController } from "@admin/fields/join/join-field-controller.svelte";
	import "@admin/fields/join/join-field.scss";

	let {
		field,
		form,
		authoring,
	}: {
		field: SchemaField;
		form: FormController;
		authoring?: FieldAuthoringHost;
	} = $props();

	const runtime = getAdminRuntime();
	const controller = new JoinFieldController({
		runtime,
		notifications: getNotificationCenter(),
		get field() {
			return field;
		},
		get form() {
			return form;
		},
		get authoring() {
			return authoring;
		},
	});
	const ReferenceBrowser = $derived(authoring?.referenceBrowser);
	const {
		targetCollection,
		sourceID,
		canCreate,
		canManage,
		canOpenDocument,
		editingBlocked,
		pending,
		mutationError,
		documents,
		sortedDocuments,
		availableColumns,
		columnSelection,
		visibleColumns,
		sort,
		browserOpen,
		browserMode,
		browserDocumentID,
	} = $derived(controller);

	let columnsOpen = $state(false);
</script>

<div class="ridu-field ridu-join" data-field-path={field.path} data-join-field={field.name}>
	<div class="ridu-field-heading ridu-join__heading">
		<label id={`${field.id}-label`} class="ridu-field-label" for={field.id}>
			{field.admin.label}
		</label>
		{#if field.required}
			<span class="ridu-field-required" aria-hidden="true"></span>
		{/if}
		{#if field.admin.readOnly}
			<span class="ridu-field-status">Read only</span>
		{/if}
		<div class="ridu-join__actions">
			{#if canCreate}
				<button
					class="ridu-join__add"
					type="button"
					disabled={pending || editingBlocked}
					onclick={() => controller.openBrowser("create")}
				>
					{runtime.i18n.t("fields:addNew")}
				</button>
			{/if}
			{#if canManage}
				<button
					class="ridu-join__pill ridu-join__manage"
					type="button"
					disabled={pending || editingBlocked}
					onclick={() => controller.openBrowser("manage")}
					aria-label={runtime.i18n.t(pending ? "fields:updating" : "fields:manageRelationships")}
					title={runtime.i18n.t("fields:manageRelationships")}
				>
					<ListPlusIcon aria-hidden="true" />
				</button>
			{/if}
			<button
				class="ridu-join__pill"
				type="button"
				aria-expanded={columnsOpen}
				aria-controls={`${field.id}-columns`}
				onclick={() => (columnsOpen = !columnsOpen)}
			>
				{runtime.i18n.t("collections:columns")}
				<ChevronDownIcon
					class={["ridu-join__chevron", columnsOpen && "ridu-join__chevron--open"]}
					aria-hidden="true"
				/>
			</button>
		</div>
	</div>
	<FieldFeedback
		controlID={field.id}
		description={field.admin.description}
		errors={mutationError === undefined ? [] : [mutationError]}
	>
		<CollapsiblePanel id={`${field.id}-columns`} open={columnsOpen}>
			<ColumnPicker
				columns={columnSelection}
				{availableColumns}
				canReadField={() => true}
				onChange={controller.setColumns}
			/>
		</CollapsiblePanel>
		<div class="ridu-join__table-wrap" aria-busy={pending}>
			{#if sortedDocuments.length > 0 && visibleColumns.length > 0}
				<div class="ridu-join__scroll">
					<table class="ridu-join__table" aria-labelledby={`${field.id}-label`}>
						<thead>
							<tr>
								{#each visibleColumns as column (column.path)}
									<SortHeading {column} {sort} onSort={controller.setSort} compact />
								{/each}
							</tr>
						</thead>
						<tbody>
							{#each sortedDocuments as document (document.id)}
								<tr>
									{#each visibleColumns as column, index (column.path)}
										<td>
											{#if index === 0 && canOpenDocument}
												<button
													class="ridu-join__document"
													type="button"
													onclick={() => controller.openBrowser("edit", document.id)}
													aria-label={runtime.i18n.t("fields:open", {
														label: documentLabel(targetCollection, document),
													})}
												>
													<span>{controller.cellText(document, column)}</span>
													<PencilIcon aria-hidden="true" />
												</button>
											{:else}
												<span class="ridu-join__cell">{controller.cellText(document, column)}</span>
											{/if}
										</td>
									{/each}
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			{:else if sortedDocuments.length === 0}
				<p class="ridu-join__empty">
					{runtime.i18n.t("fields:noRelated", {
						label: (targetCollection?.labels.plural ?? field.admin.label).toLocaleLowerCase(
							runtime.i18n.language
						),
					})}
				</p>
			{:else}
				<p class="ridu-join__empty">{runtime.i18n.t("collections:columns")}</p>
			{/if}
		</div>
	</FieldFeedback>
</div>

{#if browserOpen && targetCollection !== undefined && ReferenceBrowser !== undefined && sourceID !== undefined}
	<ReferenceBrowser
		bind:open={() => browserOpen, controller.setBrowserOpen}
		{field}
		collection={targetCollection}
		hasMany={true}
		selectedIDs={documents.map((document) => document.id)}
		readOnly={controller.browserReadOnly}
		defaultValues={controller.defaultValues}
		allowCreate={canCreate}
		initialCreate={browserMode === "create"}
		initialDocumentID={browserMode === "edit" ? browserDocumentID : undefined}
		locale={form.contentLocale}
		onCommit={controller.commit}
		onClose={controller.browserClosed}
	/>
{/if}
