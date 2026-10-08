<script lang="ts">
	import type { PluginFieldProps } from "@riducms/plugin";
	import "#lib/seo.scss";

	import type { OverviewConfig } from "#lib/seo-config.js";

	let {
		field: binding,
		form,
		config,
		i18n,
	}: PluginFieldProps<undefined, OverviewConfig, "ui"> = $props();
	const field = $derived(binding.schema);

	const title = $derived(form.get(config.titlePath));
	const description = $derived(form.get(config.descriptionPath));
	const image = $derived(form.get(config.imagePath));
	const checks = $derived([
		typeof title === "string" && title.length >= config.titleMin && title.length <= config.titleMax,
		typeof description === "string" &&
			description.length >= config.descriptionMin &&
			description.length <= config.descriptionMax,
		Boolean(image),
	]);
	const passing = $derived(checks.filter(Boolean).length);
</script>

<section
	class="ridu-seo-overview"
	aria-label={field.admin.label}
	data-seo-overview
	data-field-path={field.path}
>
	<p aria-live="polite">
		{i18n.t("plugin.seo:checksPassing", { current: passing, max: checks.length })}
	</p>
</section>
