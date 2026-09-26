<script lang="ts">
	import { getAdminI18n } from "@riducms/plugin";
	import type { SchemaField } from "@riducms/protocol";
	import { Button } from "@riducms/ui";
	import { Dialog } from "bits-ui";
	import { tick } from "svelte";
	import XIcon from "~icons/lucide/x";

	import { focusFieldIssue } from "@admin/core/forms/field-issue-focus";
	import { FormController } from "@admin/core/forms/form-controller.svelte";
	import { initialBulkEditValues } from "@admin/features/bulk-edit/bulk-edit-fields";
	import BulkEditFieldPicker from "@admin/features/bulk-edit/field-picker.svelte";
	import type { CollectionBulkUpdateTarget } from "@admin/features/collections/collection-list-controller.svelte";
	import { getCollectionList } from "@admin/features/collections/collection-list.svelte";
	import FieldLayout from "@admin/fields/field-layout.svelte";
	import "@admin/features/bulk-edit/bulk-edit.scss";

	const list = getCollectionList();
	const i18n = getAdminI18n();
	const form = new FormController({}, i18n);

	let { open = $bindable(false) }: { open?: boolean } = $props();

	let target = $state.raw<CollectionBulkUpdateTarget>();
	let editorFields = $state.raw<readonly SchemaField[]>([]);
	let ownerFields: readonly SchemaField[] | undefined;
	let selectedPaths = $state<string[]>([]);
	// Only the issue-focus handler reads this DOM binding.
	// svelte-ignore non_reactive_update
	let fieldsViewport: HTMLElement | null = null;
	let wasOpen = false;

	const selectedFields = $derived(
		editorFields.filter((field) => selectedPaths.includes(field.path))
	);
	const pending = $derived(list.controller.bulkPending || form.submitting);
	const selectedCount = $derived(target?.ids.length ?? 0);
	const selectedLabel = $derived.by(() => {
		const collection = target?.collection;
		if (collection === undefined) return i18n.t("collections:documents");
		const labels = collection.labels;
		return selectedCount === 1
			? i18n.text(labels.singular, labels.singularTranslations)
			: i18n.text(labels.plural, labels.pluralTranslations);
	});

	form.setEditorScope(() => open);

	$effect(() => {
		const currentFields = list.bulkEditableFields;
		if (open && !wasOpen) {
			target = list.controller.captureBulkUpdateTarget();
			ownerFields = currentFields;
			editorFields = [...currentFields];
			selectedPaths = [];
			form.reset({}, []);
			form.setResource({ collection: target.slug });
			form.setLocalization(target.locale);
		} else if (
			open &&
			target !== undefined &&
			(target.slug !== list.slug ||
				target.locale !== list.contentLocale ||
				target.collection !== list.collection ||
				ownerFields !== currentFields)
		) {
			open = false;
			resetEditor();
		} else if (!open && wasOpen) {
			resetEditor();
		}

		wasOpen = open;
	});

	$effect(() => () => form.disposeBindings());

	function changeOpen(next: boolean) {
		if (pending && !next) return;
		open = next;
	}

	function resetEditor() {
		target = undefined;
		ownerFields = undefined;
		editorFields = [];
		selectedPaths = [];
		form.reset({}, []);
		form.setResource(undefined);
		form.setLocalization(undefined);
	}

	function changeSelectedPaths(paths: string[]) {
		const allowed = new Set(editorFields.map((field) => field.path));
		selectedPaths = [...new Set(paths.filter((path) => allowed.has(path)))];
		const fields = editorFields.filter((field) => selectedPaths.includes(field.path));
		form.reset(initialBulkEditValues(fields, form.snapshot()), fields);
	}

	async function focusFirstIssue() {
		const issue = form.issues[0];
		if (issue === undefined || fieldsViewport === null) return;
		await tick();
		await focusFieldIssue(issue.path, fieldsViewport);
	}

	async function submit() {
		const updateTarget = target;
		if (
			pending ||
			selectedFields.length === 0 ||
			updateTarget === undefined ||
			updateTarget.ids.length === 0
		)
			return;

		try {
			const updated = await form.submit(selectedFields, true, (values) =>
				list.controller.bulkUpdate(updateTarget, values)
			);
			if (updated) open = false;
		} catch {
			await focusFirstIssue();
		}
	}
</script>

<Dialog.Root {open} onOpenChange={changeOpen}>
	<Dialog.Portal>
		<Dialog.Overlay class="ridu-bulk-edit-overlay" />
		<Dialog.Content
			class="ridu-bulk-edit-drawer"
			dir={i18n.direction}
			onEscapeKeydown={(event) => pending && event.preventDefault()}
		>
			<main class="ridu-bulk-edit-main" bind:this={fieldsViewport}>
				<header class="ridu-bulk-edit-header">
					<Dialog.Title class="ridu-bulk-edit-title">
						{i18n.t("collections:bulkEditSelected", {
							count: selectedCount,
							formattedCount: i18n.formatNumber(selectedCount),
							label: selectedLabel,
						})}
					</Dialog.Title>
					<Dialog.Close
						type="button"
						class="ridu-bulk-edit-close"
						disabled={pending}
						aria-label={i18n.t("general:close")}
					>
						<XIcon aria-hidden="true" />
					</Dialog.Close>
				</header>
				<Dialog.Description class="ridu-bulk-edit-description">
					{i18n.t("collections:bulkEditDescription")}
				</Dialog.Description>

				<BulkEditFieldPicker
					fields={editorFields}
					value={selectedPaths}
					onValueChange={changeSelectedPaths}
					disabled={pending}
				/>

				{#if selectedFields.length > 0}
					<div class="ridu-bulk-edit-fields">
						<FieldLayout fields={selectedFields} {form} />
					</div>
				{/if}
			</main>

			<aside class="ridu-bulk-edit-sidebar">
				<Button
					disabled={pending ||
						selectedFields.length === 0 ||
						target === undefined ||
						target.ids.length === 0}
					aria-busy={pending}
					onclick={submit}
				>
					{pending ? i18n.t("collections:applying") : i18n.t("collections:applyChanges")}
				</Button>
			</aside>
		</Dialog.Content>
	</Dialog.Portal>
</Dialog.Root>
