<script lang="ts">
	import { tick } from "svelte";
	import { DragDropProvider, type DragDropEvents } from "@dnd-kit-svelte/svelte";
	import { isSortable } from "@dnd-kit-svelte/svelte/sortable";
	import { getAdminI18n } from "@riducms/plugin";
	import ColumnPill from "@admin/features/collections/controls/column-pill.svelte";
	import type { ListColumn, ListColumnSelection } from "@admin/features/collections/list-workspace";
	import "@admin/features/collections/controls/list-controls.scss";

	let {
		columns,
		availableColumns,
		canReadField,
		onChange,
		navigationIdle = true,
	}: {
		columns: readonly ListColumnSelection[];
		availableColumns: readonly ListColumn[];
		canReadField: (path: string) => boolean;
		onChange: (columns: ListColumnSelection[]) => void;
		navigationIdle?: boolean;
	} = $props();

	const i18n = getAdminI18n();
	let root: HTMLDivElement | undefined;
	let dragging = $state(false);
	let pendingColumnFocus = $state<{ element: HTMLElement; columns: string }>();

	$effect(() => {
		const focus = pendingColumnFocus;
		if (!focus || !navigationIdle || JSON.stringify(columns) !== focus.columns) return;

		let active = true;
		tick().then(() => {
			if (!active) return;

			pendingColumnFocus = undefined;
			if (focus.element.isConnected) focus.element.focus({ preventScroll: true });
		});

		return () => {
			active = false;
		};
	});

	const orderedColumns = $derived(
		columns.flatMap((selection) => {
			const column = availableColumns.find((column) => column.path === selection.path);

			return column && (column.field === undefined || canReadField(column.path))
				? [{ ...column, active: selection.active }]
				: [];
		})
	);

	function setColumns(nextColumns: ListColumnSelection[]) {
		const element = document.activeElement;
		if (element instanceof HTMLElement && root?.contains(element))
			pendingColumnFocus = { element, columns: JSON.stringify(nextColumns) };

		onChange(nextColumns);
	}

	function toggleColumn(path: string) {
		setColumns(
			columns.map((column) =>
				column.path === path ? { ...column, active: !column.active } : column
			)
		);
	}

	function reorder(event: Parameters<DragDropEvents["dragend"]>[0]) {
		dragging = false;
		const source = event.operation.source;
		if (event.canceled || !source || !isSortable(source)) return;

		const from = source.sortable.initialIndex;
		const to = source.sortable.index;
		if (from < 0 || to < 0 || from === to) return;

		const order = [...orderedColumns];
		const [moved] = order.splice(from, 1);
		order.splice(to, 0, moved!);
		const readable = new Set(order.map((column) => column.path));
		let next = 0;
		setColumns(
			columns.map((column) => {
				if (!readable.has(column.path)) return column;
				const replacement = order[next++]!;
				return { path: replacement.path, active: replacement.active };
			})
		);
	}
</script>

<DragDropProvider onDragStart={() => (dragging = true)} onDragEnd={reorder}>
	<div class="ridu-list-columns" bind:this={root}>
		{#each orderedColumns as column, index (column.path)}
			<ColumnPill
				disabled={dragging}
				path={column.path}
				label={column.label}
				{index}
				selected={column.active}
				onToggle={() => toggleColumn(column.path)}
				reorderLabel={i18n.t("collections:reorderColumn", { label: column.label })}
			/>
		{/each}
	</div>
</DragDropProvider>
