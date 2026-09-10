<script lang="ts">
	import type { AdminDashboardPanelProps } from '@riducms/plugin';

	let { user }: AdminDashboardPanelProps = $props();
</script>

<h2>Welcome to Acme Studio</h2>
{#if typeof user?.email === 'string'}
	<p>Signed in as {user.email}.</p>
{/if}
<p>
	Start with a draft. Ask the editorial team for a review before
	publishing.
</p>
