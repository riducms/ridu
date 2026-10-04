<script lang="ts">
	import { isSortable } from "@dnd-kit-svelte/svelte/sortable";
	import { resolveBlockTypes } from "@riducms/protocol";

	import type { SchemaField } from "@riducms/protocol";
	import { fieldControlARIA, Button, buttonVariants } from "@riducms/ui";
	import { DragDropProvider, type DragDropEvents } from "@dnd-kit-svelte/svelte";
	import ChevronDownIcon from "~icons/lucide/chevron-down";
	import CirclePlusIcon from "~icons/lucide/circle-plus";
	import EllipsisIcon from "~icons/lucide/ellipsis";
	import GripVerticalIcon from "~icons/lucide/grip-vertical";

	import BlockPicker from "@admin/fields/nested/block-picker.svelte";
	import {
		DropdownMenu,
		DropdownMenuContent,
		DropdownMenuItem,
		DropdownMenuSeparator,
		DropdownMenuTrigger,
	} from "@admin/components/ui/dropdown-menu";

	import type { FormController } from "@admin/core/forms/form-controller.svelte";
	import { FieldBindingLifetime } from "@admin/core/forms/field-binding-lifetime";
	import { initialFormValues } from "@admin/core/forms/form-schema";
	import { immutableRowLabelSnapshot } from "@admin/core/plugins/row-label-registry";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import { focusFieldIssue, fieldIssueRevealEvent } from "@admin/core/forms/field-issue-focus";
	import FieldLayout from "@admin/fields/field-layout.svelte";
	import { getFieldLayoutPresentation } from "@admin/fields/field-layout-presentation";
	import FieldMessages from "@admin/fields/field-messages.svelte";
	import {
		compatibleClipboardValue,
		cloneFieldForPaste,
		createFieldClipboardPayload,
		readFieldClipboard,
		writeFieldClipboard,
	} from "@admin/fields/field-clipboard";
	import { scopeRepeatedRowField } from "@admin/fields/nested/scoped-field";
	import "@admin/fields/nested/nested-field.scss";
	import SortableRow from "@admin/fields/nested/sortable-row.svelte";
	import BlockHeader from "@admin/fields/nested/block-header.svelte";
	import { blockHeaderValue, visibleBlockChild } from "@admin/fields/nested/block-header";

	let { field, form }: { field: SchemaField; form: FormController } = $props();
	const layoutPresentation = getFieldLayoutPresentation();
	const runtime = getAdminRuntime();
	const customRowLabel = $derived(runtime.rowLabels.resolve(field));
	const blocks = $derived(resolveBlockTypes(field.blocks));
	const blockTypes = $derived(new Map(blocks.map((block) => [block.slug, block])));
	const rows = $derived((form.get(field.path) as Record<string, unknown>[] | undefined) ?? []);
	const allIssues = $derived(form.issuesFor(field.path));
	const issues = $derived(allIssues.filter((issue) => issue.path === field.path));
	const controlARIA = $derived(
		fieldControlARIA(field.id, field.admin.description !== undefined, issues.length > 0)
	);
	const minRows = $derived(
		(field.type === "blocks" ? field.blocks?.minRows : field.nested?.minRows) ?? 0
	);
	const maxRows = $derived(
		(field.type === "blocks" ? field.blocks?.maxRows : field.nested?.maxRows) ?? 0
	);
	const unknownRows = $derived(
		field.type === "blocks" && rows.some((row) => !blockTypes.has(String(row.blockType)))
	);
	const readOnly = $derived(field.admin.readOnly === true || unknownRows);
	const editingBlocked = $derived(readOnly || form.editingBlocked);
	const canAdd = $derived(!editingBlocked && (maxRows === 0 || rows.length < maxRows));
	// Only the row-error click handler reads this DOM binding.
	// svelte-ignore non_reactive_update
	let repeatedRoot: HTMLElement;
	let collapsed = $state.raw(new Set<string>());
	let clipboardMessage = $state("");
	// Pasted rows get new keys and editors; a pasted group remounts its editors the same way,
	// discarding unsaved editor input even when the pasted value equals the form value.
	let groupPastes = $state(0);
	let blockPickerOpen = $state(false);
	let insertAfter = $state<string>();

	function changeBlockPickerOpen(open: boolean) {
		blockPickerOpen = open;
		if (!open) {
			insertAfter = undefined;
		}
	}

	function rowField(child: SchemaField, index: number, rowKey: string) {
		return scopeRepeatedRowField(child, `${field.path}.${index}`, rowKey, readOnly);
	}

	function inheritedField(child: SchemaField) {
		return readOnly ? { ...child, admin: { ...child.admin, readOnly: true } } : child;
	}

	const inheritedFields = $derived((field.nested?.fields ?? []).map(inheritedField));
	// A clipboard permission prompt may outlive the editor or its current field occurrence.
	const pendingPastes = new Set<FieldBindingLifetime>();
	$effect(() => () => {
		for (const paste of pendingPastes) paste.destroy();
		pendingPastes.clear();
	});

	function beginPaste(owner: FormController) {
		const lifetime = new FieldBindingLifetime(
			owner,
			() => field,
			() => runtime.manifestRevision
		);
		pendingPastes.add(lifetime);
		lifetime.onDestroy(() => pendingPastes.delete(lifetime));
		return lifetime;
	}

	function createRow(blockType?: string) {
		const children =
			field.type === "blocks"
				? (blockTypes.get(blockType ?? "")?.fields ?? [])
				: (field.nested?.fields ?? []);
		return {
			...initialFormValues(children),
			_key: crypto.randomUUID(),
			...(field.type === "blocks" ? { blockType: blockType ?? "" } : {}),
		};
	}

	function add(blockType?: string) {
		if (editingBlocked || !canAdd) return;
		if (field.type === "blocks" && blockType === undefined) {
			blockPickerOpen = true;
			return;
		}
		const next = [...rows];
		const index =
			insertAfter === undefined
				? rows.length
				: rows.findIndex((row) => row._key === insertAfter) + 1;
		next.splice(index, 0, createRow(blockType));
		updateRows(next);
		insertAfter = undefined;
		blockPickerOpen = false;
	}

	function addBelow(index: number) {
		if (editingBlocked || !canAdd) return;
		if (field.type === "blocks") {
			insertAfter = String(rows[index]?._key);
			blockPickerOpen = true;
			return;
		}
		const next = [...rows];
		next.splice(index + 1, 0, createRow());
		updateRows(next);
	}

	async function copyField() {
		await writeFieldClipboard(createFieldClipboardPayload(field, "field", form.get(field.path)));
		clipboardMessage = runtime.i18n.t("fields:copied", { label: field.admin.label });
	}

	async function pasteField() {
		if (editingBlocked) return;
		const owner = form;
		const lifetime = beginPaste(owner);
		try {
			const payload = await readFieldClipboard();
			if (form !== owner || lifetime.readOnly || editingBlocked) return;
			const target = lifetime.schema;
			const value = compatibleClipboardValue(payload, target, "field");
			if (value === undefined) {
				clipboardMessage = runtime.i18n.t("errors:clipboardField");
				return;
			}
			if ((target.type === "array" || target.type === "blocks") && !Array.isArray(value)) {
				clipboardMessage = runtime.i18n.t("errors:clipboardRows");
				return;
			}
			if (
				target.type === "group" &&
				(value === null || typeof value !== "object" || Array.isArray(value))
			) {
				clipboardMessage = runtime.i18n.t("errors:clipboardGroup");
				return;
			}
			const minimum =
				(target.type === "blocks" ? target.blocks?.minRows : target.nested?.minRows) ?? 0;
			const maximum =
				(target.type === "blocks" ? target.blocks?.maxRows : target.nested?.maxRows) ?? 0;
			if (Array.isArray(value) && value.length < minimum) {
				clipboardMessage = runtime.i18n.t("errors:clipboardMinRows", { count: minimum });
				return;
			}
			if (Array.isArray(value) && maximum > 0 && value.length > maximum) {
				clipboardMessage = runtime.i18n.t("errors:clipboardMaxRows", { count: maximum });
				return;
			}
			const pasted = Array.isArray(value)
				? value.map((row) =>
						row !== null && typeof row === "object" && !Array.isArray(row)
							? { ...row, _key: crypto.randomUUID() }
							: row
					)
				: value;
			if (Array.isArray(pasted)) owner.setRows(target.path, pasted as Record<string, unknown>[]);
			else {
				owner.set(target.path, pasted);
				groupPastes++;
			}
			clipboardMessage = runtime.i18n.t("fields:pasted", { label: target.admin.label });
		} finally {
			pendingPastes.delete(lifetime);
			lifetime.destroy();
		}
	}

	async function copyRow(index: number) {
		await writeFieldClipboard(createFieldClipboardPayload(field, "row", rows[index]));
		clipboardMessage = runtime.i18n.t("fields:copied", {
			label: rowLabel(rows[index] ?? {}, index),
		});
	}

	async function pasteRow(index: number) {
		if (editingBlocked || !canAdd) return;
		const anchor = rows[index];
		if (anchor === undefined) return;
		const owner = form;
		const anchorKey = anchor._key;
		const anchorVariant = anchor.blockType;
		// Removing a row revokes this paste even if another row later reuses its serialized key.
		const anchorMount = owner.rowMountKey(anchor);
		const lifetime = beginPaste(owner);
		try {
			const payload = await readFieldClipboard();
			if (form !== owner || lifetime.readOnly || editingBlocked) return;
			const target = lifetime.schema;
			const currentRows = (owner.get(target.path) as Record<string, unknown>[] | undefined) ?? [];
			const currentIndex = currentRows.findIndex(
				(row) =>
					row._key === anchorKey &&
					row.blockType === anchorVariant &&
					owner.rowMountKey(row) === anchorMount
			);
			const maximum =
				(target.type === "blocks" ? target.blocks?.maxRows : target.nested?.maxRows) ?? 0;
			if (currentIndex < 0 || (maximum > 0 && currentRows.length >= maximum)) return;
			const value = compatibleClipboardValue(payload, target, "row");
			if (value === null || typeof value !== "object" || Array.isArray(value)) {
				clipboardMessage = runtime.i18n.t("errors:clipboardRow");
				return;
			}
			const row = value as Record<string, unknown>;
			row._key = crypto.randomUUID();
			if (
				target.type === "blocks" &&
				!resolveBlockTypes(target.blocks).some((block) => block.slug === row.blockType)
			) {
				clipboardMessage = runtime.i18n.t("errors:clipboardBlockType");
				return;
			}
			const next = [...currentRows];
			next.splice(currentIndex + 1, 0, row);
			owner.setRows(target.path, next);
			clipboardMessage = runtime.i18n.t("fields:pasted", {
				label: rowLabel(row, currentIndex + 1),
			});
		} finally {
			pendingPastes.delete(lifetime);
			lifetime.destroy();
		}
	}

	function updateRows(next: Record<string, unknown>[]) {
		if (editingBlocked) return;
		form.setRows(field.path, next);
	}

	function duplicate(index: number) {
		if (editingBlocked || !canAdd) return;
		const copy = cloneFieldForPaste(field, rows[index], "row") as Record<string, unknown>;
		const next = [...rows];
		next.splice(index + 1, 0, copy);
		updateRows(next);
	}

	function errorCount(count: number) {
		return runtime.i18n.t(count === 1 ? "fields:rowError" : "fields:rowErrors", { count });
	}

	function rowLabel(row: Record<string, unknown>, index: number) {
		const block = blockTypes.get(String(row.blockType));
		if (field.type === "blocks" && block !== undefined) {
			const path = `${field.path}.${index}`;
			return (
				blockHeaderValue(form, block, path, "blockName") ||
				blockHeaderValue(form, block, path, block.admin?.rowLabel) ||
				runtime.i18n.t("fields:untitled", { label: block.labels.singular })
			);
		}
		const key = field.type === "blocks" ? block?.admin?.rowLabel : field.nested?.rowLabel;
		const configured = key === undefined ? undefined : row[key];
		if (configured !== undefined && configured !== null && String(configured).trim() !== "") {
			return String(configured);
		}
		const number = runtime.i18n.formatNumber(index + 1, {
			minimumIntegerDigits: 2,
			useGrouping: false,
		});
		return field.nested?.rowLabels?.singular === undefined
			? runtime.i18n.t("fields:rowNumber", { number })
			: runtime.i18n.t("fields:labeledRowNumber", {
					label: field.nested.rowLabels.singular,
					number,
				});
	}

	function visibleRowLabel(row: Record<string, unknown>, index: number) {
		if (field.type !== "blocks") return rowLabel(row, index);
		const key = blockTypes.get(String(row.blockType))?.admin?.rowLabel;
		const configured = key === undefined ? undefined : row[key];
		return configured === undefined || configured === null || String(configured).trim() === ""
			? runtime.i18n.t("fields:untitled", {
					label: blockLabel(row) ?? runtime.i18n.t("fields:block"),
				})
			: String(configured);
	}

	function blockLabel(row: Record<string, unknown>) {
		if (field.type !== "blocks") return undefined;
		return (
			blockTypes.get(String(row.blockType))?.labels.singular ??
			(typeof row.blockType === "string" ? row.blockType : undefined)
		);
	}

	function revealRow(key: string) {
		return (element: HTMLElement) => {
			const reveal = () => {
				if (collapsed.has(key)) toggleCollapsed(key);
			};
			element.addEventListener(fieldIssueRevealEvent, reveal);
			return () => element.removeEventListener(fieldIssueRevealEvent, reveal);
		};
	}

	function toggleCollapsed(key: string) {
		const next = new Set(collapsed);
		next.has(key) ? next.delete(key) : next.add(key);
		collapsed = next;
	}

	function collapseAll() {
		collapsed = new Set(rows.map((row) => String(row._key)));
	}

	function showAll() {
		collapsed = new Set();
	}

	function fieldsFor(row: Record<string, unknown>) {
		if (field.type !== "blocks") return field.nested?.fields ?? [];
		return blockTypes.get(String(row.blockType))?.fields ?? [];
	}

	function fieldsForRow(row: Record<string, unknown>, index: number) {
		const rowKey = String(row._key);
		return fieldsFor(row)
			.filter((child) => field.type !== "blocks" || child.name !== "blockName")
			.map((child) => rowField(child, index, rowKey));
	}

	function rowLabelSnapshot(row: Record<string, unknown>, index: number) {
		const block = blockTypes.get(String(row.blockType));
		if (block === undefined) return immutableRowLabelSnapshot(row);
		const visible = { ...row };
		for (const name of ["blockName", block.admin?.rowLabel]) {
			const child = block.fields.find((field) => field.name === name);
			if (child !== undefined && !visibleBlockChild(form, rowField(child, index, String(row._key))))
				delete visible[child.name];
		}
		return immutableRowLabelSnapshot(visible);
	}

	function reorder(event: Parameters<DragDropEvents["dragend"]>[0]) {
		const source = event.operation.source;
		if (event.canceled || editingBlocked || !source || !isSortable(source)) return;
		const from = source.sortable.initialIndex;
		const to = source.sortable.index;
		if (from === to) return;
		const next = [...rows];
		const [moved] = next.splice(from, 1);
		if (moved !== undefined) next.splice(to, 0, moved);
		updateRows(next);
	}
