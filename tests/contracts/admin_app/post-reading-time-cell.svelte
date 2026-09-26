<script lang="ts">
	import type { AdminListCellRendererProps } from "@riducms/plugin";

	let { value }: AdminListCellRendererProps = $props();
</script>

<span class="font-mono text-[11px] text-foreground-sub">
	{typeof value === "number" ? `${value} min` : "—"}
</span>
