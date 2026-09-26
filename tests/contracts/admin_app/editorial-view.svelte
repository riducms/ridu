<script lang="ts">
	import type { AdminCoreViewProps, AdminExtensionProps, AdminLoaderProps } from "@riducms/plugin";
	import type { AdminLoaderData } from "./loaders.generated";
	import { Link } from "@riducms/admin/routing";
	import { Button } from "@riducms/ui";
	let {
		data,
		refresh,
		refreshing,
		global,
		documentID,
	}: AdminExtensionProps &
		AdminLoaderProps<AdminLoaderData["editorial-view"]> &
		Pick<AdminCoreViewProps, "global" | "documentID"> = $props();
</script>

<section class="mx-auto w-full max-w-4xl space-y-5 p-5 sm:p-8" data-testid="editorial-view">
	<h1 class="text-2xl font-semibold">Editorial workspace</h1>
	<p data-testid="view-path">{data.pathname}</p>
	<p>Locale: {data.locale} · Search: {data.search || "All"}</p>
	<nav aria-label="Editorial workspace" class="flex flex-wrap gap-4">
		<Link to="/editorial-report?locale=en">Report</Link>
		<Link to="/collections/loader-records?locale=en">Records</Link>
		<Link to="/collections/loader-records/create?locale=en">New record</Link>
		<Link to="/globals/loader-summary?locale=en">Summary</Link>
		<Link to="?locale=fr&q=Alpha">French Alpha</Link>
	</nav>
	<div class="grid gap-3 sm:grid-cols-2">
		{#each data.documents as document (document.id)}
			<article class="rounded-lg border p-5">
				<h2 class="text-lg">{document.title}</h2>
				{#if global === undefined && document.id !== documentID}
					<Link
						class="text-primary underline"
						to={`/collections/loader-records/${encodeURIComponent(document.id)}?locale=${data.locale}`}
					>
						Open {document.title}
					</Link>
				{/if}
			</article>
		{:else}
			<p>No matching records.</p>
		{/each}
	</div>
	<Button variant="outline" disabled={refreshing} onclick={refresh}>Refresh workspace</Button>
</section>
