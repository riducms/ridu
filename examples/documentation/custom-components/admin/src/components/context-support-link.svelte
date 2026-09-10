<script lang="ts">
	import type { AdminNavigationComponentProps } from '@riducms/plugin';
	import { getSupport } from '../support-context';

	let { user }: AdminNavigationComponentProps = $props();
	// Read the value supplied by SupportProvider above this component.
	const support = getSupport();
</script>

{#if user}
	<a href={`mailto:${support.email}`}>Contact the editorial team</a>
{/if}
