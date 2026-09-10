<script lang="ts">
	import { resolveBlockTypes } from "@riducms/protocol";

	import type { SchemaField } from "@riducms/protocol";
	import { fieldControlARIA } from "@riducms/ui";
	import { DragDropProvider, type DragDropEvents } from "@dnd-kit-svelte/svelte";
	import ChevronDownIcon from "~icons/lucide/chevron-down";
	import CirclePlusIcon from "~icons/lucide/circle-plus";
	import EllipsisIcon from "~icons/lucide/ellipsis";
	import GripVerticalIcon from "~icons/lucide/grip-vertical";
	import LayoutTemplateIcon from "~icons/lucide/layout-template";
	import SearchIcon from "~icons/lucide/search";

	import { Button, buttonVariants } from "@admin/components/ui/button";
	import {
		Dialog,
		DialogContent,
		DialogDescription,
		DialogHeader,
		DialogTitle,
	} from "@admin/components/ui/dialog";
	import {
		DropdownMenu,
		DropdownMenuContent,
		DropdownMenuItem,
		DropdownMenuSeparator,
		DropdownMenuTrigger,
	} from "@admin/components/ui/dropdown-menu";
	import { Input } from "@admin/components/ui/input";
	import type { FormController } from "@admin/core/forms/form-controller.svelte";
	import { initialFormValues } from "@admin/core/forms/form-schema";
	import { immutableRowLabelSnapshot } from "@admin/core/plugins/row-label-registry";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import { focusFieldIssue, fieldIssueRevealEvent } from "@admin/core/forms/field-issue-focus";
	import FieldLayout from "@admin/fields/field-layout.svelte";
	import FieldMessages from "@admin/fields/field-messages.svelte";
	import {
		compatibleClipboardValue,
		cloneFieldForPaste,
		createFieldClipboardPayload,
		readFieldClipboard,
		writeFieldClipboard,
	} from "@admin/fields/field-clipboard";
	import { scopeRepeatedRowField } from "@admin/fields/nested/scoped-field";
	import SortableRow from "@admin/fields/nested/sortable-row.svelte";

	let { field, form }: { field: SchemaField; form: FormController } = $props();
	const runtime = getAdminRuntime();
	const customRowLabel = $derived(runtime.rowLabels.resolve(field));
	const rows = $derived((form.get(field.path) as Record<string, unknown>[] | undefined) ?? []);
	const issues = $derived(form.issuesFor(field.path).filter((issue) => issue.path === field.path));
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
		field.type === "blocks" &&
			rows.some(
				(row) => !resolveBlockTypes(field.blocks).some((block) => block.slug === row.blockType)
			)
	);
	const readOnly = $derived(field.admin.readOnly === true || unknownRows);
	const editingBlocked = $derived(readOnly || form.editingBlocked);
	const canAdd = $derived(!editingBlocked && (maxRows === 0 || rows.length < maxRows));
	let collapsed = $state(new Set<string>());
	let clipboardMessage = $state("");
	let blockPickerOpen = $state(false);
	let blockQuery = $state("");
	let insertAfter = $state<string>();
	const visibleBlockTypes = $derived(
		(resolveBlockTypes(field.blocks) ?? []).filter((block) =>
			block.labels.singular
				.toLocaleLowerCase(runtime.i18n.language)
				.includes(blockQuery.trim().toLocaleLowerCase(runtime.i18n.language))
		)
	);

	function changeBlockPickerOpen(open: boolean) {
		blockPickerOpen = open;
		if (!open) {
			blockQuery = "";
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

	function createRow(blockType?: string) {
		const children =
			field.type === "blocks"
				? (resolveBlockTypes(field.blocks).find((block) => block.slug === blockType)?.fields ?? [])
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
		blockQuery = "";
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
		const value = compatibleClipboardValue(await readFieldClipboard(), field, "field");
		if (value === undefined) {
			clipboardMessage = runtime.i18n.t("errors:clipboardField");
			return;
		}
		if ((field.type === "array" || field.type === "blocks") && !Array.isArray(value)) {
			clipboardMessage = runtime.i18n.t("errors:clipboardRows");
			return;
		}
		if (
			field.type === "group" &&
			(value === null || typeof value !== "object" || Array.isArray(value))
		) {
			clipboardMessage = runtime.i18n.t("errors:clipboardGroup");
			return;
		}
		if (Array.isArray(value) && value.length < minRows) {
			clipboardMessage = runtime.i18n.t("errors:clipboardMinRows", { count: minRows });
			return;
		}
		if (Array.isArray(value) && maxRows > 0 && value.length > maxRows) {
			clipboardMessage = runtime.i18n.t("errors:clipboardMaxRows", { count: maxRows });
			return;
		}
		const pasted = Array.isArray(value)
			? value.map((row) =>
					row !== null && typeof row === "object" && !Array.isArray(row)
						? { ...row, _key: crypto.randomUUID() }
						: row
				)
			: value;
		if (Array.isArray(pasted)) updateRows(pasted as Record<string, unknown>[]);
		else form.set(field.path, pasted);
		clipboardMessage = runtime.i18n.t("fields:pasted", { label: field.admin.label });
	}

	async function copyRow(index: number) {
		await writeFieldClipboard(createFieldClipboardPayload(field, "row", rows[index]));
		clipboardMessage = runtime.i18n.t("fields:copied", {
			label: rowLabel(rows[index] ?? {}, index),
		});
	}

	async function pasteRow(index: number) {
		if (editingBlocked || !canAdd) return;
		const value = compatibleClipboardValue(await readFieldClipboard(), field, "row");
		if (value === null || typeof value !== "object" || Array.isArray(value)) {
			clipboardMessage = runtime.i18n.t("errors:clipboardRow");
			return;
		}
		const row = value as Record<string, unknown>;
		row._key = crypto.randomUUID();
		if (
			field.type === "blocks" &&
			!resolveBlockTypes(field.blocks).some((block) => block.slug === row.blockType)
		) {
			clipboardMessage = runtime.i18n.t("errors:clipboardBlockType");
			return;
		}
		const next = [...rows];
		next.splice(index + 1, 0, row);
		updateRows(next);
		clipboardMessage = runtime.i18n.t("fields:pasted", { label: rowLabel(row, index + 1) });
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

	function rowLabel(row: Record<string, unknown>, index: number) {
		const key =
			field.type === "blocks"
				? resolveBlockTypes(field.blocks).find((block) => block.slug === row.blockType)?.admin
						?.rowLabel
				: field.nested?.rowLabel;
		const configured = key === undefined ? undefined : row[key];
		if (configured !== undefined && configured !== null && String(configured).trim() !== "") {
			return String(configured);
		}
		const number = runtime.i18n.formatNumber(index + 1);
		return field.nested?.rowLabels?.singular === undefined
			? runtime.i18n.t("fields:rowNumber", { number })
			: runtime.i18n.t("fields:labeledRowNumber", {
					label: field.nested.rowLabels.singular,
					number,
				});
	}

	function visibleRowLabel(row: Record<string, unknown>, index: number) {
		if (field.type !== "blocks") return rowLabel(row, index);
		const key = resolveBlockTypes(field.blocks).find((block) => block.slug === row.blockType)?.admin
			?.rowLabel;
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
			resolveBlockTypes(field.blocks).find((block) => block.slug === row.blockType)?.labels
				.singular ?? (typeof row.blockType === "string" ? row.blockType : undefined)
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
		return (
			resolveBlockTypes(field.blocks).find((block) => block.slug === row.blockType)?.fields ?? []
		);
	}

	function fieldsForRow(row: Record<string, unknown>, index: number) {
		const rowKey = String(row._key);
		return fieldsFor(row).map((child) => rowField(child, index, rowKey));
	}

	function reorder(event: Parameters<DragDropEvents["dragend"]>[0]) {
		if (editingBlocked) return;
		const source = String(event.operation.source?.id ?? "");
		const target = String(event.operation.target?.id ?? "");
		const from = rows.findIndex((row) => row._key === source);
		const to = rows.findIndex((row) => row._key === target);
		if (event.canceled || from < 0 || to < 0 || from === to) return;
		const next = [...rows];
		const [moved] = next.splice(from, 1);
		if (moved !== undefined) next.splice(to, 0, moved);
		updateRows(next);
	}
</script>

{#snippet fieldActions()}
	<DropdownMenu>
		<DropdownMenuTrigger
			class={buttonVariants({ variant: "ghost", size: "icon-xs" })}
			aria-label={runtime.i18n.t("fields:openActions", { label: field.admin.label })}
		>
			<EllipsisIcon class="size-4" />
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
		class="grid gap-5"
		data-field-path={field.path}
		data-invalid={issues.length > 0}
		aria-labelledby={`${field.id}-label`}
		{...controlARIA}
	>
		<FieldMessages controlID={field.id} {issues} description={field.admin.description}>
			<div class="flex items-start justify-between gap-3 border-b border-control-border pb-2.5">
				<div class="min-w-0">
					<h2 id={`${field.id}-label`} class="text-[17px] font-medium text-foreground">
						{field.admin.label}{#if field.required}<span
								class="ridu-field-required"
								aria-hidden="true"
							>
								*
							</span>{/if}
					</h2>
					{#if field.admin.readOnly}<span class="ridu-field-status mt-1">
							{runtime.i18n.t("fields:readOnly")}
						</span>{/if}
				</div>
				{@render fieldActions()}
			</div>
		</FieldMessages>
		<FieldLayout fields={inheritedFields} {form} />
		{#if clipboardMessage !== ""}<p
				class="font-mono text-[10px] text-foreground-faint"
				aria-live="polite"
			>
				{clipboardMessage}
			</p>{/if}
	</section>
{:else}
	<section
		class="grid gap-3"
		data-field-path={field.path}
		data-invalid={issues.length > 0}
		aria-labelledby={`${field.id}-label`}
		{...controlARIA}
	>
		<FieldMessages controlID={field.id} {issues} description={field.admin.description}>
			<div class="flex flex-wrap items-end justify-between gap-3">
				<div class="min-w-0">
					<h2 id={`${field.id}-label`} class="text-[19px] leading-7 font-medium text-foreground">
						{field.admin.label}{#if field.required}<span
								class="ridu-field-required"
								aria-hidden="true"
							>
								*
							</span>{/if}
					</h2>
					{#if field.admin.readOnly}<span class="ridu-field-status mt-1">
							{runtime.i18n.t("fields:readOnly")}
						</span>{/if}
				</div>
				<div class="flex items-center gap-1">
					{#if rows.length > 0}
						<Button size="xs" variant="ghost" class="px-1.5" onclick={collapseAll}>
							{runtime.i18n.t("fields:collapseAll")}
						</Button>
						<Button size="xs" variant="ghost" class="px-1.5" onclick={showAll}>
							{runtime.i18n.t("fields:showAll")}
						</Button>
					{/if}
					{@render fieldActions()}
				</div>
			</div>
		</FieldMessages>
		{#if unknownRows}<p role="alert" class="text-sm text-destructive">
				{runtime.i18n.t("errors:unknownBlockRecovery")}
			</p>{/if}
		<DragDropProvider onDragEnd={reorder}>
			<div class="grid gap-3">
				{#each rows as row, index (form.rowMountKey(row))}
					{const rowKey = String(row._key)}
					{const rowCollapsed = $derived(collapsed.has(rowKey))}
					{const rowIssues = $derived(form.issuesFor(`${field.path}.${index}`))}
					{const rowSnapshot = $derived(immutableRowLabelSnapshot(row))}
					<SortableRow id={String(row._key)} {index} disabled={editingBlocked}>
						{#snippet children(sortable)}
							<div class="flex min-h-10 items-center justify-between gap-2 bg-control px-3 py-2">
								<Button
									variant="ghost"
									size="icon-xs"
									class="shrink-0 cursor-grab text-foreground-faint hover:bg-background hover:text-foreground active:cursor-grabbing"
									aria-label={runtime.i18n.t("fields:drag", {
										label: rowLabel(row, index),
									})}
									disabled={editingBlocked}
									{@attach sortable.handleRef}
								>
									<GripVerticalIcon class="size-3.5 text-foreground-faint" />
								</Button>
								<div
									id={`${field.id}-${rowKey}-row-label`}
									class="flex min-w-0 flex-1 items-center gap-2 text-[12.5px] text-foreground-muted"
								>
									<span class="font-mono text-[10px] text-foreground-faint">
										{runtime.i18n.formatNumber(index + 1, {
											minimumIntegerDigits: 2,
											useGrouping: false,
										})}
									</span>
									{#if blockLabel(row) !== undefined}<span
											class="rounded-[2px] bg-background px-1.5 py-0.5 text-[11px] text-foreground"
										>
											{blockLabel(row)}
										</span>{/if}
									<div class="min-w-0 flex-1 truncate">
										{#if customRowLabel === undefined}
											{visibleRowLabel(row, index)}
										{:else}
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
									</div>
								</div>
								<button
									type="button"
									class="group shrink-0 rounded-[3px] p-1 text-foreground-faint outline-none hover:bg-background hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/70"
									onclick={() => toggleCollapsed(rowKey)}
									aria-label={runtime.i18n.t(rowCollapsed ? "fields:expand" : "fields:collapse")}
									aria-describedby={`${field.id}-${rowKey}-row-label`}
									aria-expanded={!rowCollapsed}
									aria-controls={`${field.id}-${rowKey}-content`}
								>
									<ChevronDownIcon
										class={[
											"ms-auto size-3.5 shrink-0 text-foreground-faint transition-transform duration-200 motion-reduce:transition-none",
											rowCollapsed &&
												(runtime.i18n.direction === "rtl" ? "rotate-90" : "-rotate-90"),
										]}
									/>
								</button>
								<div class="flex items-center gap-0.5">
									<DropdownMenu>
										<DropdownMenuTrigger
											class={buttonVariants({ variant: "ghost", size: "icon-xs" })}
											aria-label={runtime.i18n.t("fields:openActions", {
												label: rowLabel(row, index),
											})}
										>
											<EllipsisIcon class="size-4" />
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
												class="text-destructive data-[highlighted]:text-destructive"
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
							{#if rowIssues.length > 0}
								<button
									type="button"
									class="px-4 py-2 text-start text-sm text-destructive underline"
									onclick={() => focusFieldIssue(rowIssues[0].path)}
								>
									{runtime.i18n.t("fields:rowErrors", { count: rowIssues.length })}: {rowIssues[0]
										.message}
								</button>
							{/if}
							<div
								{@attach revealRow(rowKey)}
								data-field-path={`${field.path}.${index}`}
								id={`${field.id}-${rowKey}-content`}
								class={[
									"grid transition-[grid-template-rows,opacity] duration-200 ease-out motion-reduce:transition-none",
									rowCollapsed
										? "invisible grid-rows-[0fr] opacity-0"
										: "grid-rows-[1fr] opacity-100",
								]}
								aria-hidden={rowCollapsed}
								inert={rowCollapsed}
							>
								<div class="min-h-0 overflow-hidden">
									<div class="grid gap-5 p-4">
										{#if !rowCollapsed}<FieldLayout fields={fieldsForRow(row, index)} {form} />{/if}
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
			class="w-fit px-0 text-foreground-faint hover:bg-transparent hover:text-foreground"
			disabled={editingBlocked || !canAdd}
			onclick={() => add()}
		>
			<CirclePlusIcon class="size-5" />
			{field.type === "blocks"
				? runtime.i18n.t("fields:addBlock")
				: field.nested?.rowLabels?.singular === undefined
					? runtime.i18n.t("fields:addRow")
					: runtime.i18n.t("fields:add", { label: field.nested.rowLabels.singular })}
		</Button>
		{#if minRows > 0 || maxRows > 0}
			<p class="font-mono text-[10px] text-foreground-faint">
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
		{#if clipboardMessage !== ""}<p
				class="font-mono text-[10px] text-foreground-faint"
				aria-live="polite"
			>
				{clipboardMessage}
			</p>{/if}
	</section>
{/if}

{#if field.type === "blocks"}
	<Dialog bind:open={() => blockPickerOpen, changeBlockPickerOpen}>
		<DialogContent class="h-[calc(100vh-4rem)] sm:max-w-4xl">
			<DialogHeader>
				<DialogTitle class="text-[22px]">
					{runtime.i18n.t("fields:add", { label: field.admin.label })}
				</DialogTitle>
				<DialogDescription class="sr-only">
					{runtime.i18n.t("fields:chooseBlockFor", { label: field.admin.label })}
				</DialogDescription>
			</DialogHeader>
			<div class="relative">
				<SearchIcon
					class="pointer-events-none absolute top-1/2 start-3 size-4 -translate-y-1/2 text-foreground-faint"
				/>
				<Input
					class="ps-9"
					placeholder={runtime.i18n.t("fields:searchBlock")}
					aria-label={runtime.i18n.t("fields:searchBlock")}
					bind:value={blockQuery}
				/>
			</div>
			{#if visibleBlockTypes.length === 0}
				<p class="py-10 text-center text-[13px] text-foreground-muted">
					{runtime.i18n.t("fields:noBlocks")}
				</p>
			{:else}
				<div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
					{#each visibleBlockTypes as block (block.slug)}
						<button
							type="button"
							class="group overflow-hidden rounded-[3px] border border-control-border bg-background text-start transition-colors hover:border-control-border-hover focus-visible:border-ring focus-visible:outline-none"
							onclick={() => add(block.slug)}
						>
							<span
								class="flex aspect-video items-center justify-center border-b border-control-border bg-control text-foreground-faint transition-colors group-hover:bg-control-hover group-hover:text-foreground-muted"
							>
								<LayoutTemplateIcon class="size-8" />
							</span>
							<span class="block px-3 py-2.5 text-[13px] font-medium text-foreground">
								{block.labels.singular}
							</span>
						</button>
					{/each}
				</div>
			{/if}
		</DialogContent>
	</Dialog>
{/if}
