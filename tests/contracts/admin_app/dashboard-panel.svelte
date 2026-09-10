<script lang="ts">
	import type { AdminDashboardPanelProps } from "@riducms/plugin";

	let { manifest, user, i18n }: AdminDashboardPanelProps = $props();
</script>

<section
	class="rounded-[10px] border border-primary/20 bg-primary/[0.035] p-5"
	aria-labelledby="editorial-dashboard-title"
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
</section>
