<script lang="ts">
	import type { AdminCoreViewProps } from "@riducms/plugin";

	let { defaultView, surface }: AdminCoreViewProps = $props();
</script>

<p
	class="fixed right-4 bottom-4 z-50 rounded-full border border-primary/20 bg-background-surface px-3 py-1.5 text-[10.5px] text-primary"
>
	Plugin {surface} view
</p>
{@render defaultView()}
