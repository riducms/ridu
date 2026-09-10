<script lang="ts">
	import type { AdminCoreViewProps } from '@riducms/plugin';

	let { defaultView }: AdminCoreViewProps = $props();
</script>

<aside aria-label="Editorial reminder">
	Check image credits before publishing an article.
</aside>
<!-- Keep the built-in editor below the reminder. -->
{@render defaultView()}
