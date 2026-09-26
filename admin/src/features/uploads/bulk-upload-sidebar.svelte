<script lang="ts">
	import { getAdminI18n } from "@riducms/plugin";
	import FileIcon from "~icons/lucide/file";
	import XIcon from "~icons/lucide/x";

	import type {
		BulkUploadController,
		BulkUploadStatus,
	} from "@admin/features/uploads/bulk-upload-controller.svelte";
	import BulkUploadFilePicker from "@admin/features/uploads/bulk-upload-file-picker.svelte";
	import UploadThumbnail from "@admin/features/uploads/upload-thumbnail.svelte";

	let { controller }: { controller: BulkUploadController } = $props();
	const i18n = getAdminI18n();

	function formatBytes(value: number) {
		if (value < 1_024) return i18n.formatNumber(value, { style: "unit", unit: "byte" });
		if (value < 1_048_576)
			return i18n.formatNumber(value / 1_024, {
				maximumFractionDigits: 0,
				style: "unit",
				unit: "kilobyte",
			});
		return i18n.formatNumber(value / 1_048_576, {
			maximumFractionDigits: 1,
			style: "unit",
			unit: "megabyte",
		});
	}

	function statusLabel(status: BulkUploadStatus) {
		const labels = {
			queued: "uploads:statusQueued",
			uploading: "uploads:statusUploading",
			complete: "uploads:statusComplete",
			failed: "uploads:statusFailed",
			uncertain: "uploads:statusUncertain",
		} as const;
		return i18n.t(labels[status]);
	}

	function queueSummary() {
		const current = controller.queue.find((item) => item.status === "uploading");
		return i18n.t("uploads:queueSummary", {
			current:
				current === undefined
					? ""
					: i18n.t("uploads:uploadingFilename", { filename: current.sourceFile.name }),
			complete: i18n.formatNumber(controller.completedCount),
			queued: i18n.formatNumber(controller.queue.filter((item) => item.status === "queued").length),
			failed: i18n.formatNumber(controller.failedCount),
			uncertain: i18n.formatNumber(controller.uncertainCount),
		});
	}
</script>

<aside class="ridu-bulk-upload-sidebar">
	<header class="ridu-bulk-upload-sidebar__header">
		<strong>
			{i18n.t(controller.queue.length === 1 ? "uploads:fileToUpload" : "uploads:filesToUpload", {
				count: controller.queue.length,
				formattedCount: i18n.formatNumber(controller.queue.length),
			})}
		</strong>
		<BulkUploadFilePicker {controller} compact />
	</header>
	<p class="ridu-bulk-upload-status" role="status" aria-live="polite" aria-atomic="true">
		{queueSummary()}
	</p>

	<div
		class="ridu-bulk-upload-sidebar__list"
		role="list"
		aria-label={i18n.t("uploads:uploadQueue")}
	>
		{#each controller.queue as item, index (item.id)}
			<div
				class={[
					"ridu-bulk-upload-file",
					{
						"ridu-bulk-upload-file--active": index === controller.activeIndex,
						"ridu-bulk-upload-file--error": item.issueCount > 0,
					},
				]}
				role="listitem"
			>
				<button
					type="button"
					class="ridu-bulk-upload-file__select"
					aria-current={index === controller.activeIndex ? "true" : undefined}
					title={item.issueLabels.join(", ") || undefined}
					onclick={() => controller.select(index)}
				>
					<span class="ridu-bulk-upload-file__thumbnail" aria-hidden="true">
						{#if item.upload.mimeType.startsWith("image/") && item.upload.present}
							<UploadThumbnail draft={item.upload} />
						{:else}
							<FileIcon />
						{/if}
					</span>
					<span class="ridu-bulk-upload-file__details">
						<span class="ridu-bulk-upload-file__name">{item.sourceFile.name}</span>
						<span>{formatBytes(item.sourceFile.size)}</span>
					</span>
					{#if item.issueCount > 0}
						<span class="ridu-bulk-upload-file__issues" aria-label={item.issueLabels.join(", ")}>
							{item.issueCount}
						</span>
					{:else}
						<span class="ridu-bulk-upload-file__status">{statusLabel(item.status)}</span>
					{/if}
				</button>
				<button
					type="button"
					class="ridu-bulk-upload-file__remove"
					aria-label={i18n.t("uploads:removeFilename", { filename: item.sourceFile.name })}
					disabled={controller.running}
					onclick={() => controller.remove(item.id)}
				>
					<XIcon />
				</button>
			</div>
		{/each}
	</div>
</aside>
