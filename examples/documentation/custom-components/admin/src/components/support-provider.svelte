<script lang="ts">
	import type { AdminProviderProps } from '@riducms/plugin';
	import { setSupport } from '../support-context';

	let { defaultView }: AdminProviderProps = $props();
	// Set context during setup, before child components read it.
	setSupport({ email: 'editors@example.com' });
</script>

{@render defaultView()}
