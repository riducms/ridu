<script lang="ts">
	import type { SchemaField } from "@riducms/protocol";
	import { untrack } from "svelte";

	import { Button } from "@admin/components/ui/button";
	import { Input } from "@admin/components/ui/input";
	import type {
		DerivedTextBinding,
		FormController,
	} from "@admin/core/forms/form-controller.svelte";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import FieldShell from "@admin/fields/field-shell.svelte";
	import { normalizeSlug, slugFollowsSource } from "@admin/fields/text/slug";

	interface Props {
		field: SchemaField;
		form: FormController;
	}

	let { field, form }: Props = $props();
	const runtime = getAdminRuntime();
	const value = $derived(String(form.get(field.path) ?? ""));
	const issues = $derived(form.issuesFor(field.path));
	const hasMessage = $derived(issues.length > 0 || field.admin.description !== undefined);
	let locked = $state(true);
	let binding = $state<DerivedTextBinding>();

	$effect(() => {
		form.revision;
		const sourcePath = field.text?.slug?.sourcePath;
		const fieldPath = field.path;
		if (sourcePath === undefined) return;
		return untrack(() => {
			binding = form.bindDerivedText(
				fieldPath,
				sourcePath,
				(source) => normalizeSlug(String(source ?? "")),
				(current, source) => String(current ?? "") === "" || slugFollowsSource(current, source)
			);
		});
	});

	function generate() {
		binding?.follow();
	}

	function normalizeManualValue() {
		if (!locked) binding?.setManual(normalizeSlug(value));
	}

	function setManualValue(value: string) {
		binding?.setManual(value);
	}
</script>

<FieldShell {field} {issues}>
	<div class="flex items-center gap-2">
		<Input
			id={field.id}
			name={field.path}
			required={field.required}
			minlength={field.text?.minLength}
			maxlength={field.text?.maxLength}
			placeholder={field.admin.placeholder}
			aria-invalid={issues.length > 0}
			aria-describedby={hasMessage ? `${field.id}-message` : undefined}
			aria-errormessage={issues.length > 0 ? `${field.id}-message` : undefined}
			readonly={field.admin.readOnly || locked}
			class="min-w-0 flex-1 font-mono"
			{value}
			oninput={(event) => setManualValue(event.currentTarget.value)}
			onblur={normalizeManualValue}
		/>
		{#if !field.admin.readOnly}
			<div class="flex shrink-0 items-center gap-1">
				{#if !locked}
					<Button type="button" size="xs" variant="ghost" onclick={generate}>
						{runtime.i18n.t("fields:generateSlug")}
					</Button>
				{/if}
				<Button type="button" size="xs" variant="ghost" onclick={() => (locked = !locked)}>
					{runtime.i18n.t(locked ? "fields:unlockSlug" : "fields:lockSlug")}
				</Button>
			</div>
		{/if}
	</div>
</FieldShell>
