<script lang="ts">
	import type { AdminNavigationComponentProps } from "@riducms/plugin";

	let { manifest }: AdminNavigationComponentProps = $props();
</script>

<p class="mx-4 mb-3 rounded-md border border-primary/15 px-2.5 py-2 text-[10.5px] text-primary">
	{manifest.application.name} plugin shell
</p>
