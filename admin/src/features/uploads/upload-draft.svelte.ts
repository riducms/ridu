import type { AdminI18n } from "@riducms/plugin";
import type { AdminClient, AdminDocument } from "@admin/core/api/admin-client";
import { type SchemaUploadSettings, type UploadImageEdit } from "@riducms/protocol";

export interface UploadSource {
	url: string;
	width: number;
	height: number;
}

export const originalImageEdit = (): UploadImageEdit => ({
	focalX: 50,
	focalY: 50,
	cropX: 0,
	cropY: 0,
	cropWidth: 0,
	cropHeight: 0,
});

export function documentImageEdit(document?: AdminDocument): UploadImageEdit {
	const edit = originalImageEdit();
	for (const field of Object.keys(edit) as (keyof UploadImageEdit)[]) {
		const value = document?.[field];
		if (typeof value === "number") edit[field] = value;
	}
	return edit;
}

/** The document owns this draft; an editor dialog works on a separate copy. */
export class UploadDraft {
	#document = $state.raw<AdminDocument>();
	#file = $state.raw<File>();
	#filename = $state("");
	#image = $state.raw<UploadImageEdit>();
	#source = $state.raw<UploadSource>();
	#removed = $state(false);
	#busy = $state(false);
	#editingImage = $state(false);
	#error = $state<string>();
	#request?: AbortController;
	#generation = 0;
	#revision = 0;

	constructor(
		private readonly client: AdminClient,
		private readonly collection: () => string,
		private readonly i18n: AdminI18n
	) {}

	get revision() {
		return this.#revision;
	}

	get document() {
		return this.#document;
	}

	get file() {
		return this.#file;
	}

	get filename() {
		return this.#filename;
	}

	get image() {
		return this.#image;
	}

	get source() {
		return this.#source;
	}

	get busy() {
		return this.#busy;
	}

	get editingImage() {
		return this.#editingImage;
	}

	get error() {
		return this.#error;
	}

	get removed() {
		return this.#removed;
	}

	get present() {
		return !this.#removed && (this.#file !== undefined || this.#document !== undefined);
	}

	get mimeType() {
		return this.#file?.type ?? String(this.#document?.mimeType ?? "");
	}

	get editableImage() {
		return this.mimeType === "image/jpeg" || this.mimeType === "image/png";
	}

	get previewURL() {
		return this.#source?.url ?? String(this.#document?.url ?? "");
	}

	get currentImage() {
		return this.#image ?? documentImageEdit(this.#file ? undefined : this.#document);
	}

	get dirty() {
		return (
			this.#file !== undefined ||
			this.#removed ||
			this.#image !== undefined ||
			this.#filename !== String(this.#document?.filename ?? "")
		);
	}

	setFilename = (name: string) => {
		this.#revision++;
		this.#filename = name;
	};

	reset = (document?: AdminDocument) => {
		this.#revision++;
		this.dispose();
		this.#document = document;
		this.#file = undefined;
		this.#filename = String(document?.filename ?? "");
		this.#image = undefined;
		this.#removed = false;
		this.#busy = false;
		this.#error = undefined;
	};

	remove = () => {
		this.#revision++;
		this.dispose();
		this.#file = undefined;
		this.#image = undefined;
		this.#filename = "";
		this.#removed = this.#document !== undefined;
		this.#error = undefined;
		this.#busy = false;
	};

	select = async (file: File, settings: SchemaUploadSettings) => {
		this.cancelPending();
		const generation = this.#generation;
		this.#busy = true;
		this.#error = undefined;
		try {
			if (file.size > settings.maxFileSize) throw new Error(this.i18n.t("uploads:fileTooLarge"));
			if (
				!settings.mimeTypes.some(
					(type) =>
						type === file.type || (type.endsWith("/*") && file.type.startsWith(type.slice(0, -1)))
				)
			) {
				throw new Error(this.i18n.t("uploads:fileTypeDenied"));
			}
			const url = URL.createObjectURL(file);
			let source: UploadSource;
			try {
				source = await imageSource(url, file.type);
			} catch (cause) {
				URL.revokeObjectURL(url);
				throw cause;
			}
			if (generation !== this.#generation) {
				URL.revokeObjectURL(url);
				return;
			}
			if (this.#source) URL.revokeObjectURL(this.#source.url);
			this.#revision++;
			this.#source = source;
			this.#file = file;
			this.#filename = file.name;
			this.#image = undefined;
			this.#removed = false;
		} catch (cause) {
			if (generation === this.#generation)
				this.#error =
					cause instanceof Error ? cause.message : this.i18n.t("uploads:fileOpenFailed");
		} finally {
			if (generation === this.#generation) this.#busy = false;
		}
	};

	fromURL = async (url: string, settings: SchemaUploadSettings) => {
		this.#request?.abort();
		const request = new AbortController();
		this.#request = request;
		this.#busy = true;
		this.#error = undefined;
		try {
			const file = await this.client.previewUploadFromURL(this.collection(), url, {
				id: this.#document?.id,
				signal: request.signal,
			});
			if (!request.signal.aborted)
				await this.select(new File([file.blob], file.filename, { type: file.blob.type }), settings);
		} catch (cause) {
			if (!request.signal.aborted)
				this.#error =
					cause instanceof Error ? cause.message : this.i18n.t("uploads:fileDownloadFailed");
		} finally {
			if (this.#request === request) {
				this.#request = undefined;
				this.#busy = false;
			}
		}
	};

	loadSource = async () => {
		if (this.#source || !this.#document || !this.editableImage) return;
		const request = new AbortController();
		this.#request?.abort();
		this.#request = request;
		this.#busy = true;
		this.#error = undefined;
		try {
			const blob = await this.client.readUploadSource(this.collection(), this.#document.id, {
				signal: request.signal,
			});
			if (request.signal.aborted) return;
			const url = URL.createObjectURL(blob);
			let source: UploadSource;
			try {
				source = await imageSource(url, blob.type);
			} catch (cause) {
				URL.revokeObjectURL(url);
				throw cause;
			}
			if (request.signal.aborted) {
				URL.revokeObjectURL(url);
				return;
			}
			this.#source = source;
		} catch (cause) {
			if (!request.signal.aborted)
				this.#error =
					cause instanceof Error ? cause.message : this.i18n.t("uploads:previewLoadFailed");
		} finally {
			if (this.#request === request) {
				this.#request = undefined;
				this.#busy = false;
			}
		}
	};

	beginImageEdit = () => {
		if (!this.editableImage) return;
		this.#editingImage = true;
		return this.loadSource();
	};

	endImageEdit = () => {
		this.#editingImage = false;
		this.cancelPending();
	};

	applyImage = (edit: UploadImageEdit) => {
		this.#revision++;
		const original = documentImageEdit(this.#file ? undefined : this.#document);
		this.#image = Object.keys(original).every(
			(key) => original[key as keyof UploadImageEdit] === edit[key as keyof UploadImageEdit]
		)
			? undefined
			: { ...edit };
	};

	cancelPending = () => {
		this.#generation++;
		this.#request?.abort();
		this.#request = undefined;
		this.#busy = false;
	};

	dispose = () => {
		this.#editingImage = false;
		this.cancelPending();
		if (this.#source) URL.revokeObjectURL(this.#source.url);
		this.#source = undefined;
	};
}

async function imageSource(url: string, mimeType: string): Promise<UploadSource> {
	if (!mimeType.startsWith("image/")) return { url, width: 0, height: 0 };
	const image = new Image();
	image.src = url;
	await image.decode();
	return { url, width: image.naturalWidth, height: image.naturalHeight };
}
