<script lang="ts">
	import type { FieldEditorProps } from "@riducms/plugin/editor";
	import Field from "@riducms/plugin/editor/field";
	import { Input, Button } from "@riducms/ui";

	let { field, config }: FieldEditorProps<"text", { capture: boolean }> = $props();
	function capture() {
		// Browser contract harness retains the original capability across host transitions.
		window.dispatchEvent(new CustomEvent("ridu-test-editor-capture", { detail: field }));
	}
</script>

<Field {field}>
	<Input
		{...field.inputProps}
		readonly={field.readOnly}
		value={field.value ?? ""}
		oninput={(event) => field.set(event.currentTarget.value)}
		data-local-editor="text"
	/>
	{#if config.capture}
		<Button type="button" variant="ghost" onclick={capture}>Capture editor</Button>
	{/if}
</Field>
