<script lang="ts">
	import type { FieldComponentProps } from "@riducms/plugin";
	import CheckIcon from "~icons/lucide/check";
	import XIcon from "~icons/lucide/x";

	import { overviewConfig } from "@plugin-seo/seo-config";

	let { field, form, i18n }: FieldComponentProps = $props();
	const config = $derived(overviewConfig(field));
	const title = $derived(form.get(config.titlePath));
	const description = $derived(form.get(config.descriptionPath));
	const image = $derived(form.get(config.imagePath));
	const checks = $derived([
		{
			key: "title",
			label: i18n.t("plugin.seo:title"),
			passing:
				typeof title === "string" &&
				title.length >= config.titleMin &&
				title.length <= config.titleMax,
		},
		{
			key: "description",
			label: i18n.t("plugin.seo:description"),
			passing:
				typeof description === "string" &&
				description.length >= config.descriptionMin &&
				description.length <= config.descriptionMax,
		},
		{ key: "image", label: i18n.t("plugin.seo:image"), passing: Boolean(image) },
	]);
	const passing = $derived(checks.filter((check) => check.passing).length);
</script>

<section
	class="rounded-[4px] border border-control-border bg-control/35 p-3.5"
	aria-labelledby={`${field.id}-heading`}
	data-seo-overview
	data-field-path={field.path}
>
	<div class="flex flex-wrap items-center justify-between gap-2">
		<h3 id={`${field.id}-heading`} class="text-[13px] font-semibold text-foreground-strong">
			{field.admin.label}
		</h3>
		<p class="font-mono text-[11px] text-foreground-muted" aria-live="polite">
			{i18n.t("plugin.seo:checksPassing", { current: passing, max: checks.length })}
		</p>
	</div>
	<ul class="mt-2 flex flex-wrap gap-1.5">
		{#each checks as check (check.key)}
			<li
				class={[
					"inline-flex items-center gap-1 rounded-full px-2 py-1 text-[11px]",
					check.passing ? "bg-success/12 text-success" : "bg-destructive/10 text-destructive",
				]}
				data-seo-check={check.key}
				data-passing={check.passing}
			>
				{#if check.passing}<CheckIcon class="size-3" aria-hidden="true" />{:else}<XIcon
						class="size-3"
						aria-hidden="true"
					/>{/if}
				<span>{check.label}</span>
			</li>
		{/each}
	</ul>
</section>
