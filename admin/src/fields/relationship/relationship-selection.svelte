<script lang="ts">
	import { useSortable } from "@dnd-kit-svelte/svelte/sortable";
	import { getAdminI18n } from "@riducms/plugin";
	import type { AdminDocument } from "@admin/core/api/admin-client";
	import FileIcon from "~icons/lucide/file";
	import PencilIcon from "~icons/lucide/pencil";
	import XIcon from "~icons/lucide/x";
	import GripVerticalIcon from "~icons/lucide/grip-vertical";
	import "@admin/fields/relationship/relationship.scss";

	let {
		id,
		index,
		label,
		document,
		upload = false,
		sortable = false,
		readOnly = false,
		blocked = false,
		onEdit,
		onRemove,
	}: {
		id: string;
		index: number;
		label: string;
		document?: AdminDocument;
		upload?: boolean;
		sortable?: boolean;
		readOnly?: boolean;
		blocked?: boolean;
		onEdit: () => void;
		onRemove: () => void;
	} = $props();

	const i18n = getAdminI18n();
	const sortableItem = useSortable({
		id: () => id,
		index: () => index,
		disabled: () => !sortable || readOnly || blocked,
	});
	const metadata = $derived(
		[
			typeof document?.filesize === "number"
				? i18n.formatNumber(document.filesize, { style: "unit", unit: "byte", unitDisplay: "long" })
				: undefined,
			typeof document?.width === "number" && typeof document?.height === "number"
				? `${document.width} × ${document.height}`
				: undefined,
			typeof document?.mimeType === "string" ? document.mimeType : undefined,
		]
			.filter(Boolean)
			.join(" — ")
	);
</script>

<!-- dnd-kit's popover reset belongs on the drag container, outside the styled surface. -->
<div
	class="ridu-relationship-selection"
	data-dragging={sortableItem.isDragging.current}
	{@attach sortable && sortableItem.ref}
>
	<div
		class={[
			upload ? "ridu-upload-reference" : "ridu-relationship-chip",
			{ "ridu-upload-reference--compact": upload && sortable },
		]}
	>
		{#if sortable}
			<button
				type="button"
				class="ridu-relationship-grip"
				hidden={readOnly}
				disabled={blocked || readOnly}
				aria-label={i18n.t("fields:drag", { label })}
				{@attach sortableItem.handleRef}
			>
				<GripVerticalIcon />
			</button>
		{/if}
		{#if upload}
			<div class="ridu-upload-reference__thumbnail">
				{#if typeof document?.mimeType === "string" && document.mimeType.startsWith("image/") && typeof document.url === "string"}
					<img src={document.url} alt="" />
				{:else}
					<FileIcon />
				{/if}
			</div>
		{/if}
		<div class="ridu-relationship-label">
			<button type="button" class="ridu-relationship-title" disabled={blocked} onclick={onEdit}>
				{label}
			</button>
			{#if upload && !sortable && metadata}
				<span class="ridu-upload-reference__metadata">
					{metadata}
				</span>
			{/if}
		</div>
		<button
			type="button"
			class="ridu-relationship-action"
			disabled={blocked}
			aria-label={i18n.t("fields:edit", { label })}
			onclick={onEdit}
		>
			<PencilIcon />
		</button>
		{#if !readOnly}
			<button
				type="button"
				class="ridu-relationship-action"
				disabled={blocked}
				aria-label={i18n.t("fields:remove", { label })}
				onclick={onRemove}
			>
				<XIcon />
			</button>
		{/if}
	</div>
</div>
