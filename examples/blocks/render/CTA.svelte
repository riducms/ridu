<script lang="ts">
	import type { CTA } from "../generated/ridu.generated";
	let { block }: { block: CTA } = $props();
</script>

<aside>{block.label ?? ""}</aside>
