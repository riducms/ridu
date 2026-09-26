<script lang="ts">
	import type { PluginFieldProps } from "@riducms/plugin";

	import { GenerationController } from "@plugin-seo/generation-controller.svelte";
	import "@plugin-seo/seo.scss";
	import type { PreviewConfig } from "@plugin-seo/seo-config";

	let {
		field: binding,
		form,
		config,
		i18n,
		authoring,
	}: PluginFieldProps<undefined, PreviewConfig, "ui"> = $props();
	const field = $derived(binding.schema);

	const title = $derived(String(form.get(config.titlePath) ?? ""));
	const description = $derived(String(form.get(config.descriptionPath) ?? ""));
	let href = $state("");
	const generation = new GenerationController();

	$effect(() => {
		href = "";
		if (!config.generate) return;
		form.snapshot?.();
		form.contentLocale;
		form.resource;
		const timeout = setTimeout(async () => {
			const result = await generation.run(authoring, form, "generate-url");
			if (result !== undefined) href = result;
		}, 250);
		return () => {
			clearTimeout(timeout);
			generation.cancel();
		};
	});
</script>

<section
	class="ridu-seo-preview"
	aria-labelledby={`${field.id}-heading`}
	data-seo-preview
	data-field-path={field.path}
>
	<div>
		<h3 id={`${field.id}-heading`} class="ridu-seo-preview-heading">
			{i18n.t("plugin.seo:preview")}
		</h3>
		<p class="ridu-seo-preview-description">
			{i18n.t("plugin.seo:previewDescription")}
		</p>
	</div>
	<div class="ridu-seo-preview-card" aria-busy={generation.status === "pending"}>
		<p class="ridu-seo-preview-url">{href || "https://..."}</p>
		<p class="ridu-seo-preview-title">{title}</p>
		<p class="ridu-seo-preview-copy">{description}</p>
	</div>
	{#if generation.status === "error"}
		<p class="ridu-seo-error" role="alert">
			{i18n.t("plugin.seo:generationFailed")}
		</p>
	{/if}
</section>
