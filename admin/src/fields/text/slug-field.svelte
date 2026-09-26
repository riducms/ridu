<script lang="ts">
	import type { SchemaField } from "@riducms/protocol";
	import { fieldControlARIA, Button, Input } from "@riducms/ui";
	import { untrack } from "svelte";

	import type {
		DerivedTextBinding,
		FormController,
	} from "@admin/core/forms/form-controller.svelte";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import FieldShell from "@admin/fields/field-shell.svelte";
	import { normalizeSlug, slugFollowsSource } from "@admin/fields/text/slug";
	import "@admin/fields/text/slug-field.scss";

	interface Props {
		field: SchemaField;
		form: FormController;
	}

	let { field, form }: Props = $props();
	const runtime = getAdminRuntime();
	const value = $derived(String(form.get(field.path) ?? ""));
	const issues = $derived(form.issuesFor(field.path));
	const editingBlocked = $derived(field.admin.readOnly === true || form.editingBlocked);
	const controlARIA = $derived(
		fieldControlARIA(field.id, field.admin.description !== undefined, issues.length > 0)
	);
	let locked = $state(true);
	let binding: DerivedTextBinding | undefined;

	$effect(() => {
		form.revision;
		const sourcePath = field.text?.slug?.sourcePath;
		const fieldPath = field.path;
		if (sourcePath === undefined) return;
		// Only the initialization snapshot is excluded: later changes are observed by
		// FormController, while form.revision above owns reset/rebind synchronization.
		const initial = untrack(() => ({
			current: form.get(fieldPath),
			source: form.get(sourcePath),
		}));
		binding = form.bindDerivedText(
			fieldPath,
			sourcePath,
			(source) => normalizeSlug(String(source ?? "")),
			(current, source) => String(current ?? "") === "" || slugFollowsSource(current, source),
			initial
		);
	});

	function generate() {
		if (editingBlocked) return;
		binding?.follow();
	}

	function normalizeManualValue() {
		if (!editingBlocked && !locked) binding?.setManual(normalizeSlug(value));
	}

	function setManualValue(value: string) {
		if (editingBlocked) return;
		binding?.setManual(value);
	}
</script>

<FieldShell {field} {issues}>
	<div class="ridu-slug-field">
		<Input
			id={field.id}
			name={field.path}
			required={field.required}
			minlength={field.text?.minLength}
			maxlength={field.text?.maxLength}
			placeholder={field.admin.placeholder}
			{...controlARIA}
			readonly={editingBlocked || locked}
			class="ridu-slug-field__input"
			{value}
			oninput={(event) => setManualValue(event.currentTarget.value)}
			onblur={normalizeManualValue}
		/>
		{#if !field.admin.readOnly}
			<div class="ridu-slug-field__actions">
				{#if !locked}
					<Button
						type="button"
						size="xs"
						variant="ghost"
						disabled={editingBlocked}
						onclick={generate}
					>
						{runtime.i18n.t("fields:generateSlug")}
					</Button>
				{/if}
				<Button
					type="button"
					size="xs"
					variant="ghost"
					disabled={editingBlocked}
					onclick={() => (locked = !locked)}
				>
					{runtime.i18n.t(locked ? "fields:unlockSlug" : "fields:lockSlug")}
				</Button>
			</div>
		{/if}
	</div>
</FieldShell>
