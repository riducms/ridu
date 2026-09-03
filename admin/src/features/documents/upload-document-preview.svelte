<script lang="ts">
	import { getAdminI18n } from "@riducms/plugin";
	import FileIcon from "~icons/lucide/file";
	import PencilIcon from "~icons/lucide/pencil";

	import { Button } from "@admin/components/ui/button";
	import { Checkbox } from "@admin/components/ui/checkbox";
	import {
		Dialog,
		DialogContent,
		DialogDescription,
		DialogHeader,
		DialogTitle,
	} from "@admin/components/ui/dialog";
	import { Slider } from "@admin/components/ui/slider";
	import type { AdminDocument } from "@admin/core/api/admin-client";

	type Variant = {
		name: string;
		label: string;
		url: string;
		width?: number;
		height?: number;
		filesize?: number;
	};

	let {
		document,
		editable = false,
		updating = false,
		onUpdate,
	}: {
		document: AdminDocument;
		editable?: boolean;
		updating?: boolean;
		onUpdate?: (input: ImageEditInput) => Promise<boolean>;
	} = $props();
	const i18n = getAdminI18n();
	type ImageEditInput = {
		focalX: number;
		focalY: number;
		cropX: number;
		cropY: number;
		cropWidth: number;
		cropHeight: number;
	};

	const filename = $derived(stringValue(document.filename) ?? i18n.t("uploads:untitledAsset"));
	const mimeType = $derived(stringValue(document.mimeType));
	const url = $derived(stringValue(document.url));
	const alt = $derived(stringValue(document.alt) ?? filename);
	const width = $derived(numberValue(document.width));
	const height = $derived(numberValue(document.height));
	const storedFocalX = $derived(numberValue(document.focalX) ?? 50);
	const storedFocalY = $derived(numberValue(document.focalY) ?? 50);
	const storedCropX = $derived(numberValue(document.cropX) ?? 0);
	const storedCropY = $derived(numberValue(document.cropY) ?? 0);
	const storedCropWidth = $derived(numberValue(document.cropWidth) ?? 0);
	const storedCropHeight = $derived(numberValue(document.cropHeight) ?? 0);
	const isImage = $derived(mimeType?.startsWith("image/") === true);
	const variants = $derived(imageVariants(document, url, width, height));
	let selectedName = $state("original");
	let focalX = $state(50);
	let focalY = $state(50);
	let cropX = $state(0);
	let cropY = $state(0);
	let cropWidth = $state(0);
	let cropHeight = $state(0);
	let changed = $state(false);
	let sizesOpen = $state(false);
	let editorOpen = $state(false);
	const cropEnabled = $derived(cropWidth > 0 && cropHeight > 0);
	const selected = $derived(
		variants.find((variant) => variant.name === selectedName) ?? variants[0]
	);
	$effect(() => {
		if (changed) return;
		focalX = storedFocalX;
		focalY = storedFocalY;
		cropX = storedCropX;
		cropY = storedCropY;
		cropWidth = storedCropWidth;
		cropHeight = storedCropHeight;
	});

	function updateChanged() {
		changed =
			focalX !== storedFocalX ||
			focalY !== storedFocalY ||
			cropX !== storedCropX ||
			cropY !== storedCropY ||
			cropWidth !== storedCropWidth ||
			cropHeight !== storedCropHeight;
	}

	function setFocal(axis: "x" | "y", value: number) {
		if (!editable || updating) return;
		if (axis === "x") {
			focalX = cropEnabled ? Math.max(cropX, Math.min(cropX + cropWidth, value)) : value;
		} else {
			focalY = cropEnabled ? Math.max(cropY, Math.min(cropY + cropHeight, value)) : value;
		}
		updateChanged();
	}

	function setCrop(axis: "x" | "y" | "width" | "height", value: number) {
		if (!editable || updating) return;
		if (axis === "x") cropX = Math.min(value, 100 - cropWidth);
		if (axis === "y") cropY = Math.min(value, 100 - cropHeight);
		if (axis === "width") cropWidth = Math.min(value, 100 - cropX);
		if (axis === "height") cropHeight = Math.min(value, 100 - cropY);
		focalX = Math.max(cropX, Math.min(cropX + cropWidth, focalX));
		focalY = Math.max(cropY, Math.min(cropY + cropHeight, focalY));
		updateChanged();
	}

	function toggleCrop(enabled: boolean) {
		if (!editable || updating) return;
		if (enabled) {
			cropX = storedCropWidth > 0 ? storedCropX : 10;
			cropY = storedCropHeight > 0 ? storedCropY : 10;
			cropWidth = storedCropWidth > 0 ? storedCropWidth : 80;
			cropHeight = storedCropHeight > 0 ? storedCropHeight : 80;
			focalX = Math.max(cropX, Math.min(cropX + cropWidth, focalX));
			focalY = Math.max(cropY, Math.min(cropY + cropHeight, focalY));
		} else {
			cropX = 0;
			cropY = 0;
			cropWidth = 0;
			cropHeight = 0;
		}
		updateChanged();
	}

	function chooseFocal(event: MouseEvent) {
		if (!editable || updating || width === undefined || height === undefined) return;
		// Keyboard-generated clicks do not carry meaningful viewport coordinates. Treat activation as
		// the explicit centre action; arrow keys below provide precise adjustment.
		if (event.detail === 0) {
			setFocal("x", 50);
			setFocal("y", 50);
			return;
		}
		const bounds =
			event.currentTarget instanceof HTMLElement
				? event.currentTarget.getBoundingClientRect()
				: undefined;
		if (bounds === undefined) return;
		const scale = Math.min(bounds.width / width, bounds.height / height);
		const renderedWidth = width * scale;
		const renderedHeight = height * scale;
		const offsetX = (bounds.width - renderedWidth) / 2;
		const offsetY = (bounds.height - renderedHeight) / 2;
		setFocal("x", clamp(((event.clientX - bounds.left - offsetX) / renderedWidth) * 100));
		setFocal("y", clamp(((event.clientY - bounds.top - offsetY) / renderedHeight) * 100));
	}

	function adjustFocal(event: KeyboardEvent) {
		if (!editable || updating) return;
		const step = event.shiftKey ? 10 : 1;
		if (event.key === "ArrowLeft") setFocal("x", clamp(focalX - step));
		else if (event.key === "ArrowRight") setFocal("x", clamp(focalX + step));
		else if (event.key === "ArrowUp") setFocal("y", clamp(focalY - step));
		else if (event.key === "ArrowDown") setFocal("y", clamp(focalY + step));
		else return;
		event.preventDefault();
	}

	async function applyImageEdit() {
		if (
			onUpdate === undefined ||
			!(await onUpdate({ focalX, focalY, cropX, cropY, cropWidth, cropHeight }))
		)
			return;
		changed = false;
	}

	function startCropDrag(event: PointerEvent) {
		if (!cropEnabled || !editable || updating) return;
		event.preventDefault();
		event.stopPropagation();
		const target = event.currentTarget as HTMLElement;
		const parent = target.parentElement;
		if (parent === null) return;
		const bounds = parent.getBoundingClientRect();
		const startX = event.clientX;
		const startY = event.clientY;
		const originalX = cropX;
		const originalY = cropY;
		target.setPointerCapture(event.pointerId);
		const move = (next: PointerEvent) => {
			cropX = Math.max(
				0,
				Math.min(100 - cropWidth, originalX + ((next.clientX - startX) / bounds.width) * 100)
			);
			cropY = Math.max(
				0,
				Math.min(100 - cropHeight, originalY + ((next.clientY - startY) / bounds.height) * 100)
			);
			focalX = Math.max(cropX, Math.min(cropX + cropWidth, focalX));
			focalY = Math.max(cropY, Math.min(cropY + cropHeight, focalY));
			updateChanged();
		};
		const end = () => {
			target.removeEventListener("pointermove", move);
			target.removeEventListener("pointerup", end);
			target.removeEventListener("pointercancel", end);
		};
		target.addEventListener("pointermove", move);
		target.addEventListener("pointerup", end);
		target.addEventListener("pointercancel", end);
	}

	function imageVariants(
		document: AdminDocument,
		originalURL: string | undefined,
		originalWidth: number | undefined,
		originalHeight: number | undefined
	): Variant[] {
		const result: Variant[] = [];
		if (originalURL !== undefined) {
			result.push({
				name: "original",
				label: i18n.t("uploads:original"),
				url: originalURL,
				width: originalWidth,
				height: originalHeight,
				filesize: numberValue(document.filesize),
			});
		}
		if (!recordValue(document.sizes)) return result;
		for (const [name, raw] of Object.entries(document.sizes)) {
			if (!recordValue(raw)) continue;
			const variantURL = stringValue(raw.url);
			if (variantURL === undefined) continue;
			result.push({
				name,
				label: name.replaceAll("-", " "),
				url: variantURL,
				width: numberValue(raw.width),
				height: numberValue(raw.height),
				filesize: numberValue(raw.filesize),
			});
		}
		return result;
	}

	function stringValue(value: unknown) {
		return typeof value === "string" && value.length > 0 ? value : undefined;
	}

	function numberValue(value: unknown) {
		return typeof value === "number" && Number.isFinite(value) ? value : undefined;
	}

	function recordValue(value: unknown): value is Record<string, unknown> {
		return typeof value === "object" && value !== null && !Array.isArray(value);
	}

	function clamp(value: number) {
		return Math.max(0, Math.min(100, Math.round(value)));
	}

	function formatBytes(value: number | undefined) {
		if (value === undefined) return undefined;
		if (value < 1_024) return i18n.formatNumber(value, { style: "unit", unit: "byte" });
		if (value < 1_048_576)
			return i18n.formatNumber(value / 1_024, {
				maximumFractionDigits: 0,
				style: "unit",
				unit: "kilobyte",
			});
		return i18n.formatNumber(value / 1_048_576, {
			maximumFractionDigits: value < 10_485_760 ? 1 : 0,
			style: "unit",
			unit: "megabyte",
		});
	}

	function cropControlLabel(key: unknown) {
		if (key === "x") return i18n.t("uploads:left");
		if (key === "y") return i18n.t("uploads:top");
		if (key === "width") return i18n.t("uploads:width");
		return i18n.t("uploads:height");
	}
