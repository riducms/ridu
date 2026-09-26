<script lang="ts">
	import type { AdminI18n } from "@riducms/plugin";

	import { lengthState } from "@plugin-seo/length-indicator";
	import "@plugin-seo/seo.scss";

	let {
		text,
		minLength,
		maxLength,
		i18n,
	}: { text: string; minLength: number; maxLength: number; i18n: AdminI18n } = $props();
	const state = $derived(lengthState(text, minLength, maxLength));
	const suffix = $derived(
		state.status === "missing" || state.status === "tooShort" || state.status === "almostThere"
			? i18n.t("plugin.seo:charactersToGo", { characters: state.remaining })
			: state.status === "tooLong"
				? i18n.t("plugin.seo:charactersTooMany", { characters: state.remaining })
				: i18n.t("plugin.seo:charactersLeft", { characters: state.remaining })
	);
</script>

<div class="ridu-seo-length" data-length-status={state.status}>
	<span class="ridu-seo-pill" data-status={state.status}>
		{i18n.t(`plugin.seo:${state.status}`)}
	</span>
	<small class="ridu-seo-length-copy">
		{i18n.t("plugin.seo:characterCount", {
			current: state.length,
			minLength,
			maxLength,
		})}{suffix}
	</small>
	<span class="ridu-seo-progress" aria-hidden="true">
		<span
			class="ridu-seo-progress-value"
			data-status={state.status}
			style:transform="scaleX({state.progress})"
		></span>
	</span>
</div>
