<script lang="ts">
	import type { AdminLoaderProps } from '@riducms/plugin';
	import { Link } from '@riducms/admin/routing';
	import { Button } from '@riducms/ui';
	import {
		adminLoaders,
		type AdminLoaderData
	} from '../../../generated/ridu.generated';

	let {
		data,
		refresh,
		refreshing
	}: AdminLoaderProps<AdminLoaderData['post-summary']> = $props();

	function filterLink(q: string) {
		const query = new URLSearchParams(
			adminLoaders['post-summary'].query({ q })
		);
		// Keep the selected content language when changing the search.
		if (data.locale) query.set('locale', data.locale);
		return `?${query}`;
	}
</script>

<section class="space-y-4 p-6" aria-label="Post summary">
	<h2 class="text-xl font-semibold">Post summary</h2>
	<p>{data.total} posts match {data.search || 'all titles'}.</p>
	<nav aria-label="Filter post summary" class="flex gap-4">
		<Link to={filterLink('')}>All posts</Link>
		<Link to={filterLink('Welcome')}>Welcome posts</Link>
	</nav>
	<Button variant="outline" disabled={refreshing} onclick={refresh}>
		{refreshing ? 'Refreshing…' : 'Refresh count'}
	</Button>
</section>
