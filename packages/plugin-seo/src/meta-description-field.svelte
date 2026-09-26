<script lang="ts">
	import type { PluginFieldProps } from "@riducms/plugin";
	import { Button, FieldFrame, Textarea, fieldControlARIA } from "@riducms/ui";

	import { GenerationController } from "@plugin-seo/generation-controller.svelte";
	import LengthIndicator from "@plugin-seo/length-indicator.svelte";
	import "@plugin-seo/seo.scss";
	import type { LengthConfig } from "@plugin-seo/seo-config";

	let {
		field: binding,
		form,
		config,
		i18n,
		authoring,
	}: PluginFieldProps<string, LengthConfig, "textarea"> = $props();
	const field = $derived(binding.schema);
	const editingBlocked = $derived(binding.readOnly);

	const minLength = $derived(field.textarea?.minLength ?? config.minLength);
	const maxLength = $derived(field.textarea?.maxLength ?? config.maxLength);
	const value = $derived(String(binding.value ?? ""));
	const issues = $derived(binding.issues);
	const inputARIA = $derived(
		fieldControlARIA(field.id, field.admin.description !== undefined, issues.length > 0)
	);
	const generation = new GenerationController();

	$effect(() => () => generation.cancel());

	async function generate() {
		const result = await generation.run(authoring, form, "generate-description");
		if (result !== undefined) binding.set(result);
	}
</script>

<div data-field-path={field.path}>
	{#snippet headingAction()}
		<span class="ridu-seo-heading-separator" aria-hidden="true">—</span>
		<Button
			variant="link"
			size="xs"
			class="ridu-seo-generate"
			disabled={editingBlocked || generation.status === "pending"}
			aria-busy={generation.status === "pending"}
			onclick={generate}
		>
			{i18n.t("plugin.seo:autoGenerate")}
		</Button>
	{/snippet}
	<FieldFrame
		controlID={field.id}
		label={field.admin.label}
		required={field.required}
		readOnly={field.admin.readOnly}
		description={field.admin.description}
		errors={issues.map((issue) => issue.message)}
		class="ridu-seo-field"
		headingAction={config.generate ? headingAction : undefined}
	>
		<p class="ridu-seo-guidance">
			<span>{i18n.t("plugin.seo:lengthTipDescription", { minLength, maxLength })}</span>
			<a
				class="ridu-seo-guidance-link"
				href="https://developers.google.com/search/docs/appearance/snippet#meta-descriptions"
				target="_blank"
				rel="noopener noreferrer"
			>
				{i18n.t("plugin.seo:bestPractices")}
			</a>
			<span>.</span>
		</p>
		<Textarea
			class="ridu-seo-description"
			data-empty={value === ""}
			id={field.id}
			name={field.path}
			required={field.required}
			readonly={editingBlocked}
			minlength={field.textarea?.minLength}
			maxlength={field.textarea?.maxLength}
			aria-invalid={inputARIA["aria-invalid"]}
			aria-describedby={inputARIA["aria-describedby"]}
			aria-errormessage={inputARIA["aria-errormessage"]}
			{value}
			oninput={(event) => binding.set(event.currentTarget.value)}
		/>
		<LengthIndicator text={value} {minLength} {maxLength} {i18n} />
		{#if generation.status === "error"}
			<p class="ridu-seo-error" role="alert">
				{i18n.t("plugin.seo:generationFailed")}
			</p>
		{/if}
	</FieldFrame>
</div>
