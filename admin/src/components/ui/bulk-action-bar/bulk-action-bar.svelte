<script lang="ts">
	import XIcon from "~icons/lucide/x";
	import type { Snippet } from "svelte";
	import { getAdminI18n } from "@riducms/plugin";

	import { Button } from "@admin/components/ui/button";

	let {
		count,
		onClear,
		label,
		selectionLabel,
		clearLabel,
		children,
	}: {
		count: number;
		onClear: () => void;
		label?: string;
		selectionLabel?: string;
		clearLabel?: string;
		children?: Snippet;
	} = $props();
	const i18n = getAdminI18n();
	const resolvedLabel = $derived(label ?? i18n.t("general:bulkActions"));
	const resolvedSelectionLabel = $derived(selectionLabel ?? i18n.t("general:selected", { count }));
	const resolvedClearLabel = $derived(clearLabel ?? i18n.t("general:clearSelection"));
</script>

{#if count > 0}
	<div
		data-slot="bulk-action-bar"
		class="ridu-popover-enter fixed bottom-6 left-1/2 z-40 flex max-w-[calc(100vw-2rem)] -translate-x-1/2 items-center gap-2 rounded-[4px] border border-control-border bg-popover px-3 py-2 text-popover-foreground shadow-[var(--shadow-popover)]"
		role="region"
		aria-label={resolvedLabel}
	>
		<span
			class="whitespace-nowrap px-2 text-[13.5px] font-medium text-foreground"
			role="status"
			aria-live="polite"
			aria-atomic="true"
		>
			{resolvedSelectionLabel}
		</span>
		<span class="h-5 w-px bg-control-border" aria-hidden="true"></span>
		{@render children?.()}
		<Button variant="ghost" size="icon-sm" onclick={onClear} aria-label={resolvedClearLabel}>
			<XIcon class="size-3.5" />
		</Button>
	</div>
{/if}
