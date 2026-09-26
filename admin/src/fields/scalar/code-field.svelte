<script lang="ts">
	import { untrack } from "svelte";
	import type { SchemaField, ValidationIssue } from "@riducms/protocol";
	import { fieldControlARIA } from "@riducms/ui";
	import type { FormController } from "@admin/core/forms/form-controller.svelte";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import CodeEditor from "@admin/components/ui/code-editor/code-editor.svelte";
	import FieldShell from "@admin/fields/field-shell.svelte";

	let { field, form }: { field: SchemaField; form: FormController } = $props();
	const runtime = getAdminRuntime();
	// Repeated-field parents recreate the field object when an index changes. Stabilizing that
	// object to its occurrence ID keeps an unfinished draft alive while the row moves.
	const fieldIdentity = $derived(field.id);
	const editorLifetime = $derived({ form, epoch: form.editorEpoch, id: fieldIdentity });
	const value = $derived(form.get(field.path));
	const editingBlocked = $derived(field.admin.readOnly === true || form.editingBlocked);
	let draft = $state("");
	let jsonError = $state<string>();
	let committed = "";

	const issues = $derived<readonly ValidationIssue[]>(
		jsonError
			? [{ code: "invalid_json", path: field.path, message: jsonError }]
			: form.issuesFor(field.path)
	);
	const aria = $derived(
		fieldControlARIA(field.id, field.admin.description !== undefined, issues.length > 0)
	);

	$effect(() => {
		const owner = form;
		return untrack(() =>
			owner.registerPendingEdit(() =>
				jsonError && !field.admin.readOnly
					? [{ code: "invalid_json", path: field.path, message: jsonError }]
					: []
			)
		);
	});
	$effect(() => {
		// Reset unfinished text on document reload, locale change or row identity change.
		const owner = editorLifetime;
		untrack(() => {
			jsonError = undefined;
			committed = encode(owner.form.get(field.path));
			draft = committed;
		});
	});
	$effect(() => {
		// Moving a row can clone its JSON value without replacing the editing lifetime.
		const next = encode(value);
		untrack(() => {
			if (next === committed) return;
			committed = next;
			draft = next;
			jsonError = undefined;
		});
	});
	$effect(() => {
		if (jsonError && !field.admin.readOnly) {
			const path = field.path;
			return untrack(() => form.setLiveInputUnavailable(path));
		}
	});

	function encode(next: unknown) {
		return field.type === "json" ? JSON.stringify(next ?? {}, null, 2) : String(next ?? "");
	}

	function update(next: string) {
		if (editingBlocked || next === draft) return;
		draft = next;
		try {
			const parsed = field.type === "json" ? (next.trim() === "" ? null : JSON.parse(next)) : next;
			form.set(field.path, parsed);
			committed = encode(form.get(field.path));
			jsonError = undefined;
		} catch {
			jsonError = runtime.i18n.t("errors:validJSON");
		}
	}
</script>

<FieldShell {field} {issues}>
	{#key editorLifetime}
		<CodeEditor
			value={draft}
			language={field.type === "json" ? "json" : (field.code?.language ?? "text")}
			configuration={{
				id: field.id,
				label: field.admin.label,
				readOnly: editingBlocked,
				invalid: issues.length > 0,
				describedBy: aria["aria-describedby"],
				errorMessage: aria["aria-errormessage"],
			}}
			onChange={update}
		/>
	{/key}
</FieldShell>