</script>

{#snippet fieldActions()}
	<DropdownMenu>
		<DropdownMenuTrigger
			class={[buttonVariants({ variant: "ghost", size: "icon-xs" }), "ridu-nested-actions-trigger"]}
			aria-label={runtime.i18n.t("fields:openActions", { label: field.admin.label })}
		>
			<EllipsisIcon />
		</DropdownMenuTrigger>
		<DropdownMenuContent>
			<DropdownMenuItem onSelect={copyField}>
				{runtime.i18n.t("fields:copyField")}
			</DropdownMenuItem>
			<DropdownMenuItem disabled={editingBlocked} onSelect={pasteField}>
				{runtime.i18n.t("fields:pasteField")}
			</DropdownMenuItem>
		</DropdownMenuContent>
	</DropdownMenu>
{/snippet}

{#if field.type === "group" && field.admin.namedTab}
	<div data-field-path={field.path}>
		<FieldLayout fields={inheritedFields} {form} />
	</div>
{:else if field.type === "group"}
	<section
		class="ridu-group-field"
		data-field-path={field.path}
		data-invalid={issues.length > 0}
		aria-labelledby={`${field.id}-label`}
		{...controlARIA}
	>
		<FieldMessages controlID={field.id} {issues} description={field.admin.description}>
			<div class="ridu-group-field__header">
				<div class="ridu-group-field__heading">
					<h2 id={`${field.id}-label`} class="ridu-group-field__title">
						{field.admin.label}
						{#if field.required}
							<span class="ridu-field-required" aria-hidden="true">*</span>
						{/if}
					</h2>
					{#if field.admin.readOnly}
						<span class="ridu-group-field__status ridu-field-status">
							{runtime.i18n.t("fields:readOnly")}
						</span>
					{/if}
				</div>
				{#if layoutPresentation?.groupActions ?? true}
					{@render fieldActions()}
				{/if}
			</div>
		</FieldMessages>
		{#key groupPastes}
			<FieldLayout fields={inheritedFields} {form} />
		{/key}
		{#if clipboardMessage !== ""}
			<p class="ridu-nested-field__note" aria-live="polite">
				{clipboardMessage}
			</p>
		{/if}
	</section>
{:else}
	<section
		bind:this={repeatedRoot}
		class="ridu-repeated-field"
		data-field-path={field.path}
		data-invalid={allIssues.length > 0}
		aria-labelledby={`${field.id}-label`}
		{...controlARIA}
	>
		<FieldMessages controlID={field.id} {issues} description={field.admin.description}>
			<div class="ridu-repeated-field__header">
				<div class="ridu-repeated-field__heading-wrap">
					<h2 id={`${field.id}-label`} class="ridu-repeated-field__heading">
						{field.admin.label}
						{#if field.required}
							<span class="ridu-field-required" aria-hidden="true">*</span>
						{/if}
						{#if allIssues.length > 0}
							<span class="ridu-error-count">
								{errorCount(allIssues.length)}
							</span>
						{/if}
					</h2>
					{#if field.admin.readOnly}
						<span class="ridu-repeated-field__status ridu-field-status">
							{runtime.i18n.t("fields:readOnly")}
						</span>
					{/if}
				</div>
				<div class="ridu-repeated-field__actions">
					{#if rows.length > 0}
						<Button
							size="xs"
							variant="ghost"
							class="ridu-repeated-field__header-action"
							onclick={collapseAll}
						>
							{runtime.i18n.t("fields:collapseAll")}
						</Button>
						<Button
							size="xs"
							variant="ghost"
							class="ridu-repeated-field__header-action"
							onclick={showAll}
						>
							{runtime.i18n.t("fields:showAll")}
						</Button>
					{/if}
					{@render fieldActions()}
				</div>
			</div>
		</FieldMessages>
		{#if unknownRows}
			<p role="alert" class="ridu-repeated-field__recovery">
				{runtime.i18n.t("errors:unknownBlockRecovery")}
			</p>
		{/if}
		<DragDropProvider onDragEnd={reorder}>
			<div class="ridu-repeated-field__rows">
				{#each rows as row, index (form.rowMountKey(row))}
					{const rowKey = String(row._key)}
					{const rowCollapsed = $derived(collapsed.has(rowKey))}
					{const rowIssues = $derived(form.issuesFor(`${field.path}.${index}`))}
					{const rowSnapshot = $derived(rowLabelSnapshot(row, index))}
					{const block = $derived(blockTypes.get(String(row.blockType)))}
					<SortableRow
						id={String(row._key)}
						{index}
						disabled={editingBlocked}
						invalid={rowIssues.length > 0}
					>
						{#snippet children(sortable)}
							<div class="ridu-repeated-row__header">
								<Button
									variant="ghost"
									size="icon-xs"
									class="ridu-repeated-row__drag"
									aria-label={runtime.i18n.t("fields:drag", {
										label: rowLabel(row, index),
									})}
									disabled={editingBlocked}
									{@attach sortable.handleRef}
								>
									<GripVerticalIcon />
								</Button>
								<div id={`${field.id}-${rowKey}-row-label`} class="ridu-repeated-row__heading">
									{#if field.type === "blocks"}
										<span class="ridu-repeated-row__number">
											{runtime.i18n.formatNumber(index + 1, {
												minimumIntegerDigits: 2,
												useGrouping: false,
											})}
										</span>
									{/if}
									{#if blockLabel(row) !== undefined}
										<span class="ridu-repeated-row__type">
											{blockLabel(row)}
										</span>
									{/if}
									<div class="ridu-repeated-row__label">
										{#if block !== undefined}
											<BlockHeader
												{block}
												path={`${field.path}.${index}`}
												instance={rowKey}
												{form}
												readOnly={editingBlocked}
											/>
										{:else if customRowLabel === undefined}
											{visibleRowLabel(row, index)}
										{/if}
										{#if customRowLabel !== undefined}
											<customRowLabel.component
												{field}
												row={rowSnapshot}
												rowNumber={index + 1}
												i18n={runtime.i18n}
												{...customRowLabel.config === undefined
													? {}
													: { config: customRowLabel.config }}
											/>
										{/if}
										{#if rowIssues.length > 0}
											<button
												type="button"
												class="ridu-error-count"
												aria-label={`${errorCount(rowIssues.length)}: ${rowIssues[0].message}`}
												onclick={() =>
													repeatedRoot && focusFieldIssue(rowIssues[0].path, repeatedRoot)}
											>
												{errorCount(rowIssues.length)}
											</button>
										{/if}
									</div>
								</div>
								<button
									type="button"
									class="ridu-repeated-row__toggle"
									onclick={() => toggleCollapsed(rowKey)}
									aria-label={runtime.i18n.t(rowCollapsed ? "fields:expand" : "fields:collapse")}
									aria-describedby={`${field.id}-${rowKey}-row-label`}
									aria-expanded={!rowCollapsed}
									aria-controls={`${field.id}-${rowKey}-content`}
								>
									<ChevronDownIcon />
								</button>
								<div class="ridu-repeated-row__actions">
									<DropdownMenu>
										<DropdownMenuTrigger
											class={[
												buttonVariants({ variant: "ghost", size: "icon-xs" }),
												"ridu-nested-actions-trigger",
											]}
											aria-label={runtime.i18n.t("fields:openActions", {
												label: rowLabel(row, index),
											})}
										>
											<EllipsisIcon />
										</DropdownMenuTrigger>
										<DropdownMenuContent>
											<DropdownMenuItem
												disabled={editingBlocked || index === 0}
												onSelect={() => {
													const next = [...rows];
													[next[index - 1], next[index]] = [next[index], next[index - 1]];
													updateRows(next);
												}}
											>
												{runtime.i18n.t("fields:moveUp")}
											</DropdownMenuItem>
											<DropdownMenuItem
												disabled={editingBlocked || index === rows.length - 1}
												onSelect={() => {
													const next = [...rows];
													[next[index], next[index + 1]] = [next[index + 1], next[index]];
													updateRows(next);
												}}
											>
												{runtime.i18n.t("fields:moveDown")}
											</DropdownMenuItem>
											<DropdownMenuItem
												disabled={editingBlocked || !canAdd}
												onSelect={() => addBelow(index)}
											>
												{runtime.i18n.t("fields:addBelow")}
											</DropdownMenuItem>
											<DropdownMenuSeparator />
											<DropdownMenuItem onSelect={() => copyRow(index)}>
												{runtime.i18n.t("fields:copyRow")}
											</DropdownMenuItem>
											<DropdownMenuItem
												disabled={editingBlocked || !canAdd}
												onSelect={() => pasteRow(index)}
											>
												{runtime.i18n.t("fields:pasteRow")}
											</DropdownMenuItem>
											<DropdownMenuItem
												disabled={editingBlocked || !canAdd}
												onSelect={() => duplicate(index)}
											>
												{runtime.i18n.t("fields:duplicate")}
											</DropdownMenuItem>
											<DropdownMenuSeparator />
											<DropdownMenuItem
												class="ridu-repeated-row__remove-action"
												disabled={editingBlocked ||
													rows.length <= Math.max(minRows, field.required ? 1 : 0)}
												onSelect={() =>
													updateRows(rows.filter((_, rowIndex) => rowIndex !== index))}
											>
												{runtime.i18n.t("general:remove")}
											</DropdownMenuItem>
										</DropdownMenuContent>
									</DropdownMenu>
								</div>
							</div>
							<div
								{@attach revealRow(rowKey)}
								data-field-path={`${field.path}.${index}`}
								id={`${field.id}-${rowKey}-content`}
								class="ridu-repeated-row__content"
								data-collapsed={rowCollapsed}
								aria-hidden={rowCollapsed}
								inert={rowCollapsed}
							>
								<div class="ridu-repeated-row__content-clip">
									<div class="ridu-repeated-row__fields">
										{#if !rowCollapsed || form
												.pendingEditIssues()
												.some((issue) => issue.path.startsWith(`${field.path}.${index}.`))}
											<FieldLayout fields={fieldsForRow(row, index)} {form} />
										{/if}
									</div>
								</div>
							</div>
						{/snippet}
					</SortableRow>
				{/each}
			</div>
		</DragDropProvider>
		<Button
			variant="ghost"
			size="sm"
			class="ridu-repeated-field__add"
			disabled={editingBlocked || !canAdd}
			onclick={() => add()}
		>
			<CirclePlusIcon />
			{field.type === "blocks"
				? runtime.i18n.t("fields:addBlock")
				: field.nested?.rowLabels?.singular === undefined
					? runtime.i18n.t("fields:addRow")
					: runtime.i18n.t("fields:add", { label: field.nested.rowLabels.singular })}
		</Button>
		{#if minRows > 0 || maxRows > 0}
			<p class="ridu-nested-field__note">
				{runtime.i18n.t("fields:rowLimits", {
					count: rows.length,
					minimum:
						minRows > 0
							? runtime.i18n.t("fields:minimum", { count: minRows })
							: runtime.i18n.t("fields:noMinimum"),
					maximum:
						maxRows > 0
							? runtime.i18n.t("fields:maximum", { count: maxRows })
							: runtime.i18n.t("fields:noMaximum"),
				})}
			</p>
		{/if}
		{#if clipboardMessage !== ""}
			<p class="ridu-nested-field__note" aria-live="polite">
				{clipboardMessage}
			</p>
		{/if}
	</section>
{/if}

{#if field.type === "blocks" && blockPickerOpen}
	<BlockPicker
		open={blockPickerOpen}
		label={field.admin.label}
		{blocks}
		onOpenChange={changeBlockPickerOpen}
		onSelect={add}
	/>
{/if}
