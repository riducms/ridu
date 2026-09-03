<script lang="ts">
	import type { FieldComponentProps } from "@riducms/plugin";

	let { field, form }: FieldComponentProps = $props();
	const value = $derived(String(form.get(field.path) ?? "#000000"));
</script>

<label for={field.id}>{field.admin.label}</label>
<input
	id={field.id}
	type="color"
	{value}
	disabled={field.admin.readOnly}
	oninput={(event) => form.set(field.path, event.currentTarget.value)}
/>
