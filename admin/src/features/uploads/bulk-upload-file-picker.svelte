<script lang="ts">
	import { getAdminI18n } from "@riducms/plugin";
	import FileUpIcon from "~icons/lucide/file-up";

	import type { BulkUploadController } from "@admin/features/uploads/bulk-upload-controller.svelte";

	let { controller, compact = false }: { controller: BulkUploadController; compact?: boolean } =
		$props();
	const i18n = getAdminI18n();
	let input: HTMLInputElement;
	let dragging = $state(false);
	const disabled = $derived(
		!controller.uploadEnabled ||
			!controller.canCreate ||
			controller.running ||
			controller.accessLoading
	);
	const acceptedTypes = $derived(controller.collection?.uploadSettings?.mimeTypes.join(",") ?? "");

	function choose(event: Event) {
		if (!(event.currentTarget instanceof HTMLInputElement)) return;
		controller.addFiles(Array.from(event.currentTarget.files ?? []));
		event.currentTarget.value = "";
	}

	function drop(event: DragEvent) {
		event.preventDefault();
		dragging = false;
		if (disabled) return;
		controller.addFiles(Array.from(event.dataTransfer?.files ?? []));
	}
</script>

<!-- File drops supplement the keyboard-accessible chooser. -->
<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
	class={[
		"ridu-bulk-upload-picker",
		{
			"ridu-bulk-upload-picker--compact": compact,
			"ridu-bulk-upload-picker--dragging": dragging,
		},
	]}
	ondragover={(event) => {
		event.preventDefault();
		if (!disabled) dragging = true;
	}}
	ondragleave={(event) => {
		if (!event.currentTarget.contains(event.relatedTarget as Node | null)) dragging = false;
	}}
	ondrop={drop}
>
	<input
		bind:this={input}
		type="file"
		multiple
		accept={acceptedTypes}
		{disabled}
		aria-hidden="true"
		tabindex="-1"
		onchange={choose}
	/>
	<button type="button" {disabled} onclick={() => input.click()}>
		<FileUpIcon aria-hidden="true" />
		<span>{i18n.t(compact ? "uploads:addFile" : "uploads:chooseFile")}</span>
	</button>
	{#if !compact}
		<p>{i18n.t("uploads:dropFile")}</p>
	{/if}
</div>
