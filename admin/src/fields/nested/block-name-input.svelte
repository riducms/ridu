<script lang="ts">
	import type { SchemaField } from "@riducms/protocol";
	import { Input } from "@riducms/ui";
	import FieldMessages from "@admin/fields/field-messages.svelte";
	import LiveValidationFeedback from "@admin/fields/live-validation-feedback.svelte";
	import type { FormController } from "@admin/core/forms/form-controller.svelte";
	import { FieldEditorBinding } from "@admin/core/forms/field-editor-binding";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import { localizeSchemaField } from "@admin/core/i18n/localized-schema";
	import {
		contentLocaleLabel,
		withContentLocaleLabel,
	} from "@admin/fields/localized-field-presentation";
	import "@admin/fields/nested/nested-field.scss";

	let {
		schema,
		form,
		readOnly = false,
		disabled = false,
		onChange,
	}: {
		schema: SchemaField;
		form: FormController;
		readOnly?: boolean;
		disabled?: boolean;
		onChange?: (value: string) => void;
	} = $props();
	const runtime = getAdminRuntime();
	const presented = $derived(
		withContentLocaleLabel(
			localizeSchemaField(
				{
					...schema,
					admin: { ...schema.admin, readOnly: schema.admin.readOnly || readOnly || disabled },
				},
				runtime.i18n
			),
			contentLocaleLabel(runtime.contentLocales, form.contentLocale)
		)
	);
	// The keyed host owns one occurrence capability, including delayed callbacks.
	// svelte-ignore state_referenced_locally
	const binding = new FieldEditorBinding(
		form,
		() => presented,
		"text",
		() => runtime.manifestRevision,
		(value) => {
			if (onChange !== undefined) onChange(value ?? "");
			else form.set(binding.schema.path, value);
		}
	);
	$effect(() => binding.destroy);
	const current = $derived(binding.schema);
	const placeholder = $derived(runtime.i18n.t("documents:untitled"));
	const inherited = $derived(
		form.isInherited(current.path) ? form.localizationSource(current.path) : undefined
	);
	function keydown(event: KeyboardEvent) {
		if (event.key === "Enter" && !event.isComposing) event.preventDefault();
	}
	function focusout() {
		// Removing a keyed schema host can dispatch focusout after its derived
		// values are destroyed. The binding lease is the synchronous lifetime guard.
		if (!binding.stale) form.liveValidation.flush(binding.schema.path);
	}
</script>

{#if binding.visible}
	<div class="ridu-block-name" data-field-path={current.path} onfocusout={focusout}>
		<FieldMessages
			controlID={current.id}
			description={current.admin.description}
			issues={binding.issues}
		>
			<label class="ridu-block-name__label" for={current.id}>{current.admin.label}</label>
			<div class="ridu-block-name__control" data-value={binding.value ?? ""}>
				<Input
					{...binding.inputProps}
					value={binding.value ?? ""}
					{placeholder}
					readonly={binding.readOnly}
					{disabled}
					minlength={current.text?.minLength}
					maxlength={current.text?.maxLength}
					class="ridu-block-name__input"
					oninput={(event) => binding.set(event.currentTarget.value)}
					onkeydown={keydown}
				/>
			</div>
		</FieldMessages>
		<LiveValidationFeedback
			feedback={binding.liveValidation}
			i18n={runtime.i18n}
			path={current.path}
		/>
		{#if inherited !== undefined}
			<p class="ridu-block-name__localization" data-localization-source={inherited}>
				{runtime.i18n.t("documents:inheritedFrom", {
					locale: contentLocaleLabel(runtime.contentLocales, inherited) ?? inherited,
				})}
			</p>
		{:else if current.localized && form.contentLocale !== undefined && binding.value === undefined}
			<p class="ridu-block-name__localization" data-localization-missing={form.contentLocale}>
				{runtime.i18n.t("documents:missingTranslation", {
					locale:
						contentLocaleLabel(runtime.contentLocales, form.contentLocale) ?? form.contentLocale,
				})}
			</p>
		{/if}
	</div>
{/if}
