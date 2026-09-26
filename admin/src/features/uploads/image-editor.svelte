<script lang="ts">
	import { untrack } from "svelte";
	import { Button, Input } from "@riducms/ui";
	import { getAdminI18n } from "@riducms/plugin";
	import type { UploadImageEdit } from "@riducms/protocol";
	import type { UploadSource } from "@admin/features/uploads/upload-draft.svelte";
	import { mountCropSurface } from "@admin/features/uploads/crop-surface";

	let {
		source,
		initial,
		onapply,
		oncancel,
	}: {
		source: UploadSource;
		initial: UploadImageEdit;
		onapply: (edit: UploadImageEdit) => void;
		oncancel: () => void;
	} = $props();
	const i18n = getAdminI18n();
	const instructionsID = $props.id();
	// Each mounted editor is an isolated session; Cancel leaves its parent draft untouched.
	// svelte-ignore state_referenced_locally
	let edit = $state({ ...initial });
	let surface: HTMLDivElement;
	const width = $derived(Math.round(((edit.cropWidth || 100) / 100) * source.width));
	const height = $derived(Math.round(((edit.cropHeight || 100) / 100) * source.height));

	function attachCrop(node: HTMLElement) {
		const currentSource = source;
		const adapter = untrack(() =>
			mountCropSurface(node, currentSource, {
				getEdit: () => edit,
				onchange: (next) => {
					edit = constrainFocal(next);
				},
				label: (direction) => i18n.t("uploads:cropHandle", { direction }),
			})
		);
		$effect(() => {
			// The adapter is an external DOM system. Numeric inputs and resets synchronize it here.
			edit.cropX;
			edit.cropY;
			edit.cropWidth;
			edit.cropHeight;
			adapter.sync();
		});
		return () => adapter.dispose();
	}

	function constrainFocal(next: UploadImageEdit) {
		return {
			...next,
			focalX: Math.max(next.cropX, Math.min(next.cropX + (next.cropWidth || 100), next.focalX)),
			focalY: Math.max(next.cropY, Math.min(next.cropY + (next.cropHeight || 100), next.focalY)),
		};
	}

	function setFocal(x: number, y: number) {
		if (Number.isFinite(x) && Number.isFinite(y))
			edit = constrainFocal({ ...edit, focalX: x, focalY: y });
	}

	function resize(axis: "width" | "height", value: number | undefined) {
		if (value === undefined || !Number.isFinite(value)) return;
		if (!edit.cropWidth) {
			edit.cropWidth = 100;
			edit.cropHeight = 100;
		}
		if (axis === "width")
			edit.cropWidth = Math.min(100 - edit.cropX, (Math.max(1, value) / source.width) * 100);
		else edit.cropHeight = Math.min(100 - edit.cropY, (Math.max(1, value) / source.height) * 100);
		edit = constrainFocal(edit);
	}

	function focalPointer(event: PointerEvent) {
		event.preventDefault();
		event.stopPropagation();
		if (!(event.currentTarget instanceof HTMLButtonElement)) return;
		const button = event.currentTarget;
		if (event.type === "pointerdown") button.setPointerCapture(event.pointerId);
		if (!button.hasPointerCapture(event.pointerId)) return;
		const bounds = surface.getBoundingClientRect();
		setFocal(
			((event.clientX - bounds.left) / bounds.width) * 100,
			((event.clientY - bounds.top) / bounds.height) * 100
		);
	}

	function focalKey(event: KeyboardEvent) {
		const step = event.shiftKey ? 10 : 1;
		if (event.key === "ArrowLeft") edit.focalX = Math.max(0, edit.focalX - step);
		else if (event.key === "ArrowRight") edit.focalX = Math.min(100, edit.focalX + step);
		else if (event.key === "ArrowUp") edit.focalY = Math.max(0, edit.focalY - step);
		else if (event.key === "ArrowDown") edit.focalY = Math.min(100, edit.focalY + step);
		else if (event.key === "Enter" || event.key === " ") {
			edit.focalX = 50;
			edit.focalY = 50;
		} else return;
		edit = constrainFocal(edit);
		event.preventDefault();
	}
</script>

<div class="ridu-image-editor-actions">
	<Button variant="outline" onclick={oncancel}>{i18n.t("general:cancel")}</Button>
	<Button onclick={() => onapply({ ...edit })}>{i18n.t("uploads:applyImageEdit")}</Button>
</div>

<p class="ridu-upload-description" role="status" aria-live="polite" aria-atomic="true">
	{i18n.t("uploads:cropPosition", {
		width,
		height,
		x: Math.round((edit.cropX * source.width) / 100),
		y: Math.round((edit.cropY * source.height) / 100),
	})}
	{i18n.t("uploads:focalPosition", {
		x: `${Math.round(edit.focalX)}%`,
		y: `${Math.round(edit.focalY)}%`,
	})}
</p>

<div class="ridu-image-editor">
	<div class="ridu-image-editor-preview">
		<div class="ridu-crop-surface" bind:this={surface}>
			<div {@attach attachCrop}></div>
			<button
				type="button"
				class="ridu-focal-point"
				style:left={`${edit.focalX}%`}
				style:top={`${edit.focalY}%`}
				aria-label={i18n.t("uploads:focalPoint")}
				aria-describedby={instructionsID}
				onpointerdown={focalPointer}
				onpointermove={focalPointer}
				onkeydown={focalKey}
			>
				+
			</button>
		</div>
	</div>

	<div class="ridu-image-editor-controls">
		<section>
			<header>
				<h3>{i18n.t("uploads:crop")}</h3>
				<Button
					size="xs"
					variant="ghost"
					onclick={() => {
						edit = { ...edit, cropX: 0, cropY: 0, cropWidth: 0, cropHeight: 0 };
					}}
				>
					{i18n.t("uploads:reset")}
				</Button>
			</header>
			<p>{i18n.t("uploads:cropDescription")}</p>
			<div class="ridu-image-editor-pair">
				<label>
					{i18n.t("uploads:widthPixels")}<Input
						type="number"
						min="1"
						max={source.width}
						bind:value={
							() => width,
							(value) => resize("width", value === undefined ? undefined : Number(value))
						}
					/>
				</label>
				<label>
					{i18n.t("uploads:heightPixels")}<Input
						type="number"
						min="1"
						max={source.height}
						bind:value={
							() => height,
							(value) => resize("height", value === undefined ? undefined : Number(value))
						}
					/>
				</label>
			</div>
		</section>

		<section>
			<header>
				<h3>{i18n.t("uploads:focalPoint")}</h3>
				<Button
					size="xs"
					variant="ghost"
					onclick={() => {
						setFocal(50, 50);
					}}
				>
					{i18n.t("uploads:reset")}
				</Button>
			</header>
			<p>{i18n.t("uploads:focalPointDescription")}</p>
			<p class="ridu-upload-description" id={instructionsID}>
				{i18n.t("uploads:focalKeyboardInstructions")}
			</p>
			<div class="ridu-image-editor-pair">
				<label>
					X %<Input
						type="number"
						min="0"
						max="100"
						bind:value={
							() => Math.round(edit.focalX),
							(value) => {
								if (value !== undefined) setFocal(Number(value), edit.focalY);
							}
						}
					/>
				</label>
				<label>
					Y %<Input
						type="number"
						min="0"
						max="100"
						bind:value={
							() => Math.round(edit.focalY),
							(value) => {
								if (value !== undefined) setFocal(edit.focalX, Number(value));
							}
						}
					/>
				</label>
			</div>
		</section>
	</div>
</div>
