<script lang="ts">
	import type { PluginFieldProps } from '@riducms/plugin';
	import { decodeColor, type Color } from './value';
	let { field }: PluginFieldProps<Color> = $props();
</script>

<label for={field.schema.id}>
	{field.schema.admin.label}
</label>
<!-- Black is an empty-state preview; oninput sets the form value. -->
<input
	id={field.schema.id}
	type="color"
	value={field.value ?? '#000000'}
	disabled={field.readOnly}
	oninput={(event) =>
		field.set(decodeColor(event.currentTarget.value))}
/>
{#each field.issues as issue (issue.code)}
	<p>{issue.message}</p>
{/each}
