<script lang="ts">
	import type { PluginFieldProps } from "@riducms/plugin";

	let { field }: PluginFieldProps<string> = $props();
	const value = $derived(field.value ?? "#000000");
</script>

<label for={field.schema.id}>{field.schema.admin.label}</label>
<input
	id={field.schema.id}
	type="color"
	{value}
	disabled={field.readOnly}
	oninput={(event) => field.set(event.currentTarget.value)}
/>
