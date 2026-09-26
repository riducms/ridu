<script lang="ts">
	import { Link } from "@hvniel/svelte-router";
	import "@admin/features/dashboard/dashboard.scss";
	import PlusIcon from "~icons/lucide/plus";
	import { TooltipContent, TooltipRoot, TooltipTrigger } from "@riducms/ui";

	import { collectionPath, createDocumentPath, globalPath } from "@admin/core/routing/admin-paths";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import { groupAdminResources } from "@admin/features/navigation/admin-resource-groups";
	import AdminViewHost from "@admin/core/loaders/admin-view-host.svelte";

	const runtime = getAdminRuntime();
	const extensions = runtime.config.extensions;

	const manifest = $derived(runtime.manifest);
	const resourceGroups = $derived(
		groupAdminResources(runtime.visibleCollections, runtime.visibleGlobals, {
			collections: runtime.i18n.t("navigation:collections"),
			globals: runtime.i18n.t("navigation:globals"),
		})
	);

	const replacement = extensions.dashboardPanels.find((panel) => panel.position === "replace");
	const beforePanels = extensions.dashboardPanels.filter((panel) => panel.position === "before");
	const afterPanels = extensions.dashboardPanels.filter(
		(panel) => panel.position === undefined || panel.position === "after"
	);
</script>

{#snippet resourceCard(
	label: string,
	path: string,
	create: { label: string; path: string } | undefined
)}
	<article class="ridu-dashboard__card">
		<Link
			class="ridu-dashboard__card-link"
			to={path}
			aria-label={runtime.i18n.t("dashboard:open", { label })}
		/>
		<h3 class="ridu-dashboard__card-title">
			{label}
		</h3>
		{#if create !== undefined}
			<TooltipRoot>
				<TooltipTrigger>
					{#snippet child({ props })}
						<Link
							{...props}
							class="ridu-dashboard__create"
							to={create.path}
							aria-label={runtime.i18n.t("dashboard:create", {
								label: create.label.toLocaleLowerCase(runtime.i18n.language),
							})}
						>
							<PlusIcon aria-hidden="true" />
						</Link>
					{/snippet}
				</TooltipTrigger>
				<TooltipContent>
					{runtime.i18n.t("dashboard:create", { label: create.label })}
				</TooltipContent>
			</TooltipRoot>
		{/if}
	</article>
{/snippet}

{#if manifest !== undefined && replacement !== undefined}
	<AdminViewHost
		view={replacement}
		props={{ manifest, user: runtime.session?.user, i18n: runtime.i18n }}
	/>
{:else}
	<section class="ridu-dashboard">
		{#if manifest !== undefined && beforePanels.length > 0}
			<div class="ridu-dashboard__extensions" aria-label={runtime.i18n.t("dashboard:extensions")}>
				{#each beforePanels as panel (panel.key)}
					<AdminViewHost
						view={panel}
						props={{ manifest, user: runtime.session?.user, i18n: runtime.i18n }}
					/>
				{/each}
			</div>
		{/if}

		<div class="ridu-dashboard__groups">
			{#each resourceGroups as group (group.key)}
				{const groupLabel = $derived(runtime.i18n.text(group.label, group.translations))}
				<section class="ridu-dashboard__group" aria-label={groupLabel}>
					<h2 class="ridu-dashboard__heading">
						{groupLabel}
					</h2>
					<div class="ridu-dashboard__cards">
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
							{@render resourceCard(
								pluralLabel,
								collectionPath(collection.slug),
								runtime.collectionOperations[collection.slug]?.create
									? {
											label: singularLabel,
											path: createDocumentPath(collection.slug),
										}
									: undefined
							)}
						{/each}
						{#each group.globals as global (global.id)}
							{const globalLabel = $derived(
								runtime.i18n.text(global.labels.singular, global.labels.singularTranslations)
							)}
							{@render resourceCard(globalLabel, globalPath(global.slug), undefined)}
						{/each}
					</div>
				</section>
			{/each}
		</div>

		{#if manifest !== undefined && afterPanels.length > 0}
			<div class="ridu-dashboard__extensions" aria-label={runtime.i18n.t("dashboard:extensions")}>
				{#each afterPanels as panel (panel.key)}
					<AdminViewHost
						view={panel}
						props={{ manifest, user: runtime.session?.user, i18n: runtime.i18n }}
					/>
				{/each}
			</div>
		{/if}
	</section>
{/if}
