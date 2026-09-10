<script lang="ts">
	import type { AdminLoginComponentProps } from '@riducms/plugin';

	let { defaultView }: AdminLoginComponentProps = $props();
</script>

<p>Use your work email to sign in to Acme Studio.</p>
<!-- Keep Ridu's sign-in form and authentication behavior. -->
{@render defaultView()}
