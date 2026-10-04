<script lang="ts">
	import { getAdminI18n, type EmbeddedSchemaDraftEditorProps } from "@riducms/plugin";
	import type { HostedSchemaDraft } from "@admin/core/forms/embedded-schema-draft.svelte";
	import { Sheet, SheetContent, SheetTitle, SheetDescription } from "@admin/components/ui/sheet";
	import { Button } from "@riducms/ui";
	import { focusFieldIssue } from "@admin/core/forms/field-issue-focus";
	import FieldLayout from "@admin/fields/field-layout.svelte";
	import BlockHeader from "@admin/fields/nested/block-header.svelte";
	import "@admin/fields/embedded-schema-draft-editor.scss";

	let {
		session,
		options,
	}: { session: HostedSchemaDraft; options: EmbeddedSchemaDraftEditorProps } = $props();
	// A mounted drawer owns exactly one detached edit and releases its parent blocker.
	// svelte-ignore state_referenced_locally
	const draft = session.draft;
	const i18n = getAdminI18n();
	// Only event handlers read this DOM binding.
	// svelte-ignore non_reactive_update
	let contentElement: HTMLElement | null = null;
	$effect(() => () => draft.discard());

	function cancel() {
		draft.discard();
		options.onCancel();
	}
	async function apply() {
		if (!draft.validate()) {
			const issue = draft.issues[0];
			if (issue !== undefined && contentElement) await focusFieldIssue(issue.path, contentElement);
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
		bind:ref={contentElement}
		class="ridu-embedded-draft"
		showCloseButton={false}
		onOpenAutoFocus={async (event) => {
			const issue = draft.issues[0];
			if (issue !== undefined && contentElement) {
				event.preventDefault();
				await focusFieldIssue(issue.path, contentElement);
			}
		}}
	>
		<div class="ridu-embedded-draft__header">
			<SheetTitle>{options.title ?? i18n.t("fields:embeddedEditTitle")}</SheetTitle>
			<SheetDescription>{i18n.t("fields:embeddedEditDescription")}</SheetDescription>
			{#if !draft.stale}
				<div class="ridu-embedded-draft__name">
					<BlockHeader
						block={session.block}
						path={draft.id}
						instance={draft.id}
						form={session.form}
					/>
				</div>
			{/if}
		</div>
		<div class="ridu-embedded-draft__content" data-schema-draft={draft.id}>
			{#if draft.stale}
				<p role="alert" class="ridu-embedded-draft__error">
					{i18n.t("errors:staleEmbeddedEdit")}
				</p>
			{/if}
			{#if draft.issues.length > 0}
				<div role="alert" class="ridu-embedded-draft__error">
					{#each draft.issues as issue}
						<p>{issue.message}</p>
					{/each}
				</div>
			{/if}
			{#if !draft.stale}
				<FieldLayout
					fields={session.fields.filter((field) => field.name !== "blockName")}
					form={session.form}
				/>
			{/if}
		</div>
		<div class="ridu-embedded-draft__actions">
			<Button variant="outline" onclick={cancel}>{i18n.t("general:cancel")}</Button>
			<Button onclick={apply} disabled={draft.stale}>{i18n.t("general:apply")}</Button>
		</div>
	</SheetContent>
</Sheet>
