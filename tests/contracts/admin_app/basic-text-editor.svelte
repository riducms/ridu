<script lang="ts">
	import type { FieldEditorProps } from "@riducms/plugin/editor";
	import Field from "@riducms/plugin/editor/field";
	import { Input } from "@riducms/ui";

	let { field }: FieldEditorProps<"text"> = $props();
</script>

<Field {field}>
	<Input
		{...field.inputProps}
		readonly={field.readOnly}
		value={field.value ?? ""}
		oninput={(event) => field.set(event.currentTarget.value)}
		data-local-editor="text"
	/>
</Field>
