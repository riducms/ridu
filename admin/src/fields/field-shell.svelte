<script lang="ts">
	import type { SchemaField, ValidationIssue } from "@riducms/protocol";
	import type { Snippet } from "svelte";

	import { FieldFrame } from "@riducms/ui";

	let {
		field,
		issues = [],
		children,
	}: {
		field: SchemaField;
		issues?: ValidationIssue[];
		children: Snippet;
	} = $props();
	const errorMessages = $derived(issues.map((issue) => issue.message));
</script>

<div data-field-path={field.path}>
	<FieldFrame
		controlID={field.id}
		label={field.admin.label}
		required={field.required}
		readOnly={field.admin.readOnly}
		description={field.admin.description}
		errors={errorMessages}
	>
		{@render children()}
	</FieldFrame>
</div>
