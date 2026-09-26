<script lang="ts">
	import { tick } from "svelte";
	import type { SchemaField } from "@riducms/protocol";
	import { getAdminI18n } from "@riducms/plugin";
	import Plus from "@admin/components/icons/plus.svelte";
	import FilterRow from "@admin/features/collections/controls/filter-row.svelte";
	import {
		filterOperatorsFor,
		type ListFilter,
		type ListFilterGroup,
	} from "@admin/features/collections/list-workspace";
	import "@admin/features/collections/controls/list-controls.scss";

	let {
		fields,
		filters,
		label,
		onChange,
		navigationIdle = true,
	}: {
		fields: readonly SchemaField[];
		filters: readonly ListFilterGroup[];
		label: string;
		onChange: (filters: ListFilterGroup[]) => void;
		navigationIdle?: boolean;
	} = $props();

	const i18n = getAdminI18n();
	let root: HTMLDivElement | undefined;
	let pendingFocus = $state<{
		element: HTMLElement;
		encoded: string;
		start: number | null;
		end: number | null;
	}>();

	$effect(() => {
		const focus = pendingFocus;
		if (!focus || !navigationIdle || JSON.stringify(filters) !== focus.encoded) return;

		let active = true;
		tick().then(() => {
			if (!active || !focus.element.isConnected) return;

			pendingFocus = undefined;
			focus.element.focus({ preventScroll: true });
			if (focus.element instanceof HTMLInputElement && focus.start !== null && focus.end !== null)
				focus.element.setSelectionRange(focus.start, focus.end);
		});

		return () => {
			active = false;
		};
	});

	type Row = { id: string; filter: ListFilter };

	let groups = $state<{ id: string; rows: Row[] }[]>([]);
	let lastEmittedFilters = "";
	let lastSourceFilters: string | undefined;
	let commitTimer: ReturnType<typeof setTimeout> | undefined;

	// Acknowledge this builder's writes without replacing draft row identities and input focus.
	$effect.pre(() => {
		const encoded = JSON.stringify(filters);
		if (encoded === lastSourceFilters) return;

		lastSourceFilters = encoded;
		if (encoded === lastEmittedFilters) return;

		clearTimeout(commitTimer);
		lastEmittedFilters = encoded;
		groups = filters.map((group) => ({
			id: crypto.randomUUID(),
			rows: group.map((filter) => ({ id: crypto.randomUUID(), filter: { ...filter } })),
		}));
	});

	$effect(() => () => clearTimeout(commitTimer));

	function scheduleCommit() {
		clearTimeout(commitTimer);
		commitTimer = setTimeout(() => {
			const value = groups
				.map((group) =>
					group.rows
						.map((row) => row.filter)
						.filter(
							(filter) =>
								fields.some((field) => field.path === filter.field) &&
								(filter.operator === "exists" || filter.value !== "")
						)
				)
				.filter((group) => group.length > 0);
			lastEmittedFilters = JSON.stringify(value);
			if (lastEmittedFilters !== JSON.stringify(filters)) {
				const element = document.activeElement;
				if (element instanceof HTMLElement && root?.contains(element))
					pendingFocus = {
						element,
						encoded: lastEmittedFilters,
						start: element instanceof HTMLInputElement ? element.selectionStart : null,
						end: element instanceof HTMLInputElement ? element.selectionEnd : null,
					};

				onChange(value);
			}
		}, 300);
	}

	function createRow(): Row {
		return {
			id: crypto.randomUUID(),
			filter: {
				field: fields[0]?.path ?? "",
				operator: fields[0] ? filterOperatorsFor(fields[0])[0]! : "equals",
				value: "",
			},
		};
	}

	function addGroup() {
		groups.push({ id: crypto.randomUUID(), rows: [createRow()] });
	}

	function rowIndexInList(groupIndex: number, rowIndex: number) {
		return (
			groups.slice(0, groupIndex).reduce((count, group) => count + group.rows.length, 0) + rowIndex
		);
	}

	async function removeRow(groupIndex: number, rowIndex: number) {
		const focused = root?.contains(document.activeElement);
		const index = rowIndexInList(groupIndex, rowIndex);
		groups[groupIndex]!.rows.splice(rowIndex, 1);
		if (groups[groupIndex]!.rows.length === 0) groups.splice(groupIndex, 1);

		await tick();
		if (focused) {
			const buttons = root?.querySelectorAll<HTMLButtonElement>("[data-filter-remove]");
			const target = buttons?.length
				? buttons[Math.min(index, buttons.length - 1)]
				: root?.querySelector<HTMLButtonElement>("[data-filter-add]");
			target?.focus({ preventScroll: true });
		}

		scheduleCommit();
	}
</script>

<div class="ridu-list-filter" bind:this={root}>
	{#if groups.length === 0}
		<p>{i18n.t("collections:noFilters")}</p>
		<button
			type="button"
			class="ridu-list__text-action"
			onclick={addGroup}
			data-filter-add
			disabled={fields.length === 0}
		>
			<Plus />{i18n.t("collections:addFilter")}
		</button>
	{:else}
		<p>{i18n.t("collections:filterWhere", { label })}</p>

		{#each groups as group, groupIndex (group.id)}
			{#if groupIndex > 0}
				<p class="ridu-list-filter__join">{i18n.t("collections:or")}</p>
			{/if}
			{#each group.rows as row, rowIndex (row.id)}
				{#if rowIndex > 0}
					<p class="ridu-list-filter__join">{i18n.t("collections:and")}</p>
				{/if}
				<FilterRow
					{fields}
					filter={row.filter}
					index={rowIndexInList(groupIndex, rowIndex)}
					onChange={(filter) => {
						row.filter = filter;
						scheduleCommit();
					}}
					onAdd={() => group.rows.splice(rowIndex + 1, 0, createRow())}
					onRemove={() => removeRow(groupIndex, rowIndex)}
				/>
			{/each}
		{/each}

		<button type="button" class="ridu-list__text-action" onclick={addGroup}>
			<span class="ridu-list__circle"><Plus /></span>
			{i18n.t("collections:or")}
		</button>
	{/if}
</div>
