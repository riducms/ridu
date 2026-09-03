<script lang="ts">
	import type { AdminProviderProps } from "@riducms/plugin";

	let { defaultView }: AdminProviderProps = $props();
</script>

<span class="sr-only">Plugin provider boundary</span>
{@render defaultView()}
