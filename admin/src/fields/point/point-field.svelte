<script lang="ts">
	import "@admin/fields/field-layout.scss";
	import type { SchemaField } from "@riducms/protocol";
	import { fieldControlARIA, Input } from "@riducms/ui";

	import type { FormController } from "@admin/core/forms/form-controller.svelte";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import FieldMessages from "@admin/fields/field-messages.svelte";

	let { field, form }: { field: SchemaField; form: FormController } = $props();
	const runtime = getAdminRuntime();
	const value = $derived.by(() => {
		const current = form.get(field.path);
		return Array.isArray(current) ? (current as number[]) : [];
	});
	const issues = $derived(form.issuesFor(field.path));
	const editingBlocked = $derived(field.admin.readOnly === true || form.editingBlocked);
	const controlARIA = $derived(
		fieldControlARIA(field.id, field.admin.description !== undefined, issues.length > 0)
	);

	$effect(() => form.register(field.path));

	function setCoordinate(index: number, next: number) {
		if (editingBlocked) return;
		const coordinates = [Number(value[0] ?? 0), Number(value[1] ?? 0)];
		coordinates[index] = next;
		form.set(field.path, coordinates);
	}
</script>

<div data-field-path={field.path}>
	<FieldMessages controlID={field.id} {issues} description={field.admin.description}>
		<div class="ridu-point-field">
			<label class="ridu-point-field__coordinate">
				<span>
					{field.admin.label} - {runtime.i18n.t("fields:longitude")}
					{#if field.required}
						<span class="ridu-field-required" aria-hidden="true">*</span>
					{/if}
				</span>
				<Input
					id={field.id}
					type="number"
					min="-180"
					max="180"
					step="any"
					value={value[0] ?? ""}
					readonly={editingBlocked}
					{...controlARIA}
					oninput={(event) => setCoordinate(0, event.currentTarget.valueAsNumber)}
				/>
			</label>
			<label class="ridu-point-field__coordinate">
				<span>
					{field.admin.label} - {runtime.i18n.t("fields:latitude")}
					{#if field.required}
						<span class="ridu-field-required" aria-hidden="true">*</span>
					{/if}
				</span>
				<Input
					id={`${field.id}-latitude`}
					type="number"
					min="-90"
					max="90"
					step="any"
					value={value[1] ?? ""}
					readonly={editingBlocked}
					{...controlARIA}
					oninput={(event) => setCoordinate(1, event.currentTarget.valueAsNumber)}
				/>
			</label>
		</div>
	</FieldMessages>
</div>
