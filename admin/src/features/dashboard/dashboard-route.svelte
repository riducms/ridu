<script lang="ts">
	import { Link } from "@hvniel/svelte-router";
	import ArrowRightIcon from "~icons/lucide/arrow-right";
	import PlusIcon from "~icons/lucide/plus";
	import { TooltipContent, TooltipRoot, TooltipTrigger } from "@riducms/ui";

	import { collectionPath, createDocumentPath, globalPath } from "@admin/core/routing/admin-paths";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import { groupAdminResources } from "@admin/features/navigation/admin-resource-groups";

	const runtime = getAdminRuntime();
	const manifest = $derived(runtime.manifest);
	const resourceGroups = $derived(
		groupAdminResources(runtime.visibleCollections, runtime.visibleGlobals, {
			collections: runtime.i18n.t("navigation:collections"),
			globals: runtime.i18n.t("navigation:globals"),
		})
	);
	const replacement = $derived(
		runtime.dashboardPanels.find((panel) => panel.position === "replace")
	);
	const beforePanels = $derived(
		runtime.dashboardPanels.filter((panel) => panel.position === "before")
	);
	const afterPanels = $derived(
		runtime.dashboardPanels.filter(
			(panel) => panel.position === undefined || panel.position === "after"
		)
	);
</script>

{#if manifest !== undefined && replacement !== undefined}
	<replacement.component {manifest} user={runtime.session?.user} i18n={runtime.i18n} />
{:else}<section class="w-full px-5 pb-7 sm:px-8 sm:pb-9 lg:px-15">
		{#if manifest !== undefined && beforePanels.length > 0}
			<div class="grid gap-3" aria-label={runtime.i18n.t("dashboard:extensions")}>
				{#each beforePanels as panel (panel.key)}
					<panel.component {manifest} user={runtime.session?.user} i18n={runtime.i18n} />
				{/each}
			</div>
		{/if}

		<div class="mt-9 grid gap-9">
			{#each resourceGroups as group (group.key)}
				{const groupLabel = $derived(runtime.i18n.text(group.label, group.translations))}
				<section aria-label={groupLabel}>
					<div class="mb-3 flex items-end justify-between gap-4">
						<h2 class="text-[18px] font-medium text-foreground-strong">
							{groupLabel}
						</h2>
					</div>
					<div class="grid grid-cols-[repeat(auto-fill,minmax(min(260px,100%),260px))] gap-2.5">
						{#each group.collections as collection (collection.id)}
							{const pluralLabel = $derived(
								runtime.i18n.text(collection.labels.plural, collection.labels.pluralTranslations)
							)}
							{const singularLabel = $derived(
								runtime.i18n.text(
									collection.labels.singular,
									collection.labels.singularTranslations
								)
							)}
							<article
								class="group relative flex min-h-20 items-center gap-3 rounded-[3px] bg-control px-4 py-3.5 transition-colors hover:bg-control-hover"
							>
								<Link
									class="absolute inset-0 rounded-[3px] focus-visible:outline-2 focus-visible:outline-ring focus-visible:outline-offset-2"
									to={collectionPath(collection.slug)}
									aria-label={runtime.i18n.t("dashboard:open", {
										label: pluralLabel,
									})}
								/>
								<h3 class="min-w-0 flex-1 text-[14px] font-medium text-foreground-strong">
									{pluralLabel}
								</h3>
								{#if runtime.collectionOperations[collection.slug]?.create}
									<TooltipRoot>
										<TooltipTrigger>
											{#snippet child({ props })}
												<Link
													{...props}
													class="relative z-1 grid size-8 shrink-0 place-items-center rounded-[3px] text-foreground-muted hover:bg-background-layer hover:text-foreground-strong"
													to={createDocumentPath(collection.slug)}
													aria-label={runtime.i18n.t("dashboard:create", {
														label: singularLabel.toLocaleLowerCase(runtime.i18n.language),
													})}
												>
													<PlusIcon class="size-4" />
												</Link>
											{/snippet}
										</TooltipTrigger>
										<TooltipContent>
											{runtime.i18n.t("dashboard:create", {
												label: singularLabel,
											})}
										</TooltipContent>
									</TooltipRoot>
								{:else}
									<ArrowRightIcon
										class="size-4 text-foreground-faint rtl:rotate-180"
										aria-hidden="true"
									/>
								{/if}
							</article>
						{/each}
						{#each group.globals as global (global.id)}
							{const globalLabel = $derived(
								runtime.i18n.text(global.labels.singular, global.labels.singularTranslations)
							)}
							<article
								class="group relative flex min-h-20 items-center gap-3 rounded-[3px] bg-control px-4 py-3.5 transition-colors hover:bg-control-hover"
							>
								<Link
									class="absolute inset-0 rounded-[3px] focus-visible:outline-2 focus-visible:outline-ring focus-visible:outline-offset-2"
									to={globalPath(global.slug)}
									aria-label={runtime.i18n.t("dashboard:open", {
										label: globalLabel,
									})}
								/>
								<h3 class="min-w-0 flex-1 text-[14px] font-medium text-foreground-strong">
									{globalLabel}
								</h3>
								<ArrowRightIcon
									class="size-4 text-foreground-faint rtl:rotate-180"
									aria-hidden="true"
								/>
							</article>
						{/each}
					</div>
				</section>
			{/each}
		</div>

		{#if manifest !== undefined && afterPanels.length > 0}
			<div class="mt-8 grid gap-3" aria-label={runtime.i18n.t("dashboard:extensions")}>
				{#each afterPanels as panel (panel.key)}
					<panel.component {manifest} user={runtime.session?.user} i18n={runtime.i18n} />
				{/each}
			</div>
		{/if}
	</section>{/if}
