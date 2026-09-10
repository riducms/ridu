<script lang="ts">
	import type { FieldAuthoringHost } from "@riducms/plugin";
	import type { SchemaField } from "@riducms/protocol";

	import type { FormController } from "@admin/core/forms/form-controller.svelte";
	import { evaluateFieldCondition } from "@admin/core/forms/field-condition";
	import { localizeSchemaCollection, localizeSchemaField } from "@admin/core/i18n/localized-schema";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import { fieldAccessPath } from "@admin/fields/nested/scoped-field";
	import ReferenceBrowser from "@admin/features/reference-browser/reference-browser-loader.svelte";
	import {
		contentLocaleLabel,
		withContentLocaleLabel,
	} from "@admin/fields/localized-field-presentation";

	import PluginField from "@admin/fields/plugin-field.svelte";
	import LocalFieldEditor from "@admin/fields/local-field-editor.svelte";
	import SelectField from "@admin/fields/select/select-field.svelte";
	import RadioField from "@admin/fields/radio/radio-field.svelte";
	import PointField from "@admin/fields/point/point-field.svelte";
	import UIField from "@admin/fields/ui/ui-field.svelte";
	import JoinField from "@admin/fields/join/join-field.svelte";
	import VirtualField from "@admin/fields/virtual/virtual-field.svelte";
	import ScalarField from "@admin/fields/scalar/scalar-field.svelte";
	import NestedField from "@admin/fields/nested/nested-field.svelte";
	import RelationshipField from "@admin/fields/relationship/relationship-field.svelte";
	import PrimitiveListField from "@admin/fields/primitive-list/primitive-list-field.svelte";
	import TextField from "@admin/fields/text/text-field.svelte";
	import LiveValidationFeedback from "@admin/fields/live-validation-feedback.svelte";

	interface Props {
		field: SchemaField;
		form: FormController;
	}

	let { field, form }: Props = $props();
	const runtime = getAdminRuntime();
	const authoring: FieldAuthoringHost = {
		get collections() {
			return (
				runtime.manifest?.collections.map((collection) =>
					localizeSchemaCollection(collection, runtime.i18n)
				) ?? []
			);
		},
		get documentRevision() {
			return runtime.documentRevision;
		},
		get locale() {
			return form.contentLocale;
		},
		referenceBrowser: ReferenceBrowser,
		findDocument: (collection, id, signal) =>
			runtime.client.find(collection, id, { signal, locale: form.contentLocale }),
	};
	const editor = $derived(
		field.admin.editor === undefined
			? undefined
			: runtime.editors[field.admin.editor.reference as `app:${string}`]
	);
	const plugin = $derived(editor === undefined ? runtime.fields.resolve(field) : undefined);
	const accessPath = $derived(fieldAccessPath(field));
	const visible = $derived(
		field.admin.hidden !== true &&
			form.canRead(field.path, accessPath) &&
			(field.admin.condition === undefined ||
				evaluateFieldCondition(field.admin.condition, field.path, (path) => form.get(path)))
	);
	const renderedField = $derived(
		field.admin.readOnly || form.canWrite(field.path, accessPath)
			? field
			: { ...field, admin: { ...field.admin, readOnly: true } }
	);
	const localizedField = $derived(localizeSchemaField(renderedField, runtime.i18n));
	const activeContentLocaleLabel = $derived(
		contentLocaleLabel(runtime.contentLocales, form.contentLocale)
	);
	const presentedField = $derived(withContentLocaleLabel(localizedField, activeContentLocaleLabel));
	const columnClasses: Record<number, string> = {
		1: "col-span-1 sm:col-span-1",
		2: "col-span-1 sm:col-span-2",
		3: "col-span-1 sm:col-span-3",
		4: "col-span-1 sm:col-span-4",
		5: "col-span-1 sm:col-span-5",
		6: "col-span-1 sm:col-span-6",
		7: "col-span-1 sm:col-span-7",
		8: "col-span-1 sm:col-span-8",
		9: "col-span-1 sm:col-span-9",
		10: "col-span-1 sm:col-span-10",
		11: "col-span-1 sm:col-span-11",
		12: "col-span-1 sm:col-span-12",
	};
	const columns = $derived(Math.min(12, Math.max(1, field.admin.columns ?? 12)));
	const columnClass = $derived(columnClasses[columns] ?? columnClasses[12]);
	const inheritedFrom = $derived(
		form.isInherited(field.path) ? form.localizationSource(field.path) : undefined
	);
	const inheritedFromLabel = $derived(contentLocaleLabel(runtime.contentLocales, inheritedFrom));
	const missingLocale = $derived(
		field.localized === true &&
			form.contentLocale !== undefined &&
			form.get(field.path) === undefined
	);

	function renderField() {
		switch (plugin?.type) {
			case "text":
				return TextField;
			case "text-list":
			case "number-list":
				return PrimitiveListField;
			case "select":
				return SelectField;
			case "radio":
				return RadioField;
			case "point":
				return PointField;
			case "ui":
				return UIField;
			case "join":
				return JoinField;
			case "virtual":
				return VirtualField;
			case "group":
			case "array":
			case "blocks":
				return NestedField;
			case "relationship":
			case "upload":
				return RelationshipField;
			default:
				return ScalarField;
		}
	}
</script>

{#if visible}<div
		class={columnClass}
		data-live-validation-field={field.path}
		onfocusout={(event) => {
			if (
				event.target instanceof Element &&
				event.target.closest("[data-live-validation-field]") === event.currentTarget &&
				!(event.relatedTarget instanceof Node && event.currentTarget.contains(event.relatedTarget))
			)
				form.liveValidation.flush(field.path);
		}}
	>
		{const RenderField = $derived(renderField())}
		{#if editor !== undefined}
			{#key form}
				{#key `${form.editorEpoch}:${runtime.manifestRevision}:${field.id}`}
					<LocalFieldEditor
						schema={presentedField}
						{form}
						{editor}
						{authoring}
						i18n={runtime.i18n}
					/>
				{/key}
			{/key}
		{:else if plugin?.extension !== undefined}
			{#key form}
				{#key `${form.editorEpoch}:${runtime.manifestRevision}:${field.id}`}
					<PluginField schema={presentedField} {form} extension={plugin.extension} />
				{/key}
			{/key}
		{:else}
			<RenderField field={presentedField} {form} {authoring} />
		{/if}
		<LiveValidationFeedback
			feedback={form.liveValidation.forField(field.path)}
			i18n={runtime.i18n}
			path={field.path}
		/>
		{#if inheritedFrom !== undefined}<p
				class="mt-1 font-mono text-[10px] text-foreground-faint"
				data-localization-source={inheritedFrom}
			>
				{runtime.i18n.t("documents:inheritedFrom", {
					locale: inheritedFromLabel ?? inheritedFrom,
				})}
			</p>{:else if missingLocale}<p
				class="mt-1 font-mono text-[10px] text-foreground-faint"
				data-localization-missing={form.contentLocale}
			>
				{runtime.i18n.t("documents:missingTranslation", {
					locale: activeContentLocaleLabel ?? form.contentLocale ?? "",
				})}
			</p>{/if}
	</div>{/if}
