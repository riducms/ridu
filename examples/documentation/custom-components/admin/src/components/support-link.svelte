<script lang="ts">
	import type { AdminNavigationComponentProps } from '@riducms/plugin';

	let { user }: AdminNavigationComponentProps = $props();
</script>

{#if user}
	<a href="mailto:editors@example.com">Contact the editorial team</a>
{/if}
