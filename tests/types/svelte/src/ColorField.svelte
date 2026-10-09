<script lang="ts">
	import type { PluginFieldProps } from "@riducms/plugin";

	let { field }: PluginFieldProps<string> = $props();
</script>

<label for={field.schema.id}>{field.schema.admin.label}</label>
<input
	id={field.schema.id}
	type="color"
	bind:value={() => field.value ?? "#000000", (next) => field.set(next)}
	disabled={field.readOnly}
/>
