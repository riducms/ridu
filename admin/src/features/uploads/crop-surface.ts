import CropperCanvas from "@cropper/element-canvas";
import CropperSelection from "@cropper/element-selection";
import CropperHandle from "@cropper/element-handle";
import type { UploadImageEdit } from "@riducms/protocol";
import type { UploadSource } from "@admin/features/uploads/upload-draft.svelte";

const directions = ["n", "ne", "e", "se", "s", "sw", "w", "nw"] as const;
const arrows: Record<string, [number, number]> = {
	ArrowLeft: [-1, 0],
	ArrowRight: [1, 0],
	ArrowUp: [0, -1],
	ArrowDown: [0, 1],
};

/** Owns only the crop interaction DOM. Document state and image bytes stay outside Cropper. */
export function mountCropSurface(
	host: HTMLElement,
	source: UploadSource,
	options: {
		getEdit: () => UploadImageEdit;
		onchange: (edit: UploadImageEdit) => void;
		label: (action: string) => string;
	}
) {
	CropperCanvas.$define();
	CropperSelection.$define();
	CropperHandle.$define();

	const canvas = new CropperCanvas();
	canvas.className = "ridu-crop-canvas";
	canvas.style.aspectRatio = `${source.width} / ${source.height}`;
	const image = new Image();
	image.src = source.url;
	image.alt = "";
	image.draggable = false;
	const draw = new CropperHandle();
	draw.action = "select";
	draw.plain = true;
	const selection = new CropperSelection();
	selection.className = "ridu-crop-selection";
	selection.movable = true;
	selection.resizable = true;
	selection.precise = true;
	// Cropper's keyboard option installs document-wide Delete/arrow handlers.
	// Scope accessible keyboard interaction to our focused, labelled handles instead.
	selection.keyboard = false;

	for (const direction of ["move", ...directions]) {
		const handle = new CropperHandle();
		handle.action = direction === "move" ? "move" : `${direction}-resize`;
		handle.plain = direction === "move";
		handle.tabIndex = 0;
		handle.setAttribute("role", "button");
		handle.setAttribute("aria-label", options.label(direction));
		handle.themeColor = "var(--ink-strong)";
		if (direction !== "move") {
			// Cropper centers resize targets outside the selection, but its canvas clips them at image edges.
			if (direction.includes("n")) handle.style.top = "0";
			if (direction.includes("s")) handle.style.bottom = "0";
			if (direction.includes("e")) handle.style.right = "0";
			if (direction.includes("w")) handle.style.left = "0";
			// Full-edge targets overlap the corners; diagonal targets must win the pointer hit test.
			if (direction.length === 2) handle.style.zIndex = "1";
		}
		selection.append(handle);
	}
	canvas.append(image, draw, selection);
	host.append(canvas);
	for (const handle of selection.children) {
		if (handle instanceof CropperHandle && handle.action !== "move") {
			handle.$addStyles(
				":host::after { width: 7px; height: 7px; border: 1px solid var(--ink-strong); background: var(--background); }"
			);
		}
	}

	let synchronizing = false;
	const sync = () => {
		const edit = options.getEdit();
		if (!canvas.clientWidth || !canvas.clientHeight) return;
		synchronizing = true;
		selection.$change(
			(edit.cropX / 100) * canvas.clientWidth,
			(edit.cropY / 100) * canvas.clientHeight,
			((edit.cropWidth || 100) / 100) * canvas.clientWidth,
			((edit.cropHeight || 100) / 100) * canvas.clientHeight
		);
		synchronizing = false;
	};
	const change = (event: Event) => {
		if (!(event instanceof CustomEvent) || synchronizing) return;
		const rect: { x: number; y: number; width: number; height: number } = event.detail;
		const width = canvas.clientWidth,
			height = canvas.clientHeight;
		if (
			!width ||
			!height ||
			rect.x < -0.01 ||
			rect.y < -0.01 ||
			rect.x + rect.width > width + 0.01 ||
			rect.y + rect.height > height + 0.01 ||
			rect.width < width / source.width ||
			rect.height < height / source.height
		) {
			event.preventDefault();
			return;
		}
		const next = {
			...options.getEdit(),
			cropX: Math.max(0, (rect.x / width) * 100),
			cropY: Math.max(0, (rect.y / height) * 100),
			cropWidth: (rect.width / width) * 100,
			cropHeight: (rect.height / height) * 100,
		};
		const previous = options.getEdit();
		if (
			["cropX", "cropY", "cropWidth", "cropHeight"].some(
				(name) =>
					Math.abs(next[name as keyof UploadImageEdit] - previous[name as keyof UploadImageEdit]) >
					0.00001
			)
		)
			options.onchange(next);
	};
	const keydown = (event: KeyboardEvent) => {
		if (!(event.target instanceof CropperHandle)) return;
		const offset = arrows[event.key];
		if (!offset) return;
		event.preventDefault();
		const x = ((offset[0] * canvas.clientWidth) / source.width) * (event.shiftKey ? 10 : 1);
		const y = ((offset[1] * canvas.clientHeight) / source.height) * (event.shiftKey ? 10 : 1);
		if (event.target.action === "move") selection.$move(x, y);
		else selection.$resize(event.target.action, x, y);
	};
	selection.addEventListener("change", change);
	canvas.addEventListener("keydown", keydown);
	const resize = new ResizeObserver(sync);
	resize.observe(canvas);
	sync();

	return {
		sync,
		dispose() {
			resize.disconnect();
			selection.removeEventListener("change", change);
			canvas.removeEventListener("keydown", keydown);
			canvas.remove();
		},
	};
}
