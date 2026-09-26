<script lang="ts">
	import type { AdminDashboardPanelProps, AdminLoaderProps } from "@riducms/plugin";
	import type { AdminLoaderData } from "./loaders.generated";
	import { Button } from "@riducms/ui";
	import { Link } from "@riducms/admin/routing";
	import { adminLoaders } from "./loaders.generated";

	let {
		manifest,
		user,
		i18n,
		data,
		refresh,
		refreshing,
	}: AdminDashboardPanelProps & AdminLoaderProps<AdminLoaderData["editorial-dashboard"]> = $props();
</script>

<section
	class="rounded-[10px] border border-primary/20 bg-primary/[0.035] p-5"
	aria-labelledby="editorial-dashboard-title"
	data-testid="editorial-dashboard"
>
	<p class="font-mono text-[9.5px] tracking-[0.14em] text-primary uppercase">
		{i18n.t("app:dashboard.eyebrow")}
	</p>
	<h2 id="editorial-dashboard-title" class="font-serif mt-2 text-[24px] text-foreground">
		{i18n.t("app:dashboard.title")}
	</h2>
	<p class="mt-2 text-[13px] text-foreground-muted">
		{i18n.t("app:dashboard.summary", {
			count: manifest.collections.length,
			name: String(user?.name ?? i18n.t("app:dashboard.team")),
		})}
	</p>
	<div class="mt-4 flex flex-wrap items-center gap-4">
		<p data-testid="dashboard-post-count">Posts: {data.posts}</p>
		<p data-testid="dashboard-category-count">Categories: {data.categories}</p>
		{#if data.search}
			<p>Search: {data.search}</p>
		{/if}
		<Button variant="outline" disabled={refreshing} onclick={refresh}>Refresh dashboard</Button>
		<Link to={`?${adminLoaders["editorial-dashboard"].query({ q: "Welcome" })}`}>
			Welcome posts
		</Link>
	</div>
</section>
