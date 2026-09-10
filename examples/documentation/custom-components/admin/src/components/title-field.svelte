<script lang="ts">
	import type { FieldEditorProps } from '@riducms/plugin/editor';
	import Field from '@riducms/plugin/editor/field';
	import { Input } from '@riducms/ui';

	// Read and update the current form value through Ridu.
	let { field }: FieldEditorProps<'text'> = $props();
</script>

<!-- Field connects the input to its label and validation messages. -->
<Field {field}>
	<Input
		{...field.inputProps}
		value={field.value ?? ''}
		readonly={field.readOnly}
		oninput={(event) => field.set(event.currentTarget.value)}
	/>
	<p>{(field.value ?? '').length} characters</p>
</Field>
