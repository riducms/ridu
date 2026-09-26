<script lang="ts">
	import type { AdminDocumentExtensionProps } from "@riducms/plugin";
	import { Button } from "@riducms/ui";

	let { document, host }: AdminDocumentExtensionProps = $props();

	function requestReview() {
		host.notify("success", "Review requested", `Editorial review requested for ${document.id}.`);
	}
</script>

<Button variant="outline" size="lg" onclick={requestReview}>Request review</Button>
