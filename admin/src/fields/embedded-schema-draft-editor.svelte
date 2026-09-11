<script lang="ts">
	import { getAdminI18n, type EmbeddedSchemaDraftEditorProps } from "@riducms/plugin";
	import type { HostedSchemaDraft } from "@admin/core/forms/embedded-schema-draft.svelte";
	import { Sheet, SheetContent, SheetTitle, SheetDescription } from "@admin/components/ui/sheet";
	import { Button } from "@admin/components/ui/button";
	import { focusFieldIssue } from "@admin/core/forms/field-issue-focus";
	import FieldLayout from "@admin/fields/field-layout.svelte";
	import BlockHeader from "@admin/fields/nested/block-header.svelte";
	import { onDestroy } from "svelte";

	let {
		session,
		options,
	}: { session: HostedSchemaDraft; options: EmbeddedSchemaDraftEditorProps } = $props();
	// A mounted drawer owns exactly one detached edit and releases its parent blocker.
	// svelte-ignore state_referenced_locally
	const draft = session.draft;
	const i18n = getAdminI18n();
	onDestroy(() => draft.discard());

	function cancel() {
		draft.discard();
		options.onCancel();
	}
	async function apply() {
		if (!draft.validate()) {
			const issue = draft.issues[0];
			if (issue !== undefined) await focusFieldIssue(issue.path);
			return;
		}
		options.onApply(draft.payload());
		draft.discard();
	}
</script>

<Sheet
	open
	onOpenChange={(open) => {
		if (!open) cancel();
	}}
>
	<SheetContent
		class="w-full sm:max-w-2xl"
		showCloseButton={false}
		onOpenAutoFocus={async (event) => {
			const issue = draft.issues[0];
			if (issue !== undefined) {
				event.preventDefault();
				await focusFieldIssue(issue.path);
			}
		}}
	>
		<div class="border-b border-border px-6 py-4">
			<SheetTitle>{options.title ?? i18n.t("fields:embeddedEditTitle")}</SheetTitle>
			<SheetDescription>{i18n.t("fields:embeddedEditDescription")}</SheetDescription>
			{#if !draft.stale && session.block.admin?.nameField !== undefined}
				<div class="mt-3">
					<BlockHeader
						block={session.block}
						path={draft.id}
						instance={draft.id}
						form={session.form}
					/>
				</div>
			{/if}
		</div>
		<div class="min-h-0 flex-1 overflow-y-auto px-6 py-5" data-schema-draft={draft.id}>
			{#if draft.stale}
				<p role="alert" class="mb-4 text-destructive">
					{i18n.t("errors:staleEmbeddedEdit")}
				</p>
			{/if}
			{#if draft.issues.length > 0}
				<div role="alert" class="mb-4 text-destructive">
					{#each draft.issues as issue}<p>{issue.message}</p>{/each}
				</div>
			{/if}
			{#if !draft.stale}<FieldLayout
					fields={session.fields.filter((field) => field.name !== session.block.admin?.nameField)}
					form={session.form}
				/>{/if}
		</div>
		<div class="flex justify-end gap-2 border-t border-border px-6 py-4">
			<Button variant="outline" onclick={cancel}>{i18n.t("general:cancel")}</Button>
			<Button onclick={apply} disabled={draft.stale}>{i18n.t("general:apply")}</Button>
		</div>
	</SheetContent>
</Sheet>
