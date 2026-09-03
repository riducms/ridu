<script lang="ts">
	import type { AdminListCellProps } from "@riducms/plugin";

	let { value }: AdminListCellProps = $props();
</script>

<span class="font-mono text-[11px] text-foreground-sub">
	{typeof value === "number" ? `${value} min` : "—"}
</span>
