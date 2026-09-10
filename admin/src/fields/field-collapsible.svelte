<script lang="ts">
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
	class="col-span-1 rounded-[4px] border border-control-border bg-background sm:col-span-12"
	data-field-collapsible={collapsible.id}
>
	<summary class="cursor-pointer px-5 py-4 text-[13.5px] font-medium text-foreground">
		{runtime.i18n.text(collapsible.label, collapsible.labelTranslations)}
	</summary>
	{#if open}
		<div
			class="grid grid-cols-1 gap-x-4 gap-y-6 border-t border-control-border p-5 sm:grid-cols-12"
		>
			{#each fields as field (field.id)}<FieldRenderer {field} {form} />{/each}
		</div>
	{:else}
		<!-- Addressable boundaries reveal the disclosure before mounting/focusing a field. -->
		{#each fields as field (field.id)}<span hidden data-field-path={field.path}></span>{/each}
	{/if}
</details>
