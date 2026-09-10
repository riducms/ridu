<script lang="ts">
	import type { PluginFieldProps } from "@riducms/plugin";

	import { GenerationController } from "@plugin-seo/generation-controller.svelte";
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
	class="grid gap-2"
	aria-labelledby={`${field.id}-heading`}
	data-seo-preview
	data-field-path={field.path}
>
	<div>
		<h3 id={`${field.id}-heading`} class="text-[13px] font-semibold text-foreground-strong">
			{i18n.t("plugin.seo:preview")}
		</h3>
		<p class="mt-0.5 text-[12px] text-foreground-muted">
			{i18n.t("plugin.seo:previewDescription")}
		</p>
	</div>
	<div
		class="w-full max-w-[600px] overflow-hidden rounded-[5px] border border-control-border bg-background p-4 shadow-sm"
		aria-busy={generation.status === "pending"}
	>
		<p class="truncate text-[12px] text-success">{href || "https://..."}</p>
		<p class="mt-1 truncate text-[18px] leading-6 text-primary">{title}</p>
		<p class="mt-0.5 line-clamp-2 text-[13px] leading-5 text-foreground-muted">{description}</p>
	</div>
	{#if generation.status === "error"}<p class="text-[12px] text-destructive" role="alert">
			{i18n.t("plugin.seo:generationFailed")}
		</p>{/if}
</section>
