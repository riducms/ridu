<script lang="ts">
	import { untrack } from "svelte";
	import type { EmbeddedSchemaFormProps } from "@riducms/plugin";
	import type { SchemaField, ValidationIssue } from "@riducms/protocol";
	import type { FormController } from "@admin/core/forms/form-controller.svelte";
	import type { EmbeddedOccurrence } from "@admin/core/forms/embedded-fields";
	import { observeEmbeddedSchemaForm } from "@admin/core/forms/embedded-schema-form";
	import { fieldAccessPath, scopeRepeatedRowField } from "@admin/fields/nested/scoped-field";
	import FieldLayout from "@admin/fields/field-layout.svelte";

	let {
		field,
		form,
		scope,
		occurrence,
		issues,
		onChange,
	}: {
		field: SchemaField;
		form: FormController;
		scope: EmbeddedSchemaFormProps;
		occurrence: EmbeddedOccurrence | undefined;
		issues: readonly ValidationIssue[];
		onChange?: (payload: Record<string, unknown>) => void;
	} = $props();
	$effect(() => {
		const callback = onChange;
		const treeKey = scope.treeKey;
		const identity = scope.identity;
		const schema = field;
		if (callback === undefined) return;
		// Controller notifications run in the write call stack; avoid subscribing to the
		// payload's reactive value, which would tear down this observer after each edit.
		return untrack(() =>
			observeEmbeddedSchemaForm(
				form,
				schema,
				treeKey,
				identity,
				callback,
				() =>
					!form.editingBlocked &&
					scope.readOnly !== true &&
					!field.admin.readOnly &&
					form.canWrite(field.path, fieldAccessPath(field))
			)
		);
	});
	const children = $derived(
		occurrence?.block.fields.map((child) =>
			scopeRepeatedRowField(
				child,
				occurrence.path,
				`${field.id}-${scope.treeKey}-${scope.identity}`,
				scope.readOnly === true ||
					field.admin.readOnly === true ||
					!form.canWrite(field.path, fieldAccessPath(field))
			)
		) ?? []
	);
</script>

{#if issues.length > 0}
	<p role="alert">{issues[0]?.message}</p>
{:else if occurrence === undefined}
	<p role="alert">
		The embedded field occurrence is no longer available. Close this editor and reopen it.
	</p>
{:else}
	<FieldLayout fields={children.filter((child) => child.name !== "blockName")} {form} inset />
{/if}
