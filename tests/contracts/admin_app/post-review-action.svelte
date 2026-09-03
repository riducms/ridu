<script lang="ts">
	import type { AdminDocumentExtensionProps } from "@riducms/plugin";

	let { document, host }: AdminDocumentExtensionProps = $props();

	function requestReview() {
		host.notify("success", "Review requested", `Editorial review requested for ${document.id}.`);
	}
</script>

<button
	type="button"
	class="h-9 rounded-lg border border-white/11 px-3 text-[12.5px] font-medium text-foreground-muted hover:bg-white/4 hover:text-foreground"
	onclick={requestReview}
>
	Request review
</button>
