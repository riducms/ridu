<script lang="ts">
	import { getAdminI18n } from "@riducms/plugin";
	import Chevron from "@admin/components/icons/chevron.svelte";
	import ArrowUp from "~icons/lucide/arrow-up";
	import ArrowDown from "~icons/lucide/arrow-down";
	import { sortableField, type ListColumn } from "@admin/features/collections/list-workspace";
	import "@admin/features/collections/controls/sort-heading.scss";

	let {
		column,
		sort,
		onSort,
		disabled = false,
		ready = true,
		compact = false,
	}: {
		column: ListColumn;
		sort: string;
		onSort: (path: string, descending: boolean) => void;
		disabled?: boolean;
		ready?: boolean;
		compact?: boolean;
	} = $props();

	const i18n = getAdminI18n();
	const sortable = $derived(column.field === undefined || sortableField(column.field));
	const active = $derived(sort.replace(/^-/, "") === column.path);
	const descending = $derived(sort.startsWith("-"));
	let pendingFocus = $state<boolean>();

	function selectSort(nextDescending: boolean) {
		pendingFocus = nextDescending;
		onSort(column.path, nextDescending);
	}

	// A navigation may temporarily make the results inert; restore the clicked direction afterward.
	function restoreFocus(node: HTMLDivElement) {
		if (!ready || pendingFocus === undefined) return;

		const direction = pendingFocus ? "descending" : "ascending";
		pendingFocus = undefined;
		node.querySelector<HTMLButtonElement>(`[data-ridu-sort-direction="${direction}"]`)?.focus();
	}
</script>

<th
	scope="col"
	aria-sort={sortable && active ? (descending ? "descending" : "ascending") : undefined}
>
	<div class={["ridu-list-sort", compact && "ridu-list-sort--compact"]} {@attach restoreFocus}>
		<span>{column.label}</span>
		{#if sortable}
			<div class="ridu-list-sort__buttons">
				{#each [false, true] as direction}
					<button
						type="button"
						data-active={active && descending === direction}
						data-ridu-sort-path={column.path}
						data-ridu-sort-direction={direction ? "descending" : "ascending"}
						aria-label={i18n.t(
							direction ? "collections:sortDescending" : "collections:sortAscending",
							{ label: column.label }
						)}
						{disabled}
						onclick={() => selectSort(direction)}
					>
						{#if compact}
							{const Icon = direction ? ArrowDown : ArrowUp}
							<Icon aria-hidden="true" />
						{:else}
							<Chevron class={!direction ? "ridu-list-sort__ascending" : undefined} />
						{/if}
					</button>
				{/each}
			</div>
		{/if}
	</div>
</th>
