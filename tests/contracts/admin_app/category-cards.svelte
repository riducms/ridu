<script lang="ts">
	import type { AdminListResultsRendererProps } from "@riducms/plugin";
	import { Button } from "@riducms/ui";
	import { Link } from "@riducms/admin/routing";

	let { list, i18n }: AdminListResultsRendererProps = $props();
</script>

<div class="grid gap-4 py-4 sm:grid-cols-2 lg:grid-cols-3" data-testid="category-cards">
	{#each list.documents as document (document.id)}
		<article class="rounded-lg border bg-card p-5" data-category-id={document.id}>
			<div class="mb-4 flex items-center gap-3">
				<Button
					variant="outline"
					size="sm"
					aria-pressed={list.selectedIDs.has(document.id)}
					disabled={!list.selectionEnabled}
					onclick={() => list.toggleDocument(document.id, !list.selectedIDs.has(document.id))}
					aria-label={i18n.t("collections:selectDocument", { title: list.title(document) })}
				>
					{list.selectedIDs.has(document.id) ? "✓" : "+"}
				</Button>
				<Link
					to={list.documentHref(document)}
					class="flex-1 font-medium hover:underline focus-visible:underline"
				>
					{list.title(document)}
				</Link>
			</div>
			<p class="text-sm text-foreground-muted">{String(document.description ?? "")}</p>
		</article>
	{/each}
</div>
