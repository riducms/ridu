<script lang="ts">
	import type { FieldEditorProps } from "@riducms/plugin/editor";
	import Field from "@riducms/plugin/editor/field";
	import { Textarea, Button } from "@riducms/ui";
	let { field }: FieldEditorProps<"text-list"> = $props();
</script>

<Field {field}>
	<Textarea
		{...field.inputProps}
		readonly={field.readOnly}
		value={field.value?.join("\n") ?? ""}
		oninput={(event) => field.set(event.currentTarget.value.split("\n"))}
		data-local-editor="text-list"
	/>
	<Button disabled={field.readOnly} onclick={() => field.set([])}>Clear editor points</Button>
</Field>
