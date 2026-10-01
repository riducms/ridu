<script lang="ts">
	import { getAdminI18n } from "@riducms/plugin";
	import {
		Button,
		Dialog,
		DialogContent,
		DialogDescription,
		DialogFooter,
		DialogHeader,
		DialogTitle,
	} from "@riducms/ui";
	import { tick } from "svelte";
	import { Banner } from "@admin/components/ui/banner";
	import type { DocumentController } from "@admin/features/documents/document-controller.svelte";
	import { formatVersionValue } from "@admin/features/versions/version-diff";

	import "@admin/features/documents/document-draft-recovery.scss";

	let {
		controller,
		onResolved,
	}: {
		controller: DocumentController;
		/** Receives focus after a choice removes this review from the page. */
		onResolved?: () => void;
	} = $props();
	const i18n = getAdminI18n();
	let open = $state(true);
	let reviewButton = $state<HTMLButtonElement | null>(null);
	let comparisonRegion = $state<HTMLElement | null>(null);
	const comparison = $derived(controller.recoveryComparison);

	function displayValue(value: unknown, field: (typeof comparison)[number]) {
		if (value === undefined || value === null) return i18n.t("documents:recoveryMissingValue");
		return formatVersionValue(value, i18n, field);
	}

	async function resolve(choice: () => void) {
		choice();
		await tick();
		onResolved?.();
	}
</script>

<Banner class="ridu-document-notice" tone="warning">
	<span class="ridu-document-notice__message">
		{i18n.t("documents:recoveryConflictDescription")}
	</span>
	<Button bind:ref={reviewButton} variant="outline" size="sm" onclick={() => (open = true)}>
		{i18n.t("documents:reviewUnsavedChanges")}
	</Button>
</Banner>

<Dialog bind:open>
	<DialogContent
		class="ridu-recovery-dialog"
		closeLabel={i18n.t("general:close")}
		onOpenAutoFocus={(event) => {
			// The dialog opens on page load; a stray Enter must not choose for the editor.
			event.preventDefault();
			comparisonRegion?.focus();
		}}
		onCloseAutoFocus={(event) => {
			if (controller.recoveryConflict !== undefined && reviewButton !== null) {
				event.preventDefault();
				reviewButton.focus();
			}
		}}
	>
		<DialogHeader>
			<DialogTitle>{i18n.t("documents:recoveryConflictTitle")}</DialogTitle>
			<DialogDescription>{i18n.t("documents:recoveryConflictDescription")}</DialogDescription>
		</DialogHeader>

		<div class="ridu-recovery-comparison" tabindex="-1" bind:this={comparisonRegion}>
			{#if comparison.length === 0}
				<p>{i18n.t("documents:recoveryNoVisibleChanges")}</p>
			{:else}
				<table class="ridu-recovery-table">
					<thead>
						<tr>
							<th scope="col">{i18n.t("versions:field")}</th>
							<th scope="col">{i18n.t("documents:recoveryBase")}</th>
							<th scope="col">{i18n.t("documents:recoveryYours")}</th>
							<th scope="col">{i18n.t("documents:recoveryLatest")}</th>
						</tr>
					</thead>
					<tbody>
						{#each comparison as field (field.id)}
							<tr>
								<th scope="row">{field.label}</th>
								<td>{displayValue(field.original, field)}</td>
								<td>{displayValue(field.yours, field)}</td>
								<td>{displayValue(field.latest, field)}</td>
							</tr>
						{/each}
					</tbody>
				</table>
			{/if}
		</div>

		<DialogFooter>
			<Button variant="outline" onclick={() => resolve(controller.discardChanges)}>
				{i18n.t("documents:recoveryLoadLatest")}
			</Button>
			<Button
				disabled={!controller.canKeepRecoveredChanges}
				onclick={() => resolve(controller.keepRecoveredChanges)}
			>
				{i18n.t("documents:recoveryKeepYours")}
			</Button>
		</DialogFooter>
	</DialogContent>
</Dialog>
