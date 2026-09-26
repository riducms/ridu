import { connectDocumentLiveValidation } from "@admin/core/forms/live-validation.svelte";
import type { AdminCreateDataV1, AdminDocumentDataV1, SchemaCollection } from "@riducms/protocol";
import { RiduError } from "@riducms/sdk";
import { formatAdminDateTime } from "@admin/core/i18n/format-admin-date-time";
import { tick } from "svelte";

import type { AdminDocument } from "@admin/core/api/admin-client";
import { FormController, FormValidationError } from "@admin/core/forms/form-controller.svelte";
import {
	clearFormDraft,
	saveFormDraft,
	takeFormDraft,
} from "@admin/core/forms/form-draft-recovery";
import { documentFormValues, initialFormValues } from "@admin/core/forms/form-schema";
import { invalidFieldLabels } from "@admin/core/forms/form-validation";
import type { NotificationCenter } from "@admin/core/notifications/notification-center.svelte";
import { samePreparedCreateValues } from "@admin/core/bootstrap/admin-bootstrap";
import {
	collectionPath,
	createDocumentPath,
	documentPath,
	globalPath,
	withContentLocale,
} from "@admin/core/routing/admin-paths";
import type { AdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
import { DocumentLockController } from "@admin/features/documents/document-lock-controller.svelte";
import { documentTitleField } from "@admin/features/documents/document-title";
import { isUploadMetadataField } from "@admin/features/uploads/upload-document-contracts";
import { UploadDraft } from "@admin/features/uploads/upload-draft.svelte";

type Navigate = (to: string, options?: { replace?: boolean }) => void;

interface DocumentControllerOptions {
	runtime: AdminRuntime;
	notifications: NotificationCenter;
	navigate: Navigate;
	get slug(): string;
	get documentID(): string | undefined;
	get global(): boolean;
	get locale(): string | undefined;
	get editable(): boolean;
	readonly prepared?: AdminDocumentDataV1 | AdminCreateDataV1;
}

interface DocumentRouteSnapshot {
	slug: string;
	documentID: string | undefined;
	locale: string | undefined;
	manifestRevision: number;
	global: boolean;
	editable: boolean;
	prepared?: AdminDocumentDataV1 | AdminCreateDataV1;
}

export class DocumentController {
	readonly form: FormController;
	readonly lock: DocumentLockController;
	collection = $state.raw<SchemaCollection>();
	collectionAvailable = $state(true);
	loading = $state(true);
	error = $state<string>();
	readonly upload: UploadDraft;
	publicationOperation = $state(false);
	saveOutcomeUncertain = $state(false);
	duplicateOperation = $state(false);
	deleteDialogOpen = $state(false);
	currentDocument = $state.raw<AdminDocument>();
	lastSavedAt = $state<number>();
	unlockOperation = $state(false);
	copyLocaleOperation = $state(false);
	#activeRouteKey = "";
	#routeGeneration = 0;
	#activeContentLocale?: string;
	#appliedManifestRevision = 0;
	#scheduledRouteKey = "";
	#formChangeGeneration = 0;
	#stopObservingFormChanges: () => void;
	#loadRequest?: AbortController;
	#saveRequest?: AbortController;
	#forceUnlockRequest?: AbortController;
	#copyLocaleRequest?: AbortController;

	constructor(private readonly options: DocumentControllerOptions) {
		this.upload = new UploadDraft(
			options.runtime.client,
			() => this.collectionSlug,
			options.runtime.i18n
		);
		this.form = new FormController({}, options.runtime.i18n);
		this.lock = new DocumentLockController({
			runtime: options.runtime,
			notifications: options.notifications,
			form: this.form,
		});
		this.#stopObservingFormChanges = this.form.observeChanges(() => {
			this.#formChangeGeneration += 1;
		});
		connectDocumentLiveValidation(this.form, options.runtime.client);
		this.#scheduleRouteSync(this.#routeSnapshot());
		$effect.pre(() => {
			const global = this.options.global;
			this.#scheduleRouteSync({
				slug: this.options.slug,
				documentID: global ? this.options.slug : this.options.documentID,
				locale: this.options.locale,
				manifestRevision: this.options.runtime.manifestRevision,
				global,
				editable: this.options.editable,
				prepared: this.options.prepared,
			});
		});

		$effect(() => {
			const checkpoint = () => this.checkpointDraft();
			const leavePage = () => {
				checkpoint();
				this.lock.release();
			};
			window.addEventListener("pagehide", leavePage);
			const hot = import.meta.hot;
			hot?.on("vite:beforeFullReload", checkpoint);
			return () => {
				window.removeEventListener("pagehide", leavePage);
				hot?.off("vite:beforeFullReload", checkpoint);
				this.#routeGeneration += 1;
				this.#cancelRouteRequests();
				this.#loadRequest?.abort();
				this.#stopObservingFormChanges();
				this.form.disposeBindings();
				this.upload.dispose();
				this.#saveRequest?.abort();
				this.lock.release();
			};
		});

		$effect(() => {
			const interval = this.collection?.versionSettings?.autosaveIntervalSeconds ?? 0;
			const documentID = this.documentID;
			if (documentID === undefined || interval <= 0) return;

			const timer = window.setInterval(async () => {
				if (!this.canSave || this.publicationOperation) return;
				// A background update to a published document would make its edits public.
				// Keep a recoverable local checkpoint until the author explicitly chooses
				// Publish changes; draft documents can use the ordinary server mutation.
				if (this.currentStatus === "published") this.checkpointDraft();
				else await this.save({ silent: true });
			}, interval * 1_000);
			return () => window.clearInterval(timer);
		});
	}

	#routeSnapshot(): DocumentRouteSnapshot {
		const slug = this.options.slug;
		const global = this.options.global;
		return {
			slug,
			documentID: global ? slug : this.options.documentID,
			locale: this.options.locale,
			manifestRevision: this.options.runtime.manifestRevision,
			global,
			editable: this.options.editable,
			prepared: this.options.prepared,
		};
	}

	#scheduleRouteSync(snapshot: DocumentRouteSnapshot) {
		const { slug, documentID, locale, manifestRevision, global, editable } = snapshot;
		if (manifestRevision === 0) return;
		const key = `${global ? "global" : "collection"}:${slug}:${documentID ?? "new"}:${locale ?? "default"}:${editable ? "edit" : "read"}:${manifestRevision}`;
		if (key === this.#scheduledRouteKey) return;
		this.#scheduledRouteKey = key;
		// Initial construction is outside dependency collection; retained routes commit in
		// $effect.pre. The key guard makes any dependencies read during the commit disappear on its
		// guarded rerun, leaving only the explicit route-option reads above.
		this.#enterRoute(
			slug,
			documentID,
			locale,
			manifestRevision,
			global,
			editable,
			snapshot.prepared
		);
	}

	get documentID() {
		return this.options.global ? this.options.slug : this.options.documentID;
	}

	get creating() {
		return !this.options.global && this.documentID === undefined;
	}

	get globalResource() {
		return this.options.global;
	}

	get collectionSlug() {
		return this.collection?.slug ?? this.options.slug;
	}

	get contentLocale() {
		return this.#activeRouteKey === "" ? this.options.locale : this.#activeContentLocale;
	}

	get uploadCollection() {
		return this.collection?.capabilities.upload === true;
	}

	get versionedCollection() {
		return this.collection?.capabilities.versions === true;
	}

	get draftsCollection() {
		return this.collection?.versionSettings?.drafts === true;
	}

	get currentRevision() {
		const revision = this.currentDocument?._revision;
		return typeof revision === "number" ? revision : 0;
	}

	get currentStatus(): "draft" | "published" {
		if (this.currentDocument === undefined) return this.draftsCollection ? "draft" : "published";
		return this.currentDocument._status === "draft" ? "draft" : "published";
	}

	get selectedFile() {
		return this.upload.file;
	}

	get hasUnsavedChanges() {
		return this.form.dirty || this.upload.dirty;
	}

	get creatingAuthUser() {
		return this.creating && !this.globalResource && this.collection?.capabilities.auth === true;
	}

	get canForceUnlock() {
		return (
			!this.globalResource &&
			!this.creating &&
			this.collection?.capabilities.auth === true &&
			(this.collection.authSettings?.maxLoginAttempts ?? 0) > 0 &&
			this.form.access?.operations.update === true
		);
	}

	get canSave() {
		const operationAllowed = this.creating
			? this.form.access?.operations.create === true
			: this.form.access?.operations.update === true;
		const publicationAllowed =
			!this.creating || !this.versionedCollection || this.draftsCollection || this.canPublish;
		const inputReady = this.creating
			? !this.uploadCollection || this.upload.file !== undefined
			: this.hasUnsavedChanges;
		return (
			this.collectionAvailable &&
			!this.saveOutcomeUncertain &&
			!this.upload.busy &&
			!this.upload.editingImage &&
			!this.lock.lockedByAnotherEditor &&
			operationAllowed &&
			publicationAllowed &&
			!this.form.submitting &&
			!this.form.writeBlocked &&
			inputReady &&
			(!this.uploadCollection || this.upload.present)
		);
	}

	get headingField() {
		return this.uploadCollection
			? undefined
			: documentTitleField(this.collection, (path) => this.form.canRead(path));
	}

	get documentFields() {
		return (this.collection?.fields ?? []).filter(
			(field) =>
				this.form.canRead(field.path) &&
				!(this.uploadCollection && isUploadMetadataField(field.name))
		);
	}

	get canDelete() {
		return !this.lock.lockedByAnotherEditor && this.form.access?.operations.delete === true;
	}

	get canDuplicate() {
		return !this.lock.lockedByAnotherEditor && this.form.access?.operations.duplicate === true;
	}

	get canPublish() {
		return (
			!this.lock.lockedByAnotherEditor &&
			!this.form.writeBlocked &&
			this.form.access?.operations.publish === true
		);
	}

	get canUnpublish() {
		return (
			!this.lock.lockedByAnotherEditor &&
			!this.form.writeBlocked &&
			this.form.access?.operations.unpublish === true
		);
	}

	get canReadVersions() {
		return !this.creating && this.form.access?.operations.readVersions === true;
	}

	get canEditUpload() {
		return (
			this.uploadCollection &&
			!this.lock.lockedByAnotherEditor &&
			!this.form.submitting &&
			!this.saveOutcomeUncertain &&
			!this.form.writeBlocked &&
			(this.creating
				? this.form.access?.operations.create === true
				: this.form.access?.operations.update === true)
		);
	}

	get validationFields() {
		return (this.collection?.fields ?? []).filter(
			(field) => !(this.uploadCollection && isUploadMetadataField(field.name))
		);
	}

	get collectionSingularLabel() {
		const labels = this.collection?.labels;
		return labels === undefined
			? this.options.runtime.i18n.t("documents:document")
			: this.options.runtime.i18n.text(labels.singular, labels.singularTranslations);
	}

	get documentHeading() {
		if (this.globalResource) return this.collectionSingularLabel;
		if (this.uploadCollection)
			return this.upload.filename || this.options.runtime.i18n.t("documents:untitled");
		const value =
			this.headingField === undefined ? undefined : this.form.get(this.headingField.path);
		const title = typeof value === "string" ? value.trim() : "";
		if (title.length > 0) return title;
		return this.creating
			? this.options.runtime.i18n.t("documents:newLabel", {
					label: this.collectionSingularLabel.toLocaleLowerCase(this.options.runtime.i18n.language),
				})
			: this.options.runtime.i18n.t("documents:untitled");
	}

	get assetURL() {
		return this.#documentString("url");
	}

	get assetFilename() {
		return this.#documentString("filename") ?? this.documentHeading;
	}

	get assetMimeType() {
		return this.#documentString("mimeType");
	}

	checkpointDraft = () => {
		const collection = this.collection;
		if (collection === undefined) return;
		if (this.form.dirty) {
			saveFormDraft(
				collection,
				this.documentID,
				$state.snapshot(this.form.values),
				$state.snapshot(this.form.original)
			);
		} else clearFormDraft(collection.id, this.documentID);
	};

	discardChanges = () => {
		const collection = this.collection;
		if (collection !== undefined) clearFormDraft(collection.id, this.documentID);
		this.form.reset($state.snapshot(this.form.original));
		this.upload.reset(this.currentDocument);
	};

	forceUnlock = async () => {
		const { runtime, notifications } = this.options;
		if (this.collection === undefined || this.currentDocument === undefined) return;
		const request = new AbortController();
		this.#forceUnlockRequest?.abort();
		this.#forceUnlockRequest = request;
		this.unlockOperation = true;
		try {
			await runtime.client.forceUnlock(this.collection.slug, this.currentDocument.id, {
				signal: request.signal,
			});
			if (request.signal.aborted) return;
			notifications.success({ title: runtime.i18n.t("documents:accountUnlocked") });
		} catch (cause) {
			if (request.signal.aborted) return;
			notifications.error({
				title: runtime.i18n.t("documents:accountUnlockFailed"),
				message: cause instanceof Error ? cause.message : undefined,
			});
		} finally {
			if (!request.signal.aborted) {
				this.#forceUnlockRequest = undefined;
				this.unlockOperation = false;
			}
		}
	};

	copyFromLocale = async (source: string) => {
		const { runtime, notifications } = this.options;
		if (
			this.contentLocale === undefined ||
			source === this.contentLocale ||
			this.currentDocument === undefined ||
			this.copyLocaleOperation
		)
			return;
		const request = new AbortController();
		const destinationLocale = this.contentLocale;
		const document = this.currentDocument;
		this.#copyLocaleRequest?.abort();
		this.#copyLocaleRequest = request;
		this.copyLocaleOperation = true;
		try {
			if (this.globalResource) {
				await runtime.client.copyGlobalLocale(
					this.collectionSlug,
					{ from: source, to: destinationLocale },
					{ revision: document._revision, signal: request.signal }
				);
			} else {
				await runtime.client.copyLocale(
					this.collectionSlug,
					document.id,
					{ from: source, to: destinationLocale },
					{ revision: document._revision, signal: request.signal }
				);
			}
			if (request.signal.aborted) return;
			await this.refresh();
			if (request.signal.aborted) return;
			runtime.documentsChanged();
			notifications.success({
				title: runtime.i18n.t("documents:localeCopied"),
				message: runtime.i18n.t("documents:localeCopiedDescription", {
					source,
					destination: destinationLocale,
				}),
			});
		} catch (cause) {
			if (request.signal.aborted) return;
			notifications.error({
				title: runtime.i18n.t("documents:localeNotCopied"),
				message:
					cause instanceof Error ? cause.message : runtime.i18n.t("documents:localeCopyFailed"),
			});
		} finally {
			if (!request.signal.aborted) {
				this.#copyLocaleRequest = undefined;
				this.copyLocaleOperation = false;
			}
		}
	};

	refresh = async () => {
		const documentID = this.documentID;
		if (
			documentID === undefined ||
			this.collection === undefined ||
			this.#saveRequest !== undefined ||
			this.upload.busy ||
			this.publicationOperation
		)
			return;
		await this.#load(this.collection.slug, documentID);
	};

	save = async ({
		silent = false,
		password,
		publish = false,
	}: { silent?: boolean; password?: string; publish?: boolean } = {}) => {
		if (!this.canSave) return false;
		const publishingChanges = publish && !this.creating && this.versionedCollection;
		if (publishingChanges && !this.canPublish) return false;
		const collectionSlug = this.collectionSlug;
		const documentID = this.documentID;
		const request = new AbortController();
		this.#saveRequest?.abort();
		this.#saveRequest = request;
		this.#cancelLoad();
		const wasCreating = this.creating;
		try {
			const saved = await this.form.submit(this.validationFields, this.creating, async (values) => {
				if (this.globalResource) {
					if (publishingChanges) {
						return this.options.runtime.client.publishGlobalChanges(collectionSlug, values, {
							revision: this.currentRevision,
							signal: request.signal,
							locale: this.contentLocale,
						});
					}
					return this.options.runtime.client.updateGlobal(collectionSlug, values, {
						revision: this.currentRevision,
						signal: request.signal,
						locale: this.contentLocale,
					});
				}
				if (documentID !== undefined) {
					if (this.uploadCollection) {
						return this.options.runtime.client.updateUpload(
							collectionSlug,
							documentID,
							{
								data: values,
								file: this.upload.file,
								filename: this.upload.filename,
								image: this.upload.image,
								publish: publishingChanges,
							},
							{ revision: this.currentRevision, signal: request.signal, locale: this.contentLocale }
						);
					}
					if (publishingChanges) {
						return this.options.runtime.client.publishChanges(collectionSlug, documentID, values, {
							revision: this.currentRevision,
							signal: request.signal,
							locale: this.contentLocale,
						});
					}
					return this.options.runtime.client.update(collectionSlug, documentID, values, {
						revision: this.currentRevision,
						signal: request.signal,
						locale: this.contentLocale,
					});
				}
				if (this.collection?.capabilities.auth) {
					if (password === undefined)
						throw new Error(this.options.runtime.i18n.t("documents:enterNewAccountPassword"));
					return this.options.runtime.client.createAuthUser(collectionSlug, values, password, {
						signal: request.signal,
						locale: this.contentLocale,
					});
				}
				if (!this.uploadCollection) {
					return this.options.runtime.client.create(collectionSlug, values, {
						signal: request.signal,
						locale: this.contentLocale,
						...(this.versionedCollection
							? { draft: this.draftsCollection ? !publish : false }
							: {}),
					});
				}
				if (this.selectedFile === undefined)
					throw new Error(this.options.runtime.i18n.t("uploads:chooseFileBeforeSaving"));
				return this.options.runtime.client.upload(collectionSlug, this.selectedFile, {
					filename: this.upload.filename,
					image: this.upload.image,
					publish,
					data: values,
					signal: request.signal,
					locale: this.contentLocale,
				});
			});
			if (request.signal.aborted) return false;
			this.saveOutcomeUncertain = false;
			this.#applyDocument(saved);
			this.lastSavedAt = Date.now();
			this.options.runtime.documentsChanged();
			clearFormDraft(this.collection?.id ?? this.collectionSlug, this.documentID);
			if (!silent) {
				this.options.notifications.success({
					title: wasCreating
						? this.options.runtime.i18n.t("documents:createdLabel", {
								label: this.collectionSingularLabel,
							})
						: publishingChanges
							? this.options.runtime.i18n.t("documents:published")
							: this.options.runtime.i18n.t("documents:updated"),
				});
			}
			if (wasCreating) {
				// Let the route clear any blocked departure now that the successful save has
				// removed both form and upload dirtiness before replacing the create URL.
				await tick();
				if (request.signal.aborted) return false;
				this.options.navigate(
					withContentLocale(documentPath(this.collectionSlug, saved.id), this.contentLocale),
					{ replace: true }
				);
			} else {
				await this.#refreshAccess(saved.id);
			}
			return !request.signal.aborted;
		} catch (cause) {
			if (request.signal.aborted) return false;
			if (
				this.uploadCollection &&
				!(cause instanceof FormValidationError) &&
				(!(cause instanceof RiduError) || cause.status >= 500)
			) {
				this.saveOutcomeUncertain = true;
				if (!silent) {
					this.options.notifications.error({
						title: this.options.runtime.i18n.t("uploads:outcomeUnknown"),
						message: this.options.runtime.i18n.t(
							wasCreating
								? "uploads:outcomeUnknownDescription"
								: "uploads:saveOutcomeUnknownDescription"
						),
					});
				}
				return false;
			}
			if (!silent) {
				const validationFailure =
					this.form.issues.length > 0 &&
					(cause instanceof FormValidationError ||
						(cause instanceof RiduError && cause.code === "validation"));
				if (validationFailure) {
					this.options.notifications.validation({
						labels: invalidFieldLabels(
							this.validationFields,
							this.form.issues,
							this.options.runtime.i18n
						),
					});
				} else {
					this.options.notifications.error({
						title: this.options.runtime.i18n.t("documents:notSaved"),
						message:
							cause instanceof Error
								? cause.message
								: this.options.runtime.i18n.t("documents:saveFailed"),
					});
				}
			}
			return false;
		} finally {
			if (this.#saveRequest === request) this.#saveRequest = undefined;
		}
	};

	changePublication = async (next: "draft" | "published") => {
		const allowed = next === "published" ? this.canPublish : this.canUnpublish;
		if (
			this.documentID === undefined ||
			!allowed ||
			this.form.submitting ||
			this.publicationOperation ||
			this.upload.busy ||
			this.saveOutcomeUncertain
		)
			return;
		this.#cancelLoad();
		this.publicationOperation = true;
		const generation = this.#routeGeneration;
		try {
			const saved =
				next === "published"
					? this.globalResource
						? await this.options.runtime.client.publishGlobal(this.collectionSlug, {
								revision: this.currentRevision,
								locale: this.contentLocale,
							})
						: await this.options.runtime.client.publish(this.collectionSlug, this.documentID, {
								revision: this.currentRevision,
								locale: this.contentLocale,
							})
					: this.globalResource
						? await this.options.runtime.client.unpublishGlobal(this.collectionSlug, {
								revision: this.currentRevision,
								locale: this.contentLocale,
							})
						: await this.options.runtime.client.unpublish(this.collectionSlug, this.documentID, {
								revision: this.currentRevision,
								locale: this.contentLocale,
							});
			if (generation !== this.#routeGeneration) return;
			this.#applyDocument(saved);
			this.lastSavedAt = Date.now();
			this.options.runtime.documentsChanged();
			await this.#refreshAccess(this.documentID);
			if (generation !== this.#routeGeneration) return;
			this.options.notifications.success({
				title: this.options.runtime.i18n.t(
					next === "published" ? "documents:publishedTitle" : "documents:unpublishedTitle"
				),
				message: this.options.runtime.i18n.t("documents:statusNow", {
					status: this.options.runtime.i18n.t(
						next === "published" ? "documents:published" : "documents:draft"
					),
				}),
			});
		} catch (cause) {
			if (generation !== this.#routeGeneration) return;
			this.options.notifications.error({
				title: this.options.runtime.i18n.t("documents:statusNotChanged"),
				message:
					cause instanceof Error
						? cause.message
						: this.options.runtime.i18n.t("documents:statusChangeFailed"),
			});
		} finally {
			if (generation === this.#routeGeneration) this.publicationOperation = false;
		}
	};

	duplicate = async () => {
		if (this.documentID === undefined || this.globalResource || this.lock.lockedByAnotherEditor)
			return;
		this.duplicateOperation = true;
		const generation = this.#routeGeneration;
		try {
			const duplicated = await this.options.runtime.client.duplicate(
				this.collectionSlug,
				this.documentID,
				{},
				{ locale: this.contentLocale }
			);
			if (generation !== this.#routeGeneration) return;
			this.options.runtime.documentsChanged();
			this.options.notifications.success({
				title: this.options.runtime.i18n.t("documents:duplicatedLabel", {
					label: this.collectionSingularLabel,
				}),
				message: this.options.runtime.i18n.t("documents:copyReady"),
			});
			this.options.navigate(
				withContentLocale(documentPath(this.collectionSlug, duplicated.id), this.contentLocale)
			);
		} catch (cause) {
			if (generation !== this.#routeGeneration) return;
			this.options.notifications.error({
				title: this.options.runtime.i18n.t("documents:notDuplicated"),
				message:
					cause instanceof Error
						? cause.message
						: this.options.runtime.i18n.t("documents:duplicateFailed"),
			});
		} finally {
			if (generation === this.#routeGeneration) this.duplicateOperation = false;
		}
	};

	remove = async () => {
		if (this.documentID === undefined || this.lock.lockedByAnotherEditor) return;
		this.#cancelLoad();
		const generation = this.#routeGeneration;
		try {
			await this.options.runtime.client.delete(this.collectionSlug, this.documentID);
			if (generation !== this.#routeGeneration) return;
			this.options.runtime.documentsChanged();
			this.deleteDialogOpen = false;
			this.options.notifications.success({
				title: this.options.runtime.i18n.t(
					this.collection?.capabilities.trash === true
						? "documents:movedToTrashLabel"
						: "documents:deletedLabel",
					{ label: this.collectionSingularLabel }
				),
				message:
					this.collection?.capabilities.trash === true
						? this.options.runtime.i18n.t("documents:restoreFromTrash")
						: this.options.runtime.i18n.t("documents:deletedSuccessfully"),
			});
			this.options.navigate(collectionPath(this.collectionSlug));
		} catch (cause) {
			if (generation !== this.#routeGeneration) return;
			this.options.notifications.error({
				title: this.options.runtime.i18n.t("documents:notDeleted"),
				message:
					cause instanceof Error
						? cause.message
						: this.options.runtime.i18n.t("documents:deleteFailed"),
			});
		}
	};

	documentDate = (value: string | undefined) =>
		value === undefined ? "—" : formatAdminDateTime(value, this.options.runtime.i18n);

	#cancelRouteRequests() {
		this.#forceUnlockRequest?.abort();
		this.#copyLocaleRequest?.abort();
		this.#forceUnlockRequest = undefined;
		this.#copyLocaleRequest = undefined;
		this.unlockOperation = false;
		this.copyLocaleOperation = false;
	}

	#enterRoute(
		slug: string,
		documentID: string | undefined,
		locale: string | undefined,
		manifestRevision: number,
		global: boolean,
		editable: boolean,
		prepared?: AdminDocumentDataV1 | AdminCreateDataV1
	) {
		const resourceKey = `${global ? "global" : "collection"}:${slug}:${documentID ?? "new"}`;
		const ownerKey = `${resourceKey}:${locale ?? "default"}`;
		const routeKey = `${ownerKey}:${editable ? "edit" : "read"}`;
		const sameOwner = this.#activeRouteKey.startsWith(`${ownerKey}:`);
		if (
			this.#activeRouteKey.startsWith(`${resourceKey}:`) &&
			this.#activeRouteKey !== routeKey &&
			this.hasUnsavedChanges
		) {
			if (manifestRevision !== this.#appliedManifestRevision) {
				this.#appliedManifestRevision = manifestRevision;
				this.#applySchemaUpdate();
			}
			return;
		}
		if (this.#activeRouteKey === routeKey && this.collection !== undefined) {
			if (manifestRevision !== this.#appliedManifestRevision) {
				this.#appliedManifestRevision = manifestRevision;
				this.#applySchemaUpdate();
			} else if (prepared !== undefined && !this.hasUnsavedChanges) {
				this.#load(slug, documentID, false, prepared);
			}
			return;
		}
		this.#appliedManifestRevision = manifestRevision;
		if (this.#activeRouteKey !== routeKey && !sameOwner) {
			// A later visit to the same URL is a different owner for mutation completions.
			this.#routeGeneration += 1;
			this.#cancelRouteRequests();
			this.publicationOperation = false;
			this.duplicateOperation = false;
			this.deleteDialogOpen = false;
			this.currentDocument = undefined;
			this.lastSavedAt = undefined;
			this.upload.reset(this.currentDocument);
		}
		this.#cancelLoad();
		this.#activeContentLocale = locale;
		if (this.#activeRouteKey !== "" && this.#activeRouteKey !== routeKey && !sameOwner) {
			this.#saveRequest?.abort();
			this.#saveRequest = undefined;
		}
		const collection = (
			global ? this.options.runtime.manifest?.globals : this.options.runtime.manifest?.collections
		)?.find((item) => item.slug === slug);
		if (collection === undefined) {
			this.#activeRouteKey = routeKey;
			this.lock.release();
			this.collection = undefined;
			this.collectionAvailable = false;
			this.currentDocument = undefined;
			this.upload.reset(this.currentDocument);
			this.form.reset({});
			this.form.setResource({ collection: slug, id: documentID, global });
			this.loading = false;
			this.error = this.options.runtime.i18n.t("documents:collectionUnavailable");
			this.form.setAccess(undefined, documentID === undefined ? "create" : "update");
			return;
		}
		this.collection = collection;
		this.form.setResource({
			collection: collection.slug,
			id: documentID,
			global,
		});
		this.lock.releaseUnless(collection.slug, documentID);
		if (!editable) this.lock.release();
		this.collectionAvailable = true;
		this.#activeRouteKey = routeKey;
		this.saveOutcomeUncertain = false;
		this.form.setAccess(undefined, documentID === undefined ? "create" : "update");
		if (documentID === undefined) {
			this.form.reset(initialFormValues(collection.fields), collection.fields);
			this.form.setLocalization(locale);
			this.currentDocument = undefined;
			this.lock.release();
			this.#restoreDraft();
			this.#load(collection.slug, undefined, false, prepared);
			return;
		}
		this.#load(collection.slug, documentID, false, prepared);
	}

	#applySchemaUpdate() {
		const previous = this.collection;
		if (previous === undefined) return;
		const next = (
			this.globalResource
				? this.options.runtime.manifest?.globals
				: this.options.runtime.manifest?.collections
		)?.find((item) => item.id === previous.id);
		this.#cancelLoad();
		this.#routeGeneration += 1;
		this.#cancelRouteRequests();
		this.#saveRequest?.abort();
		this.publicationOperation = false;
		this.duplicateOperation = false;
		this.lock.pause();
		const result = this.form.reconcile(
			previous.fields,
			next?.fields ?? previous.fields,
			this.creating
		);
		if (next === undefined) {
			this.lock.release();
			this.collectionAvailable = false;
			this.form.writeBlocked = true;
			this.form.setAccess(undefined, this.creating ? "create" : "update");
			this.options.notifications.error({
				title: this.options.runtime.i18n.t("documents:collectionRemoved"),
			});
			return;
		}

		this.collection = next;
		this.collectionAvailable = true;
		if (!next.capabilities.locking || previous.slug !== next.slug) this.lock.release();
		if (result.detached.length > 0) {
			this.options.notifications.warning({
				title: this.options.runtime.i18n.t("documents:schemaUpdatedWithDraft"),
				message: this.options.runtime.i18n.t("documents:incompatibleDraftValues", {
					count: result.detached.length,
				}),
			});
		}
		if (previous.slug !== next.slug) {
			// The resulting URL is the same editor under its renamed resource, so its
			// next route synchronization must not reload and replace the retained draft.
			const documentID = this.globalResource ? next.slug : this.documentID;
			this.#activeRouteKey = `${this.globalResource ? "global" : "collection"}:${next.slug}:${documentID ?? "new"}:${this.contentLocale ?? "default"}:${this.options.editable ? "edit" : "read"}`;
			this.form.setResource({
				collection: next.slug,
				id: documentID,
				global: this.globalResource,
			});
			this.options.navigate(
				withContentLocale(
					this.globalResource
						? globalPath(next.slug)
						: this.documentID === undefined
							? createDocumentPath(next.slug)
							: documentPath(next.slug, this.documentID),
					this.contentLocale
				),
				{ replace: true }
			);
		}
		// Field access is keyed by path, so the old envelope is invalid after reconciliation.
		this.form.setAccess(undefined, this.creating ? "create" : "update");
		this.form.writeBlocked = true;
		this.#load(
			next.slug,
			this.globalResource ? next.slug : this.documentID,
			this.currentDocument !== undefined
		);
	}

	#restoreDraft() {
		const collection = this.collection;
		if (collection === undefined) return false;
		const checkpoint = takeFormDraft(collection.id, this.documentID);
		if (checkpoint === undefined) return false;
		const result = this.form.recover(
			{ values: checkpoint.values, original: checkpoint.original },
			checkpoint.collection.fields,
			collection.fields
		);
		const title =
			result.restoredFields === 0
				? this.options.runtime.i18n.t("documents:noDraftFieldsRestored")
				: this.options.runtime.i18n.t("documents:draftFieldsRestored", {
						count: result.restoredFields,
					});
		const message =
			result.detached.length === 0
				? undefined
				: this.options.runtime.i18n.t("documents:incompatibleDraftValues", {
						count: result.detached.length,
					});
		if (result.restoredFields === 0 || result.detached.length > 0)
			this.options.notifications.warning({ title, message });
		else this.options.notifications.success({ title });
		return result.restoredFields > 0;
	}

	#cancelLoad() {
		this.#loadRequest?.abort();
		this.#loadRequest = undefined;
		this.loading = false;
	}

	async #load(
		slug: string,
		documentID?: string,
		preserveDocument = false,
		prepared?: AdminDocumentDataV1 | AdminCreateDataV1
	) {
		this.#loadRequest?.abort();
		this.#loadRequest = undefined;
		if (prepared !== undefined && this.#adoptPreparedLoad(prepared, slug, documentID)) return;
		const request = new AbortController();
		this.#loadRequest = request;
		const { signal } = request;
		const global = this.globalResource;
		const locale = this.contentLocale;
		// A refresh may replace the draft it started with, never edits made while it was pending.
		const revision = this.form.revision;
		const uploadRevision = this.upload.revision;
		const changeGeneration = this.#formChangeGeneration;
		this.loading = true;
		this.error = undefined;
		try {
			const [document, access] = await Promise.all([
				documentID === undefined || preserveDocument
					? undefined
					: global
						? this.options.runtime.client.global(slug, { signal, locale })
						: this.options.runtime.client.find(slug, documentID, { signal, locale }),
				global
					? this.options.runtime.client.globalAccess(slug, { signal, locale })
					: this.options.runtime.client.collectionAccess(slug, {
							id: documentID,
							...(documentID === undefined ? { data: $state.snapshot(this.form.values) } : {}),
							signal,
							locale,
						}),
			]);
			if (signal.aborted) return;
			const formUnchanged =
				this.form.revision === revision &&
				this.upload.revision === uploadRevision &&
				this.#formChangeGeneration === changeGeneration;
			this.form.setAccess(access, documentID === undefined ? "create" : "update");
			if (document !== undefined && formUnchanged) {
				const uncertain = this.saveOutcomeUncertain;
				if (uncertain) clearFormDraft(this.collection?.id ?? slug, documentID);
				this.#applyDocument(document, !uncertain);
			}
			if (
				documentID !== undefined &&
				!global &&
				this.options.editable &&
				this.collection?.capabilities.locking === true
			) {
				// The form is complete before the lock request starts. Writes remain
				// disabled until the server confirms ownership.
				this.form.writeBlocked = true;
				this.loading = false;
				await tick();
				if (signal.aborted || this.#loadRequest !== request) return;
				await this.lock.acquire(
					slug,
					documentID,
					this.collection.documentLockSettings?.durationSeconds ?? 120
				);
			} else {
				this.lock.release();
			}
		} catch (cause) {
			if (signal.aborted) return;
			this.error =
				cause instanceof Error
					? cause.message
					: this.options.runtime.i18n.t("documents:loadFailed");
		} finally {
			if (this.#loadRequest === request) {
				this.#loadRequest = undefined;
				this.loading = false;
			}
		}
	}

	#adoptPreparedLoad(
		prepared: AdminDocumentDataV1 | AdminCreateDataV1,
		slug: string,
		documentID?: string
	) {
		if (
			"values" in prepared &&
			!samePreparedCreateValues(prepared.values, $state.snapshot(this.form.values))
		)
			return false;
		const document = "document" in prepared ? prepared.document : undefined;
		const access = prepared.access;
		this.loading = false;
		this.error = document?.error?.message ?? access.error?.message;
		if (this.error !== undefined) {
			this.form.setAccess(undefined, documentID === undefined ? "create" : "update");
			return true;
		}
		this.form.setAccess(access.value, documentID === undefined ? "create" : "update");
		if (document?.value !== undefined) this.#applyDocument(document.value, true);
		if (
			documentID !== undefined &&
			!this.globalResource &&
			this.options.editable &&
			this.collection?.capabilities.locking === true
		) {
			this.form.writeBlocked = true;
			this.#acquirePreparedLock(this.#routeGeneration, slug, documentID);
		} else {
			this.lock.release();
		}
		return true;
	}

	async #acquirePreparedLock(generation: number, slug: string, documentID: string) {
		await tick();
		if (
			generation !== this.#routeGeneration ||
			this.documentID !== documentID ||
			!this.options.editable
		)
			return;
		await this.lock.acquire(
			slug,
			documentID,
			this.collection?.documentLockSettings?.durationSeconds ?? 120
		);
	}

	async #refreshAccess(documentID: string) {
		const generation = this.#routeGeneration;
		const collectionSlug = this.collectionSlug;
		const global = this.globalResource;
		const locale = this.contentLocale;
		try {
			const access = global
				? await this.options.runtime.client.globalAccess(collectionSlug, { locale })
				: await this.options.runtime.client.collectionAccess(collectionSlug, {
						id: documentID,
						locale,
					});
			if (generation !== this.#routeGeneration) return;
			this.form.setAccess(access, "update");
			this.error = undefined;
		} catch (cause) {
			if (generation !== this.#routeGeneration) return;
			this.form.setAccess(undefined, "update");
			this.error =
				cause instanceof Error
					? cause.message
					: this.options.runtime.i18n.t("documents:accessEvaluationFailed");
		}
	}

	#applyDocument(document: AdminDocument, recoverDraft = false) {
		this.saveOutcomeUncertain = false;
		this.currentDocument = document;
		this.upload.reset(document);
		this.form.reset(
			documentFormValues(this.collection?.fields ?? [], document),
			this.collection?.fields ?? []
		);
		this.form.setLocalization(this.contentLocale, document._localization?.sources);
		if (recoverDraft) this.#restoreDraft();
	}

	#documentString(name: string) {
		const value = this.currentDocument?.[name];
		return typeof value === "string" && value.length > 0 ? value : undefined;
	}
}
