<script lang="ts">
	import type { PluginFieldProps } from "@riducms/plugin";
	import { Button, FieldFrame, Textarea, fieldControlARIA } from "@riducms/ui";

	import { GenerationController } from "@plugin-seo/generation-controller.svelte";
	import LengthIndicator from "@plugin-seo/length-indicator.svelte";
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
	<FieldFrame
		controlID={field.id}
		label={field.admin.label}
		required={field.required}
		readOnly={field.admin.readOnly}
		description={field.admin.description}
		errors={issues.map((issue) => issue.message)}
	>
		<div class="-mt-1 flex flex-wrap items-center gap-x-1 text-[11.5px] text-foreground-muted">
			<span>{i18n.t("plugin.seo:lengthTipDescription", { minLength, maxLength })}</span>
			<a
				class="text-primary underline-offset-2 hover:underline focus-visible:outline-2 focus-visible:outline-ring/70"
				href="https://developers.google.com/search/docs/appearance/snippet#meta-descriptions"
				target="_blank"
				rel="noopener noreferrer"
			>
				{i18n.t("plugin.seo:bestPractices")}
			</a>
			{#if config.generate}
				<span aria-hidden="true">·</span>
				<Button
					variant="link"
					size="xs"
					class="h-auto px-0"
					disabled={editingBlocked || generation.status === "pending"}
					aria-busy={generation.status === "pending"}
					onclick={generate}
				>
					{i18n.t("plugin.seo:autoGenerate")}
				</Button>
			{/if}
		</div>
		<Textarea
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
		{#if generation.status === "error"}<p class="text-[12px] text-destructive" role="alert">
				{i18n.t("plugin.seo:generationFailed")}
			</p>{/if}
	</FieldFrame>
</div>
