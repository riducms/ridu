<script lang="ts">
	import { getAdminI18n } from "@riducms/plugin";
	import type { AdminDocument } from "@admin/core/api/admin-client";
	import type { Component } from "svelte";

	type ImageEditInput = {
		focalX: number;
		focalY: number;
		cropX: number;
		cropY: number;
		cropWidth: number;
		cropHeight: number;
	};
	type UploadDocumentPreviewProps = {
		document: AdminDocument;
		editable?: boolean;
		updating?: boolean;
		onUpdate?: (input: ImageEditInput) => Promise<boolean>;
	};

	let props: UploadDocumentPreviewProps = $props();
	const i18n = getAdminI18n();
	let Preview = $state<Component<UploadDocumentPreviewProps> | undefined>(undefined);

	void import("@admin/features/documents/upload-document-preview.svelte").then(
		({ default: component }) => (Preview = component)
	);
</script>

{#if Preview !== undefined}
	<Preview {...props} />
{:else}
	<section
		class="grid min-h-48 place-items-center"
		aria-busy="true"
		aria-label={i18n.t("uploads:loadingAssetPreview")}
	>
		<p class="text-[12px] text-foreground-faint">{i18n.t("uploads:loadingAssetPreview")}</p>
	</section>
{/if}
