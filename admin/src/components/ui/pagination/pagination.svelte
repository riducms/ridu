<script lang="ts">
	import ChevronLeftIcon from "~icons/lucide/chevron-left";
	import ChevronRightIcon from "~icons/lucide/chevron-right";
	import { getAdminI18n } from "@riducms/plugin";

	import { Button } from "@admin/components/ui/button";

	let {
		start,
		end,
		total,
		page,
		totalPages,
		hasPrevious,
		hasNext,
		busy = false,
		label,
		summary,
		pageSummary,
		previousLabel,
		nextLabel,
		onPrevious,
		onNext,
	}: {
		start: number;
		end: number;
		total: number;
		page: number;
		totalPages: number;
		hasPrevious: boolean;
		hasNext: boolean;
		busy?: boolean;
		label?: string;
		summary?: string;
		pageSummary?: string;
		previousLabel?: string;
		nextLabel?: string;
		onPrevious: () => void;
		onNext: () => void;
	} = $props();
	const i18n = getAdminI18n();
	const resolvedLabel = $derived(label ?? i18n.t("general:pagination"));
	const resolvedSummary = $derived(
		summary ?? `${i18n.formatNumber(start)}–${i18n.formatNumber(end)} / ${i18n.formatNumber(total)}`
	);
	const resolvedPageSummary = $derived(
		pageSummary ??
			i18n.t("collections:pageSummary", {
				page: i18n.formatNumber(page),
				pages: i18n.formatNumber(Math.max(totalPages, 1)),
			})
	);
	const resolvedPreviousLabel = $derived(previousLabel ?? i18n.t("general:previous"));
	const resolvedNextLabel = $derived(nextLabel ?? i18n.t("general:next"));
</script>

<nav
	data-slot="pagination"
	class="flex items-center justify-between gap-4 py-3"
	aria-label={resolvedLabel}
>
	<p class="font-mono text-[10.5px] text-foreground-faint">{resolvedSummary}</p>
	{#if totalPages > 1}
		<div class="flex items-center gap-2">
			<Button
				variant="outline"
				size="icon-sm"
				disabled={!hasPrevious || busy}
				onclick={onPrevious}
				aria-label={resolvedPreviousLabel}
			>
				<ChevronLeftIcon class="size-3.5 rtl:rotate-180" />
			</Button>
			<span class="font-mono min-w-12 text-center text-[10px] text-foreground-faint">
				{resolvedPageSummary}
			</span>
			<Button
				variant="outline"
				size="icon-sm"
				disabled={!hasNext || busy}
				onclick={onNext}
				aria-label={resolvedNextLabel}
			>
				<ChevronRightIcon class="size-3.5 rtl:rotate-180" />
			</Button>
		</div>
	{/if}
</nav>
