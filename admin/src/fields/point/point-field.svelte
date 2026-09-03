<script lang="ts">
	import type { SchemaField } from "@riducms/protocol";

	import { Input } from "@admin/components/ui/input";
	import type { FormController } from "@admin/core/forms/form-controller.svelte";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import FieldShell from "@admin/fields/field-shell.svelte";

	let { field, form }: { field: SchemaField; form: FormController } = $props();
	const runtime = getAdminRuntime();
	const value = $derived(
		Array.isArray(form.get(field.path)) ? (form.get(field.path) as number[]) : []
	);
	const issues = $derived(form.issuesFor(field.path));

	$effect(() => form.register(field.path));

	function setCoordinate(index: number, next: number) {
		const coordinates = [Number(value[0] ?? 0), Number(value[1] ?? 0)];
		coordinates[index] = next;
		form.set(field.path, coordinates);
	}
</script>

<FieldShell {field} {issues}>
	<div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
		<label class="grid gap-1.5 text-[12px] text-foreground-muted">
			{runtime.i18n.t("fields:longitude")}
			<Input
				type="number"
				min="-180"
				max="180"
				step="any"
				value={value[0] ?? ""}
				readonly={field.admin.readOnly}
				oninput={(event) => setCoordinate(0, event.currentTarget.valueAsNumber)}
			/>
		</label>
		<label class="grid gap-1.5 text-[12px] text-foreground-muted">
			{runtime.i18n.t("fields:latitude")}
			<Input
				type="number"
				min="-90"
				max="90"
				step="any"
				value={value[1] ?? ""}
				readonly={field.admin.readOnly}
				oninput={(event) => setCoordinate(1, event.currentTarget.valueAsNumber)}
			/>
		</label>
	</div>
</FieldShell>
