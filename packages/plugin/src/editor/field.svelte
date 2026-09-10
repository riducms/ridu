<script lang="ts">
	import type { Snippet } from "svelte";
	import { FieldFrame } from "@riducms/ui";
	import type { FieldBinding } from "./types";

	let { field, children }: { field: Pick<FieldBinding, "schema" | "issues">; children: Snippet } =
		$props();
</script>

<div data-field-path={field.schema.path}>
	<FieldFrame
		controlID={field.schema.id}
		label={field.schema.admin.label}
		required={field.schema.required}
		readOnly={field.schema.admin.readOnly}
		description={field.schema.admin.description}
		errors={field.issues.map((issue) => issue.message)}
	>
		{@render children()}
	</FieldFrame>
</div>