</script>

<section aria-label={i18n.t("uploads:assetPreview")} aria-busy={updating}>
	<div
		class="grid min-h-35 overflow-hidden rounded-[4px] border border-control-border bg-control sm:grid-cols-[140px_minmax(0,1fr)]"
	>
		<div class="grid min-h-28 place-items-center overflow-hidden bg-background sm:min-h-35">
			{#if isImage && url !== undefined}
				<img class="size-full object-cover" src={url} {alt} />
			{:else}
				<FileIcon class="size-7 text-foreground-faint" />
			{/if}
		</div>
		<div
			class="flex min-w-0 flex-col justify-center gap-3 border-t border-control-border p-4 sm:border-t-0 sm:border-s"
		>
			<div class="min-w-0">
				<p class="truncate text-[13px] font-medium text-foreground-strong">{filename}</p>
				<p class="mt-1 text-[11px] text-foreground-faint">
					{#if formatBytes(numberValue(document.filesize)) !== undefined}{formatBytes(
							numberValue(document.filesize)
						)} ·
					{/if}{#if width !== undefined && height !== undefined}{i18n.formatNumber(width)} × {i18n.formatNumber(
							height
						)} ·
					{/if}{mimeType ?? i18n.t("uploads:file")}
				</p>
			</div>
			<div class="flex flex-wrap gap-1.5">
				{#if variants.length > 1}
					<Button variant="secondary" size="xs" onclick={() => (sizesOpen = true)}>
						{i18n.t("uploads:previewSizes")}
					</Button>
				{/if}
				{#if editable && isImage}
					<Button variant="secondary" size="xs" onclick={() => (editorOpen = true)}>
						<PencilIcon class="size-3" />
						{i18n.t("uploads:editImage")}
					</Button>
				{/if}
			</div>
		</div>
	</div>

	<Dialog bind:open={sizesOpen}>
		<DialogContent class="h-[min(720px,calc(100vh-3rem))] sm:max-w-5xl">
			<DialogHeader>
				<DialogTitle>{i18n.t("uploads:sizesFor", { filename })}</DialogTitle>
				<DialogDescription class="sr-only">
					{i18n.t("uploads:previewGeneratedSizes")}
				</DialogDescription>
			</DialogHeader>
			<div class="grid min-h-0 flex-1 gap-5 lg:grid-cols-[minmax(0,1fr)_260px]">
				<div
					class="grid min-h-80 place-items-center overflow-hidden border border-control-border bg-background"
				>
					{#if selected !== undefined}
						<img class="max-h-full max-w-full object-contain" src={selected.url} {alt} />
					{:else}
						<FileIcon class="size-8 text-foreground-faint" />
					{/if}
				</div>
				<div
					class="grid content-start gap-2 overflow-y-auto"
					aria-label={i18n.t("uploads:imageSizes")}
				>
					{#each variants as variant (variant.name)}
						<button
							type="button"
							class="grid grid-cols-[64px_minmax(0,1fr)] items-center gap-3 rounded-[3px] border border-control-border bg-control p-2 text-start outline-none hover:border-control-border-hover focus-visible:border-ring aria-pressed:border-primary"
							aria-label={variant.label}
							aria-pressed={selectedName === variant.name}
							onclick={() => (selectedName = variant.name)}
						>
							<img class="size-16 object-cover" src={variant.url} alt="" />
							<span class="min-w-0">
								<span class="block truncate text-[12.5px] font-medium capitalize text-foreground">
									{variant.label}
								</span>
								<span class="mt-1 block text-[10px] text-foreground-faint">
									{#if variant.width !== undefined && variant.height !== undefined}{i18n.formatNumber(
											variant.width
										)} ×
										{i18n.formatNumber(
											variant.height
										)}{/if}{#if formatBytes(variant.filesize) !== undefined}
										· {formatBytes(variant.filesize)}{/if}
								</span>
							</span>
						</button>
					{/each}
				</div>
			</div>
		</DialogContent>
	</Dialog>

	<Dialog bind:open={editorOpen}>
		<DialogContent class="max-h-[calc(100vh-3rem)] overflow-y-auto sm:max-w-5xl">
			<DialogHeader>
				<DialogTitle>{i18n.t("uploads:editFilename", { filename })}</DialogTitle>
				<DialogDescription>
					{i18n.t("uploads:imageEditDescription")}
				</DialogDescription>
			</DialogHeader>
			<div class="grid gap-5 lg:grid-cols-[minmax(0,1fr)_320px]">
				<button
					type="button"
					class="relative grid aspect-[3/2] w-full place-items-center overflow-hidden rounded-[3px] border border-control-border bg-background text-start disabled:cursor-default"
					disabled={!editable || updating}
					onclick={chooseFocal}
					onkeydown={adjustFocal}
					aria-describedby="ridu-focal-instructions"
					aria-keyshortcuts="ArrowLeft ArrowRight ArrowUp ArrowDown"
					aria-label={i18n.t("uploads:chooseFocalPoint")}
				>
					{#if url !== undefined}
						<img class="pointer-events-none size-full object-contain" src={url} {alt} />
					{/if}
					<span
						class="pointer-events-none absolute size-5 -translate-x-1/2 -translate-y-1/2 rounded-full border border-media-foreground/80 ring-1 ring-media-overlay"
						style="left: {focalX}%; top: {focalY}%"
						aria-hidden="true"
					>
						<span
							class="absolute top-1/2 left-1/2 h-px w-2.5 -translate-1/2 bg-media-foreground/80"
						></span>
						<span
							class="absolute top-1/2 left-1/2 h-2.5 w-px -translate-1/2 bg-media-foreground/80"
						></span>
					</span>
					{#if cropEnabled}
						<span
							class="absolute cursor-move touch-none border-2 border-primary bg-transparent shadow-[0_0_0_9999px_var(--media-overlay)]"
							style="left: {cropX}%; top: {cropY}%; width: {cropWidth}%; height: {cropHeight}%"
							onpointerdown={startCropDrag}
							role="presentation"
						></span>
					{/if}
				</button>
				<div
					class="grid content-start gap-3 rounded-[3px] border border-control-border bg-control p-4"
				>
					<div class="flex items-start justify-between gap-4">
						<div>
							<p class="text-[12.5px] font-medium text-foreground">
								{i18n.t("uploads:focalPoint")}
							</p>
							<p class="mt-1 text-[11px] leading-4 text-foreground-faint">
								{i18n.t("uploads:focalPointDescription")}
							</p>
						</div>
						<Button size="sm" disabled={!changed || updating} onclick={applyImageEdit}>
							{updating ? i18n.t("uploads:regenerating") : i18n.t("uploads:applyImageEdit")}
						</Button>
					</div>
					<div class="grid gap-3 sm:grid-cols-2">
						<label class="grid gap-1.5 text-[11px] text-foreground-sub">
							<span class="flex justify-between">
								<span>{i18n.t("uploads:horizontal")}</span>
								<span>{i18n.formatNumber(focalX / 100, { style: "percent" })}</span>
							</span>
							<Slider
								value={focalX}
								min={0}
								max={100}
								disabled={updating}
								label={i18n.t("uploads:horizontalFocalPoint")}
								onValueChange={(value) => setFocal("x", value)}
							/>
						</label>
						<label class="grid gap-1.5 text-[11px] text-foreground-sub">
							<span class="flex justify-between">
								<span>{i18n.t("uploads:vertical")}</span>
								<span>{i18n.formatNumber(focalY / 100, { style: "percent" })}</span>
							</span>
							<Slider
								value={focalY}
								min={0}
								max={100}
								disabled={updating}
								label={i18n.t("uploads:verticalFocalPoint")}
								onValueChange={(value) => setFocal("y", value)}
							/>
						</label>
					</div>
					<div class="border-t border-control-border pt-3">
						<div class="flex items-center gap-2 text-[11px] text-foreground-sub">
							<Checkbox
								checked={cropEnabled}
								disabled={updating}
								aria-label={i18n.t("uploads:enableCrop")}
								onCheckedChange={toggleCrop}
							/>
							<span>{i18n.t("uploads:enableCrop")}</span>
						</div>
						{#if cropEnabled}
							<p class="mt-2 text-[10.5px] text-foreground-faint">
								{i18n.t("uploads:cropDescription")}
							</p>
							<div class="mt-3 grid gap-3 sm:grid-cols-2">
								{#each [["x", "Left", cropX, 100 - cropWidth], ["y", "Top", cropY, 100 - cropHeight], ["width", "Width", cropWidth, 100 - cropX], ["height", "Height", cropHeight, 100 - cropY]] as control}
									<label class="grid gap-1.5 text-[11px] text-foreground-sub">
										<span class="flex justify-between">
											<span>{cropControlLabel(control[0])}</span>
											<span>
												{i18n.formatNumber(Math.round(Number(control[2])) / 100, {
													style: "percent",
												})}
											</span>
										</span>
										<Slider
											value={Number(control[2])}
											min={control[0] === "width" || control[0] === "height" ? 5 : 0}
											max={Number(control[3])}
											disabled={updating}
											label={i18n.t("uploads:cropBoundary", {
												label: cropControlLabel(control[0]),
											})}
											onValueChange={(value) =>
												setCrop(control[0] as "x" | "y" | "width" | "height", value)}
										/>
									</label>
								{/each}
							</div>
						{/if}
					</div>
				</div>
			</div>
			<p id="ridu-focal-instructions" class="sr-only">
				{i18n.t("uploads:focalKeyboardInstructions")}
			</p>
			<p class="sr-only" aria-live="polite" aria-atomic="true" data-focal-position-status>
				{i18n.t("uploads:focalPosition", {
					x: i18n.formatNumber(focalX / 100, { style: "percent" }),
					y: i18n.formatNumber(focalY / 100, { style: "percent" }),
				})}
			</p>
		</DialogContent>
	</Dialog>
</section>
