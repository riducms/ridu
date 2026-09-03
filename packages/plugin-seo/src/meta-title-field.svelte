<script lang="ts">
	import type { FieldComponentProps } from "@riducms/plugin";
	import { Button, FieldFrame, Input } from "@riducms/ui";

	import { GenerationController } from "@plugin-seo/generation-controller.svelte";
	import LengthIndicator from "@plugin-seo/length-indicator.svelte";
	import { lengthConfig } from "@plugin-seo/seo-config";

	let { field, form, i18n, authoring }: FieldComponentProps = $props();
	const config = $derived(lengthConfig(field));
	const minLength = $derived(field.text?.minLength ?? config.minLength);
	const maxLength = $derived(field.text?.maxLength ?? config.maxLength);
	const value = $derived(String(form.get(field.path) ?? ""));
	const issues = $derived(form.issuesFor(field.path));
	const hasMessage = $derived(issues.length > 0 || field.admin.description !== undefined);
	const generation = new GenerationController();

	$effect(() => form.register(field.path));
	$effect(() => () => generation.cancel());

	async function generate() {
		const result = await generation.run(authoring, form, "generate-title");
		if (result !== undefined) form.set(field.path, result);
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
			<span>{i18n.t("plugin.seo:lengthTipTitle", { minLength, maxLength })}</span>
			<a
				class="text-primary underline-offset-2 hover:underline focus-visible:outline-2 focus-visible:outline-ring/70"
				href="https://developers.google.com/search/docs/appearance/title-link#page-titles"
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
					disabled={field.admin.readOnly || generation.status === "pending"}
					aria-busy={generation.status === "pending"}
					onclick={generate}
				>
					{i18n.t("plugin.seo:autoGenerate")}
				</Button>
			{/if}
		</div>
		<Input
			id={field.id}
			name={field.path}
			required={field.required}
			readonly={field.admin.readOnly}
			minlength={field.text?.minLength}
			maxlength={field.text?.maxLength}
			aria-invalid={issues.length > 0}
			aria-describedby={hasMessage ? `${field.id}-message` : undefined}
			aria-errormessage={issues.length > 0 ? `${field.id}-message` : undefined}
			{value}
			oninput={(event) => form.set(field.path, event.currentTarget.value)}
		/>
		<LengthIndicator text={value} {minLength} {maxLength} {i18n} />
		{#if generation.status === "error"}<p class="text-[12px] text-destructive" role="alert">
				{i18n.t("plugin.seo:generationFailed")}
			</p>{/if}
	</FieldFrame>
</div>
