<script lang="ts">
	import type { UploadDraft } from "@admin/features/uploads/upload-draft.svelte";
	let { draft }: { draft: UploadDraft } = $props();

	function drawThumbnail(target: HTMLCanvasElement) {
		const source = draft.source;
		const crop = draft.currentImage;
		if (!source) return;
		let cancelled = false;
		const image = new Image();
		image.src = source.url;
		image
			.decode()
			.then(() => {
				if (cancelled) return;
				const width = ((crop.cropWidth || 100) / 100) * source.width;
				const height = ((crop.cropHeight || 100) / 100) * source.height;
				target.width = Math.min(320, width);
				target.height = (target.width * height) / width;
				target
					.getContext("2d")
					?.drawImage(
						image,
						(crop.cropX / 100) * source.width,
						(crop.cropY / 100) * source.height,
						width,
						height,
						0,
						0,
						target.width,
						target.height
					);
			})
			.catch(() => {
				/* The owning draft reports decoding failures. */
			});
		return () => {
			cancelled = true;
		};
	}
</script>

{#if draft.source}
	<canvas {@attach drawThumbnail} aria-label={draft.filename}></canvas>
{:else}
	<img src={draft.previewURL} alt={draft.filename} />
{/if}
