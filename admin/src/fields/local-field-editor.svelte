<script lang="ts">
	import { onDestroy, type Component } from "svelte";
	import type { SchemaField } from "@riducms/protocol";
	import type { FieldAuthoringHost, AdminI18n } from "@riducms/plugin";
	import type { FieldEditorProps, RegisteredFieldEditor } from "@riducms/plugin/editor";
	import type { FormController } from "@admin/core/forms/form-controller.svelte";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import { FieldEditorBinding } from "@admin/core/forms/field-editor-binding";
	import { guardEditorAuthoring } from "@admin/core/forms/field-editor-authoring";

	let {
		schema,
		form,
		editor,
		authoring,
		i18n,
	}: {
		schema: SchemaField;
		form: FormController;
		editor: RegisteredFieldEditor;
		authoring: FieldAuthoringHost;
		i18n: AdminI18n;
	} = $props();
	const runtime = getAdminRuntime();
	// Parent keys this host by form, lifetime epoch, manifest revision and occurrence ID.
	// Each capability belongs to this mount; reordering only updates the schema getter.
	// svelte-ignore state_referenced_locally
	const config = editor.decode(schema);
	// Registration correlates props; decode above validates this manifest selection.
	// Keep this erasure at the host boundary so registrations cannot retype components.
	// svelte-ignore state_referenced_locally
	const Editor = editor.component as Component<
		Omit<FieldEditorProps<RegisteredFieldEditor["type"], unknown>, "config"> & { config?: unknown }
	>;
	// svelte-ignore state_referenced_locally
	const field = new FieldEditorBinding(
		form,
		() => schema,
		editor.type,
		() => runtime.manifestRevision
	);
	const guardedAuthoring = guardEditorAuthoring(() => authoring, field.assertActive);
	onDestroy(field.destroy);
</script>

<Editor
	{field}
	{...config === undefined ? {} : { config }}
	form={field.form}
	authoring={guardedAuthoring}
	{i18n}
/>
