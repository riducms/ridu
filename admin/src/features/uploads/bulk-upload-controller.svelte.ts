import type {
	AccessCapabilitiesEnvelope,
	AdminReadResultV1,
	SchemaCollection,
} from "@riducms/protocol";
import { RiduError } from "@riducms/sdk";

import type { AdminDocument } from "@admin/core/api/admin-client";
import { FormController, FormValidationError } from "@admin/core/forms/form-controller.svelte";
import { connectDocumentLiveValidation } from "@admin/core/forms/live-validation.svelte";
import { initialFormValues } from "@admin/core/forms/form-schema";
import { invalidFieldLabels } from "@admin/core/forms/form-validation";
import type { NotificationCenter } from "@admin/core/notifications/notification-center.svelte";
import type { AdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
import { initialBulkEditValues } from "@admin/features/bulk-edit/bulk-edit-fields";
import {
	applyBulkUploadValues,
	bulkUploadBulkEditFields,
	bulkUploadDocumentFields,
	bulkUploadFileIdentity,
	bulkUploadRemoteFilename,
	initialBulkUploadValues,
} from "@admin/features/uploads/bulk-upload";
import { UploadDraft } from "@admin/features/uploads/upload-draft.svelte";

export type BulkUploadStatus = "queued" | "uploading" | "complete" | "failed" | "uncertain";

interface BulkUploadControllerOptions {
	runtime: AdminRuntime;
	notifications: NotificationCenter;
	get slug(): string;
	get locale(): string | undefined;
	get collection(): SchemaCollection | undefined;
	get routeIdentity(): string;
	get preparedAccess(): AdminReadResultV1<AccessCapabilitiesEnvelope> | undefined;
}

export class BulkUploadItem {
	readonly form: FormController;
	readonly upload: UploadDraft;
	status = $state<BulkUploadStatus>("queued");
	error = $state<string>();
	documentID = $state<string>();
	ready: Promise<void>;
	#disposed = false;

	constructor(
		readonly id: string,
		readonly sourceFile: File,
		private readonly collection: SchemaCollection,
		private readonly runtime: AdminRuntime,
		locale: string | undefined,
		access: AccessCapabilitiesEnvelope | undefined
	) {
		const fields = bulkUploadDocumentFields(collection.fields);
		const values = initialBulkUploadValues(sourceFile.name, fields, collection.admin.useAsTitle);
		this.form = new FormController(values, runtime.i18n);
		this.form.reset(values, fields);
		connectDocumentLiveValidation(this.form, runtime.client);
		this.form.setResource({ collection: collection.slug });
		this.form.setLocalization(locale);
		this.form.setAccess(access, "create");
		this.upload = new UploadDraft(runtime.client, () => collection.slug, runtime.i18n);
		this.ready = this.prepare();
	}

	get issueCount() {
		return this.form.issues.length + (this.upload.error === undefined ? 0 : 1);
	}

	get issueLabels() {
		const labels = invalidFieldLabels(
			bulkUploadDocumentFields(this.collection.fields),
			this.form.issues,
			this.runtime.i18n
		);
		return this.upload.error === undefined ? labels : [this.upload.error, ...labels];
	}

	get editable() {
		return this.status === "queued" || this.status === "failed";
	}

	setAccess(access: AccessCapabilitiesEnvelope | undefined) {
		this.form.setAccess(access, "create");
	}

	dispose() {
		if (this.#disposed) return;
		this.#disposed = true;
		this.form.disposeBindings();
		this.upload.dispose();
	}

	private async prepare() {
		const settings = this.collection.uploadSettings;
		if (settings === undefined) return;
		await this.upload.select(this.sourceFile, settings);
		if (this.#disposed) return;
		if (this.upload.file !== undefined) return;
		this.status = "failed";
		this.error = this.upload.error ?? this.runtime.i18n.t("uploads:fileOpenFailed");
	}
}

export class BulkUploadController {
	queue = $state.raw<BulkUploadItem[]>([]);
	activeIndex = $state(0);
	running = $state(false);
	accessLoading = $state(true);
	access = $state.raw<AccessCapabilitiesEnvelope>();
	accessError = $state<string>();
	remoteURL = $state("");
	remotePending = $state(false);
	remoteError = $state<string>();
	remoteOutcomeUncertain = $state(false);
	remoteDocumentID = $state<string>();
	bulkEditOpen = $state(false);
	bulkEditPaths = $state.raw<string[]>([]);
	readonly remoteForm: FormController;
	readonly bulkEditForm: FormController;
	#remoteAttemptURL = "";
	#nextID = 0;
	#owner = "";
	#accessKey = "";
	#preparedAccessIdentity = "";
	#queueRequest?: AbortController;
	#remoteRequest?: AbortController;
	#accessRequest?: AbortController;
	#stopRemoteChangeObserver: () => void;
	#disposed = false;

	constructor(readonly options: BulkUploadControllerOptions) {
		this.remoteForm = new FormController({}, options.runtime.i18n);
		connectDocumentLiveValidation(this.remoteForm, options.runtime.client);
		this.bulkEditForm = new FormController({}, options.runtime.i18n);
		this.#stopRemoteChangeObserver = this.remoteForm.observeChanges(() => {
			if (this.remoteDocumentID !== undefined && this.remoteForm.dirty)
				this.remoteDocumentID = undefined;
		});

		$effect.pre(() => {
			const owner = `${options.slug}:${options.locale ?? "default"}:${options.runtime.manifestRevision}`;
			if (owner !== this.#owner) this.#activateOwner(owner);

			const accessKey = `${owner}:${options.routeIdentity}`;
			if (accessKey !== this.#accessKey) {
				const prepared =
					this.#preparedAccessIdentity === options.routeIdentity
						? undefined
						: options.preparedAccess;
				if (prepared !== undefined) this.#preparedAccessIdentity = options.routeIdentity;
				this.#loadAccess(accessKey, prepared);
			}
		});

		$effect(() => () => this.dispose());
	}

	get collection() {
		return this.options.collection;
	}

	get locale() {
		return this.options.locale;
	}

	get fields() {
		return bulkUploadDocumentFields(this.collection?.fields ?? []);
	}

	get editableFields() {
		const form = this.activeItem?.form ?? this.remoteForm;
		return bulkUploadDocumentFields(this.collection?.fields ?? []).filter((field) =>
			form.canRead(field.path)
		);
	}

	get bulkEditFields() {
		return bulkUploadBulkEditFields(this.collection?.fields ?? []).filter(
			(field) =>
				this.access?.fields[field.path]?.read !== false &&
				this.access?.fields[field.path]?.create !== false
		);
	}

	get selectedBulkEditFields() {
		const selected = new Set(this.bulkEditPaths);
		return this.bulkEditFields.filter((field) => selected.has(field.path));
	}

	get activeItem() {
		return this.queue[this.activeIndex];
	}

	get canCreate() {
		return this.access?.operations.create === true;
	}

	get uploadEnabled() {
		return (
			this.collection?.capabilities.upload === true && this.collection.uploadSettings !== undefined
		);
	}

	get pendingCount() {
		return this.queue.filter((item) => item.status === "queued" || item.status === "failed").length;
	}

	get preparing() {
		return this.draftItems.some((item) => item.upload.busy);
	}

	get completedCount() {
		return this.queue.filter((item) => item.status === "complete").length;
	}

	get failedCount() {
		return this.queue.filter((item) => item.status === "failed").length;
	}

	get uncertainCount() {
		return this.queue.filter((item) => item.status === "uncertain").length;
	}

	get draftItems() {
		return this.queue.filter((item) => item.status === "queued" || item.status === "failed");
	}

	get dirty() {
		return (
			this.running ||
			this.remotePending ||
			this.queue.some((item) => item.status !== "complete") ||
			this.remoteURL.trim() !== "" ||
			this.remoteForm.dirty ||
			this.remoteOutcomeUncertain
		);
	}

	addFiles = async (files: Iterable<File>) => {
		const collection = this.collection;
		if (!collection || !this.uploadEnabled || !this.canCreate || this.running) return;
		const known = new Set(this.queue.map((item) => bulkUploadFileIdentity(item.sourceFile)));
		const additions: BulkUploadItem[] = [];
		for (const file of files) {
			const identity = bulkUploadFileIdentity(file);
			if (known.has(identity)) continue;
			known.add(identity);
			additions.push(
				new BulkUploadItem(
					`upload-${++this.#nextID}`,
					file,
					collection,
					this.options.runtime,
					this.options.locale,
					this.access
				)
			);
		}
		if (additions.length === 0) return;
		const wasEmpty = this.queue.length === 0;
		this.queue = [...this.queue, ...additions];
		if (wasEmpty) this.activeIndex = 0;
		await Promise.all(additions.map((item) => item.ready));
	};

	remove = (id: string) => {
		if (this.running) return;
		const index = this.queue.findIndex((item) => item.id === id);
		if (index < 0) return;
		const activeIndex = this.activeIndex;
		this.queue[index]?.dispose();
		this.queue = this.queue.filter((item) => item.id !== id);
		this.activeIndex = Math.max(
			0,
			Math.min(index < activeIndex ? activeIndex - 1 : activeIndex, this.queue.length - 1)
		);
	};

	select = (index: number) => {
		if (index >= 0 && index < this.queue.length) this.activeIndex = index;
	};

	previous = () => {
		if (this.queue.length === 0) return;
		this.activeIndex = (this.activeIndex - 1 + this.queue.length) % this.queue.length;
	};

	next = () => {
		if (this.queue.length === 0) return;
		this.activeIndex = (this.activeIndex + 1) % this.queue.length;
	};

	saveAll = () => this.#saveItems(this.queue);

	retry = (item: BulkUploadItem) => {
		if (item.status !== "failed") return Promise.resolve();
		return this.#saveItems([item]);
	};

	setRemoteURL = (value: string) => {
		if (value !== this.remoteURL) this.remoteDocumentID = undefined;
		this.remoteURL = value;
		if (this.remoteOutcomeUncertain && value.trim() !== this.#remoteAttemptURL) {
			this.remoteOutcomeUncertain = false;
			this.remoteError = undefined;
		}
	};

	prepareRemoteDefaults = () => {
		const collection = this.collection;
		if (!collection || this.remoteForm.dirty) return;
		this.remoteForm.reset(
			initialBulkUploadValues(
				bulkUploadRemoteFilename(this.remoteURL),
				this.fields,
				collection.admin.useAsTitle
			),
			this.fields
		);
		this.#configureForm(this.remoteForm);
	};

	uploadRemote = async () => {
		const collection = this.collection;
		const owner = this.#owner;
		const fields = this.fields;
		const locale = this.options.locale;
		const url = this.remoteURL.trim();
		if (
			!collection ||
			this.remotePending ||
			!this.uploadEnabled ||
			!this.canCreate ||
			url === "" ||
			this.remoteOutcomeUncertain
		)
			return;
		this.prepareRemoteDefaults();
		const request = new AbortController();
		this.#remoteRequest?.abort();
		this.#remoteRequest = request;
		this.remotePending = true;
		this.remoteError = undefined;
		this.remoteOutcomeUncertain = false;
		this.remoteDocumentID = undefined;
		this.#remoteAttemptURL = url;
		try {
			const document = await this.remoteForm.submit(fields, true, (values) =>
				this.options.runtime.client.uploadFromURL(collection.slug, url, {
					data: values,
					signal: request.signal,
					locale,
				})
			);
			if (request.signal.aborted || this.#owner !== owner) return;
			this.remoteURL = "";
			this.remoteForm.reset(initialFormValues(fields), fields);
			this.#configureForm(this.remoteForm);
			this.remoteDocumentID = document.id;
			this.options.runtime.documentsChanged();
			this.options.notifications.success({
				title: this.options.runtime.i18n.t("uploads:remoteAssetUploaded"),
			});
		} catch (cause) {
			if (request.signal.aborted || this.#owner !== owner) return;
			const confirmed =
				cause instanceof FormValidationError || (cause instanceof RiduError && cause.status < 500);
			this.remoteOutcomeUncertain = !confirmed;
			this.remoteError = confirmed
				? cause instanceof Error
					? cause.message
					: this.options.runtime.i18n.t("documents:saveFailed")
				: this.options.runtime.i18n.t("uploads:remoteOutcomeNotConfirmed");
		} finally {
			if (this.#remoteRequest === request && this.#owner === owner) {
				this.#remoteRequest = undefined;
				this.remotePending = false;
			}
		}
	};

	setBulkEditPaths = (paths: string[]) => {
		const allowed = new Set(this.bulkEditFields.map((field) => field.path));
		this.bulkEditPaths = [...new Set(paths.filter((path) => allowed.has(path)))];
		const fields = this.selectedBulkEditFields;
		this.bulkEditForm.reset(initialBulkEditValues(fields, this.bulkEditForm.snapshot()), fields);
		this.#configureForm(this.bulkEditForm);
	};

	openBulkEdit = () => {
		this.bulkEditPaths = [];
		this.bulkEditForm.reset({}, []);
		this.#configureForm(this.bulkEditForm);
		this.bulkEditOpen = true;
	};

	applyBulkEdit = () => {
		const fields = this.selectedBulkEditFields;
		const values = this.bulkEditForm.snapshot();
		for (const item of this.draftItems) applyBulkUploadValues(item.form, fields, values);
		this.bulkEditOpen = false;
	};

	discard = () => {
		this.#queueRequest?.abort();
		this.#remoteRequest?.abort();
		this.#disposeQueue();
		this.queue = [];
		this.activeIndex = 0;
		this.running = false;
		this.remotePending = false;
		this.remoteURL = "";
		this.remoteError = undefined;
		this.remoteOutcomeUncertain = false;
		this.remoteDocumentID = undefined;
		this.#remoteAttemptURL = "";
		this.remoteForm.reset(initialFormValues(this.fields), this.fields);
		this.#configureForm(this.remoteForm);
		this.bulkEditOpen = false;
		this.bulkEditPaths = [];
		this.bulkEditForm.reset({}, []);
		this.#configureForm(this.bulkEditForm);
	};

	dispose() {
		if (this.#disposed) return;
		this.#disposed = true;
		this.#accessRequest?.abort();
		this.#queueRequest?.abort();
		this.#remoteRequest?.abort();
		this.#disposeQueue();
		this.#stopRemoteChangeObserver();
		this.remoteForm.disposeBindings();
		this.bulkEditForm.disposeBindings();
	}

	async #saveItems(items: readonly BulkUploadItem[]) {
		if (this.running || !this.uploadEnabled || !this.canCreate) return;
		const collection = this.collection;
		if (!collection) return;
		const owner = this.#owner;
		const fields = this.fields;
		const locale = this.options.locale;
		const candidates = items.filter((item) => item.status === "queued" || item.status === "failed");
		if (candidates.length === 0 || candidates.some((item) => item.upload.busy)) return;
		const request = new AbortController();
		this.#queueRequest?.abort();
		this.#queueRequest = request;
		this.running = true;

		for (const item of candidates) {
			await item.ready;
			if (request.signal.aborted || this.#owner !== owner) return;
			if (item.upload.file === undefined) {
				item.status = "failed";
				item.error =
					item.upload.error ?? this.options.runtime.i18n.t("uploads:chooseFileBeforeSaving");
				continue;
			}
			item.status = "uploading";
			item.error = undefined;
			try {
				const document = await item.form.submit(fields, true, (values) =>
					this.options.runtime.client.upload(collection.slug, item.upload.file!, {
						filename: item.upload.filename,
						image: item.upload.image,
						data: values,
						signal: request.signal,
						locale,
					})
				);
				if (request.signal.aborted || this.#owner !== owner) return;
				item.status = "complete";
				item.documentID = document.id;
				item.upload.reset(document as AdminDocument);
				this.options.runtime.documentsChanged();
			} catch (cause) {
				if (request.signal.aborted || this.#owner !== owner) return;
				const confirmed =
					cause instanceof FormValidationError ||
					(cause instanceof RiduError && cause.status < 500);
				item.status = confirmed ? "failed" : "uncertain";
				item.error = confirmed
					? cause instanceof Error
						? cause.message
						: this.options.runtime.i18n.t("documents:saveFailed")
					: this.options.runtime.i18n.t("uploads:outcomeNotConfirmed");
			}
		}

		if (request.signal.aborted || this.#owner !== owner) return;
		this.running = false;
		this.#queueRequest = undefined;
		const failures = this.failedCount;
		const uncertain = this.uncertainCount;
		if (failures === 0 && uncertain === 0 && this.completedCount > 0) {
			this.options.notifications.success({
				title: this.options.runtime.i18n.t("uploads:assetsUploaded", {
					count: this.completedCount,
				}),
			});
		} else if (failures > 0 || uncertain > 0) {
			this.options.notifications.error({
				title: this.options.runtime.i18n.t("uploads:needAttention", {
					count: failures + uncertain,
				}),
				message:
					uncertain > 0
						? this.options.runtime.i18n.t("uploads:unknownOutcomesDescription")
						: this.options.runtime.i18n.t("uploads:correctMetadata"),
			});
			const firstFailure = this.queue.findIndex((item) => item.status === "failed");
			if (firstFailure >= 0) this.activeIndex = firstFailure;
		}
	}

	#activateOwner(owner: string) {
		this.#owner = owner;
		this.#accessKey = "";
		this.#accessRequest?.abort();
		this.#queueRequest?.abort();
		this.#remoteRequest?.abort();
		this.#accessRequest = undefined;
		this.#queueRequest = undefined;
		this.#remoteRequest = undefined;
		this.#disposeQueue();
		this.queue = [];
		this.activeIndex = 0;
		this.running = false;
		this.access = undefined;
		this.accessLoading = true;
		this.accessError = undefined;
		this.remoteURL = "";
		this.remotePending = false;
		this.remoteError = undefined;
		this.remoteOutcomeUncertain = false;
		this.remoteDocumentID = undefined;
		this.#remoteAttemptURL = "";
		this.remoteForm.reset(initialFormValues(this.fields), this.fields);
		this.#configureForm(this.remoteForm);
		this.bulkEditOpen = false;
		this.bulkEditPaths = [];
		this.bulkEditForm.reset({}, []);
	}

	#loadAccess(key: string, prepared: AdminReadResultV1<AccessCapabilitiesEnvelope> | undefined) {
		this.#accessKey = key;
		this.#accessRequest?.abort();
		this.#accessRequest = undefined;
		if (prepared !== undefined) {
			this.accessLoading = false;
			this.accessError = prepared.error?.message;
			this.#setAccess(prepared.value);
			return;
		}
		const slug = this.options.slug;
		if (slug === "") {
			this.accessLoading = false;
			return;
		}
		const request = new AbortController();
		this.#accessRequest = request;
		this.accessLoading = true;
		this.accessError = undefined;
		this.#setAccess(undefined);
		this.options.runtime.client
			.collectionAccess(slug, { signal: request.signal, locale: this.options.locale })
			.then((access) => {
				if (!request.signal.aborted) this.#setAccess(access);
			})
			.catch((cause: unknown) => {
				if (!request.signal.aborted)
					this.accessError =
						cause instanceof Error
							? cause.message
							: this.options.runtime.i18n.t("uploads:accessCheckFailed");
			})
			.finally(() => {
				if (!request.signal.aborted) {
					this.accessLoading = false;
					this.#accessRequest = undefined;
				}
			});
	}

	#setAccess(access: AccessCapabilitiesEnvelope | undefined) {
		this.access = access;
		for (const item of this.queue) item.setAccess(access);
		this.remoteForm.setAccess(access, "create");
		this.bulkEditForm.setAccess(access, "create");
	}

	#configureForm(form: FormController) {
		form.setResource({ collection: this.options.slug });
		form.setLocalization(this.options.locale);
		form.setAccess(this.access, "create");
	}

	#disposeQueue() {
		for (const item of this.queue) item.dispose();
	}
}
