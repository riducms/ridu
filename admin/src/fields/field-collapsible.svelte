<script lang="ts">
	import Chevron from "@admin/components/icons/chevron.svelte";
	import type { SchemaField } from "@riducms/protocol";
	import type { FormController } from "@admin/core/forms/form-controller.svelte";
	import { fieldIssueRevealEvent } from "@admin/core/forms/field-issue-focus";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import FieldRenderer from "@admin/fields/field-renderer.svelte";

	let {
		fields,
		form,
		collapsible,
	}: {
		fields: readonly SchemaField[];
		form: FormController;
		collapsible: NonNullable<SchemaField["admin"]["collapsible"]>;
	} = $props();
	const runtime = getAdminRuntime();
	// The disclosure's identity belongs to its keyed layout group.
	// svelte-ignore state_referenced_locally
	let open = $state(!collapsible.initiallyCollapsed);
	function reveal(element: HTMLElement) {
		const show = () => {
			open = true;
		};
		element.addEventListener(fieldIssueRevealEvent, show);
		return () => element.removeEventListener(fieldIssueRevealEvent, show);
	}
</script>

<details
	bind:open
	{@attach reveal}
	class="ridu-field-collapsible"
	data-field-collapsible={collapsible.id}
>
	<summary class="ridu-field-collapsible__heading">
		{runtime.i18n.text(collapsible.label, collapsible.labelTranslations)}
		<Chevron />
	</summary>
	{#if open || fields.some((field) => form
				.pendingEditIssues()
				.some((issue) => issue.path === field.path || issue.path.startsWith(`${field.path}.`)))}
		<div class="ridu-field-grid ridu-field-collapsible__content">
			{#each fields as field (field.id)}
				<FieldRenderer {field} {form} />
			{/each}
		</div>
	{:else}
		<!-- Addressable boundaries reveal the disclosure before mounting/focusing a field. -->
		{#each fields as field (field.id)}
			<span hidden data-field-path={field.path}></span>
		{/each}
	{/if}
</details>
