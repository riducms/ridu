<script lang="ts">
	import type { EmbeddedSchemaFormScope } from "@riducms/plugin";
	import type { SchemaField, ValidationIssue } from "@riducms/protocol";
	import type { FormController } from "@admin/core/forms/form-controller.svelte";
	import type { EmbeddedOccurrence } from "@admin/core/forms/embedded-fields";
	import { fieldAccessPath, scopeRepeatedRowField } from "@admin/fields/nested/scoped-field";
	import FieldLayout from "@admin/fields/field-layout.svelte";

	let {
		field,
		form,
		scope,
		occurrence,
		issues,
	}: {
		field: SchemaField;
		form: FormController;
		scope: EmbeddedSchemaFormScope;
		occurrence: EmbeddedOccurrence | undefined;
		issues: readonly ValidationIssue[];
	} = $props();
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
	<FieldLayout fields={children} {form} />
{/if}
