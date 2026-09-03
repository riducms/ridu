<script lang="ts">
	import type { SchemaField } from "@riducms/protocol";

	import type { FormController } from "@admin/core/forms/form-controller.svelte";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import FieldShell from "@admin/fields/field-shell.svelte";

	let { field, form }: { field: SchemaField; form: FormController } = $props();
	const runtime = getAdminRuntime();
	const value = $derived(form.get(field.path));
	const formatted = $derived(
		value === undefined || value === null
			? runtime.i18n.t("general:none")
			: typeof value === "object"
				? JSON.stringify(value, null, 2)
				: String(value)
	);
</script>

<FieldShell {field} issues={[]}>
	<output
		id={field.id}
		class="block min-h-10 whitespace-pre-wrap rounded-[3px] border border-control-border-disabled bg-control-disabled px-3 py-2.5 text-[13px] leading-5 text-foreground-label"
		data-virtual-field={field.name}
	>
		{formatted}
	</output>
</FieldShell>
