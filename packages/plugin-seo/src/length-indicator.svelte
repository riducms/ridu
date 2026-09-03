<script lang="ts">
	import type { AdminI18n } from "@riducms/plugin";

	import { lengthState } from "@plugin-seo/length-indicator";

	let {
		text,
		minLength,
		maxLength,
		i18n,
	}: { text: string; minLength: number; maxLength: number; i18n: AdminI18n } = $props();
	const state = $derived(lengthState(text, minLength, maxLength));
	const tone = $derived(
		state.status === "good" ? "success" : state.status === "almostThere" ? "warning" : "danger"
	);
	const suffix = $derived(
		state.status === "missing" || state.status === "tooShort" || state.status === "almostThere"
			? i18n.t("plugin.seo:charactersToGo", { characters: state.remaining })
			: state.status === "tooLong"
				? i18n.t("plugin.seo:charactersTooMany", { characters: state.remaining })
				: i18n.t("plugin.seo:charactersLeft", { characters: state.remaining })
	);
</script>

<div class="flex min-w-0 items-center gap-2.5" data-length-status={state.status}>
	<span
		class={[
			"shrink-0 rounded-full px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide",
			tone === "success" && "bg-success/12 text-success",
			tone === "warning" && "bg-warning/12 text-warning",
			tone === "danger" && "bg-destructive/10 text-destructive",
		]}
	>
		{i18n.t(`plugin.seo:${state.status}`)}
	</span>
	<small class="shrink-0 text-[11px] text-foreground-muted">
		{i18n.t("plugin.seo:characterCount", {
			current: state.length,
			minLength,
			maxLength,
		})}{suffix}
	</small>
	<span
		class="h-0.5 min-w-8 flex-1 overflow-hidden rounded-full bg-control-border"
		aria-hidden="true"
	>
		<span
			class={[
				"block h-full origin-left transition-transform duration-150 motion-reduce:transition-none",
				tone === "success" && "bg-success",
				tone === "warning" && "bg-warning",
				tone === "danger" && "bg-destructive",
			]}
			style:transform="scaleX({state.progress})"
		></span>
	</span>
</div>
