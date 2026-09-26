<script lang="ts">
	import CheckIcon from "~icons/lucide/check";
	import CopyIcon from "~icons/lucide/copy";
	import Maximize2Icon from "~icons/lucide/maximize-2";
	import Minimize2Icon from "~icons/lucide/minimize-2";
	import { getAdminI18n } from "@riducms/plugin";
	import JsonTreeNode from "@admin/components/json-tree/json-tree-node.svelte";
	import { CopyFeedback } from "@admin/core/clipboard/copy-feedback.svelte";
	import "@admin/components/json-tree/json-viewer.scss";

	let {
		value,
		expanded = $bindable<boolean>(),
		class: className,
	}: {
		value: unknown;
		expanded?: boolean;
		class?: string;
	} = $props();

	const i18n = getAdminI18n();
	const copyFeedback = new CopyFeedback();
	const source = $derived(JSON.stringify(value, null, 2) ?? "undefined");
	const copyResult = $derived(
		copyFeedback.result?.source === source ? copyFeedback.result : undefined
	);
</script>

<div class={["ridu-json-viewer", className]}>
	<div class="ridu-json-viewer__toolbar">
		<div class="ridu-json-viewer__actions">
			<button
				type="button"
				class="ridu-json-viewer__copy"
				onclick={() => copyFeedback.copy(source)}
				aria-label={i18n.t("general:copyJSON")}
				title={copyResult?.copied === true
					? i18n.t("general:copied")
					: copyResult?.copied === false
						? i18n.t("general:copyUnavailable")
						: i18n.t("general:copyJSON")}
			>
				{#if copyResult?.copied}
					<CheckIcon />
				{:else}
					<CopyIcon />
				{/if}
			</button>
			{#if expanded !== undefined}
				<button
					type="button"
					class="ridu-json-viewer__expand"
					aria-label={i18n.t(expanded ? "general:exitExpandedJSONView" : "general:expandJSONView")}
					title={i18n.t(expanded ? "general:exitExpandedJSONView" : "general:expandJSONView")}
					aria-pressed={expanded}
					onclick={() => (expanded = !expanded)}
				>
					{#if expanded}
						<Minimize2Icon />
					{:else}
						<Maximize2Icon />
					{/if}
				</button>
			{/if}
		</div>
		<span class="sr-only" role="status">
			{copyResult ? i18n.t(copyResult.copied ? "general:copied" : "general:copyUnavailable") : ""}
		</span>
	</div>

	<section class="ridu-json-viewer__data" aria-label={i18n.t("general:jsonData")} dir="ltr">
		<ul class="ridu-json-viewer__tree"><JsonTreeNode {value} root /></ul>
	</section>
</div>
