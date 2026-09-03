<script lang="ts">
	import type { AdminLoginComponentProps } from "@riducms/plugin";

	let { defaultView }: AdminLoginComponentProps = $props();
</script>

<p
	class="fixed top-4 left-1/2 z-10 -translate-x-1/2 rounded-full border border-primary/20 bg-background-surface px-3 py-1.5 text-[11px] text-primary"
>
	Custom login wrapper
</p>
{@render defaultView()}
