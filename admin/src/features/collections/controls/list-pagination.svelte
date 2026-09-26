<script lang="ts">
	import { getAdminI18n } from "@riducms/plugin";
	import { Select, SelectContent, SelectItem, SelectTrigger } from "@admin/components/ui/select";
	import Chevron from "@admin/components/icons/chevron.svelte";
	import { listPageSizes, type ListPageSize } from "@admin/features/collections/list-workspace";
	import type { Pagination } from "@riducms/protocol";
	import "@admin/features/collections/controls/list-pagination.scss";

	let {
		pagination,
		pageSize,
		busy = false,
		inert = false,
		onPageChange,
		onPageSizeChange,
	}: {
		pagination: Pagination;
		pageSize: ListPageSize;
		busy?: boolean;
		inert?: boolean;
		onPageChange: (page: number) => void;
		onPageSizeChange: (size: ListPageSize) => void;
	} = $props();

	const i18n = getAdminI18n();
	const rangeStart = $derived(
		pagination.totalDocs === 0 ? 0 : (pagination.page - 1) * pagination.limit + 1
	);
	const rangeEnd = $derived(Math.min(pagination.page * pagination.limit, pagination.totalDocs));

	const pages = $derived.by(() => {
		const candidates = [
			...new Set([
				1,
				pagination.totalPages,
				...Array.from({ length: 5 }, (_, i) => pagination.page + i - 2),
			]),
		]
			.filter((page) => page > 0 && page <= pagination.totalPages)
			.sort((a, b) => a - b);

		return candidates.flatMap((page, index) =>
			index > 0 && page > candidates[index - 1]! + 1 ? ["gap" as const, page] : [page]
		);
	});
</script>

<div class="ridu-list-pagination" {inert}>
	<nav aria-label={i18n.t("collections:pagination")}>
		{#if pagination.totalPages > 1}
			<button
				type="button"
				class="ridu-list-pagination__page"
				aria-label={i18n.t("collections:previousPage")}
				disabled={!pagination.hasPrevPage || busy}
				onclick={() => onPageChange(pagination.page - 1)}
			>
				<Chevron class="ridu-list__chevron-previous" />
			</button>
			{#each pages as page, index (`${page}:${index}`)}
				{#if page === "gap"}
					<span>…</span>
				{:else}
					<button
						type="button"
						class="ridu-list-pagination__page"
						aria-current={pagination.page === page ? "page" : undefined}
						disabled={busy}
						onclick={() => onPageChange(page)}
					>
						{i18n.formatNumber(page)}
					</button>
				{/if}
			{/each}
			<button
				type="button"
				class="ridu-list-pagination__page"
				aria-label={i18n.t("collections:nextPage")}
				disabled={!pagination.hasNextPage || busy}
				onclick={() => onPageChange(pagination.page + 1)}
			>
				<Chevron class="ridu-list__chevron-next" />
			</button>
		{/if}
	</nav>
	<div class="ridu-list-pagination__summary">
		<span>
			{i18n.t("collections:rangeSummary", {
				start: i18n.formatNumber(rangeStart),
				end: i18n.formatNumber(rangeEnd),
				total: i18n.formatNumber(pagination.totalDocs),
			})}
		</span>
		<Select
			type="single"
			value={String(pageSize)}
			onValueChange={(value) => onPageSizeChange(Number(value) as ListPageSize)}
		>
			<SelectTrigger
				class="ridu-list-pagination__size"
				aria-label={i18n.t("collections:documentsPerPage")}
			>
				{i18n.t("collections:perPageLabel", { count: i18n.formatNumber(pageSize) })}
			</SelectTrigger>
			<SelectContent align="end">
				{#each listPageSizes as size}
					<SelectItem
						value={String(size)}
						label={i18n.t("collections:perPage", { count: i18n.formatNumber(size) })}
					/>
				{/each}
			</SelectContent>
		</Select>
	</div>
</div>
