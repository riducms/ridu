import { connectDocumentLiveValidation } from "@admin/core/forms/live-validation.svelte";
import type { AdminCreateDataV1, AdminDocumentDataV1, SchemaCollection } from "@riducms/protocol";
import { RiduError } from "@riducms/sdk";
import { formatAdminDateTime } from "@admin/core/i18n/format-admin-date-time";
import { tick } from "svelte";

import type { AdminDocument } from "@admin/core/api/admin-client";
import { FormController, FormValidationError } from "@admin/core/forms/form-controller.svelte";
import {
	clearFormDraft,
	formDraftAccessPath,
	formDraftBase,
	peekFormDraft,
	saveFormDraft,
	sameFormDraftBase,
	type FormDraftCheckpoint,
} from "@admin/core/forms/form-draft-recovery";
import {
	changedFormValues,
	documentFormValues,
	initialFormValues,
	reconcileFormSchema,
	recoverFormDraft,
	submissionFormValues,
} from "@admin/core/forms/form-schema";
import { invalidFieldLabels } from "@admin/core/forms/form-validation";
import { correlateFormIssues } from "@admin/core/forms/form-issue-correlation";
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
import { sameValue } from "@admin/features/versions/version-diff";

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
	serverSaveConflict = $state(false);
	duplicateOperation = $state(false);
	deleteDialogOpen = $state(false);
	currentDocument = $state.raw<AdminDocument>();
	versionCount = $state<number>();
	lastSavedAt = $state<number>();
	unlockOperation = $state(false);
	copyLocaleOperation = $state(false);
	recoveryConflict = $state.raw<FormDraftCheckpoint>();
	#recoveryNotificationID?: number | string;
	#activeRouteKey = "";
	#routeGeneration = 0;
	#activeContentLocale?: string;
	#appliedManifestRevision = 0;
	#scheduledRouteKey = "";
	#formChangeGeneration = 0;
	#stopObservingFormChanges: () => void;
	#loadRequest?: AbortController;
	#versionCountRequest?: AbortController;
	#saveRequest?: AbortController;
	#forceUnlockRequest?: AbortController;
	#copyLocaleRequest?: AbortController;

	constructor(private readonly options: DocumentControllerOptions) {
		this.upload = new UploadDraft(
			options.runtime.client,
			() => this.collectionSlug,
			options.runtime.i18n
		);
		this.form = new FormController(
			{},
			options.runtime.i18n,
			undefined,
			() => this.recoveryConflict === undefined
		);
		this.lock = new DocumentLockController({
			runtime: options.runtime,
			notifications: options.notifications,
			form: this.form,
		});
		this.#stopObservingFormChanges = this.form.observeChanges(() => {
			this.#formChangeGeneration += 1;
		});
		connectDocumentLiveValidation(this.form, options.runtime.client, () => this.draftsCollection);
		this.#scheduleRouteSync(this.#routeSnapshot());
		$effect.pre(() => {
			this.#scheduleRouteSync(this.#routeSnapshot());
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
				this.#dismissRecoveryNotification();
				this.#cancelRouteRequests();
				this.#loadRequest?.abort();
				this.#clearVersionCount();
				this.#stopObservingFormChanges();
				this.form.disposeBindings();
				this.upload.dispose();
				this.#saveRequest?.abort();
				this.lock.release();
			};
		});

		$effect(() => {
			const interval = this.collection?.versionSettings?.autosaveIntervalSeconds ?? 0;
			if (interval <= 0) return;

			const timer = window.setInterval(async () => {
				if (!this.canSave || this.publicationOperation || this.serverSaveConflict) return;
				// Account creation needs a password supplied by the explicit Save flow.
				// Autosave must not create identities or treat missing credentials as an
				// uncertain network mutation.
				if (this.creatingAuthUser) return;
				if (this.creating && !this.form.dirty && !this.upload.dirty) return;
				if (this.currentStatus === "published" && !this.draftsCollection) this.checkpointDraft();
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

	get hasUnsavedChanges() {
		return this.form.dirty || this.upload.dirty || this.recoveryConflict !== undefined;
	}

	get hasSavedDraftChanges() {
		return this.draftsCollection && this.currentDocument?._hasDraftChanges === true;
	}

	get canDiscardSavedDraft() {
		return (
			!this.creating &&
			this.hasSavedDraftChanges &&
			!this.hasUnsavedChanges &&
			!this.serverSaveConflict &&
			!this.saveOutcomeUncertain &&
			!this.form.submitting &&
			!this.publicationOperation &&
			!this.lock.lockedByAnotherEditor &&
			this.form.access?.operations.update === true
		);
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
			this.recoveryConflict === undefined &&
			!this.serverSaveConflict &&
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
			this.recoveryConflict === undefined &&
			!this.lock.lockedByAnotherEditor &&
			!this.form.writeBlocked &&
			this.form.access?.operations.publish === true
		);
	}

	get canUnpublish() {
		return (
			this.recoveryConflict === undefined &&
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
			this.recoveryConflict === undefined &&
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

	checkpointDraft = (): boolean => {
		const collection = this.collection;
		if (collection === undefined) return false;
		// An unresolved checkpoint stays in storage even though the saved form is clean.
		if (this.recoveryConflict !== undefined) return false;
		if (this.form.dirty) {
			// Edits survive a failed refresh or a pending access check; only a missing
			// base document makes them unrecoverable.
			if (!this.creating && this.currentDocument === undefined) return false;
			return saveFormDraft(
				collection,
				this.documentID,
				$state.snapshot(this.form.values),
				$state.snapshot(this.form.original),
				formDraftBase(this.currentDocument),
				this.#draftLocale,
				this.options.runtime.manifest?.blocks
			);
		}
		const access = this.form.access;
		if (access === undefined || this.error !== undefined) return false;
		if (this.creating ? access.operations.create === true : access.operations.update === true)
			clearFormDraft(collection.id, this.documentID, this.#draftLocale);
		return true;
	};

	discardChanges = () => {
		const collection = this.collection;
		if (collection !== undefined) clearFormDraft(collection.id, this.documentID, this.#draftLocale);
		this.recoveryConflict = undefined;
		this.serverSaveConflict = false;
		this.#dismissRecoveryNotification();
		this.form.discard();
		this.form.setLocalization(this.contentLocale, this.currentDocument?._localization?.sources);
		this.upload.reset(this.currentDocument);
	};

	get canKeepRecoveredChanges() {
		return (
			this.recoveryConflict !== undefined &&
			!this.loading &&
			this.error === undefined &&
			!this.form.writeBlocked &&
			!this.form.submitting &&
			!this.lock.lockedByAnotherEditor &&
			this.form.access?.operations.read === true &&
			this.form.access?.operations.update === true
		);
	}

	get recoveryComparison() {
		const checkpoint = this.recoveryConflict;
		const fields = this.collection?.fields ?? [];
		if (checkpoint === undefined || this.form.access?.operations.read !== true) return [];
		const reconciled = reconcileFormSchema(checkpoint, checkpoint.collection.fields, fields);
		const readable = (values: Record<string, unknown>) =>
			submissionFormValues(fields, values, this.#draftAllowed(values, false));
		const original = readable(reconciled.original);
		const yours = readable(reconciled.values);
		const canRead = (path: string, canonicalPath: string) => this.form.canRead(path, canonicalPath);
		const latest = submissionFormValues(fields, $state.snapshot(this.form.original), canRead);
		return fields
			.filter(
				(field) =>
					this.form.canRead(field.path) &&
					(!sameValue(original[field.name], yours[field.name]) ||
						!sameValue(original[field.name], latest[field.name]))
			)
			.map((field) => ({
				id: field.id,
				path: field.path,
				field,
				label: this.options.runtime.i18n.text(
					field.admin.label ?? field.name,
					field.admin.labelTranslations
				),
				original: original[field.name],
				yours: yours[field.name],
				latest: latest[field.name],
			}));
	}

	keepRecoveredChanges = () => {
		if (!this.canKeepRecoveredChanges) return;
		const checkpoint = this.recoveryConflict;
		if (checkpoint === undefined) return;
		this.recoveryConflict = undefined;
		this.serverSaveConflict = false;
		clearFormDraft(checkpoint.collection.id, this.documentID, this.#draftLocale);
		this.#applyDraft(checkpoint);
	};

	reviewServerConflict = async () => {
		if (!this.serverSaveConflict || this.creating || this.currentDocument === undefined) return;
		// The existing recovery checkpoint retains the submitted edits while the latest
		// working document is loaded. The normal recovery comparison then fences the retry.
		if (this.form.dirty && !this.checkpointDraft()) {
			this.options.notifications.error({
				title: this.options.runtime.i18n.t("documents:reviewLatestUnavailable"),
			});
			return;
		}
		await this.refresh();
		if (this.error === undefined && this.recoveryConflict === undefined && !this.form.dirty)
			this.serverSaveConflict = false;
	};

	forceUnlock = async () => {
		const { runtime, notifications } = this.options;
		if (this.collection === undefined || this.currentDocument === undefined) return;
		const request = new AbortController();
		this.#forceUnlockRequest?.abort();
		this.#forceUnlockRequest = request;
		this.unlockOperation = true;
		try {
			await runtime.client.auth.forceUnlock(
				{ collection: this.collection.slug, id: this.currentDocument.id },
				{ signal: request.signal }
			);
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
			this.recoveryConflict !== undefined ||
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
		const savingDraft = this.draftsCollection && !publish && !this.creatingAuthUser;
		if (publishingChanges && !this.canPublish) return false;
		this.#dismissRecoveryNotification();
		const collectionSlug = this.collectionSlug;
		const documentID = this.documentID;
		const request = new AbortController();
		this.#saveRequest?.abort();
		this.#saveRequest = request;
		this.#cancelLoad();
		const wasCreating = this.creating;
		const submittedSnapshot = $state.snapshot(this.form.values);
		const changeGeneration = this.#formChangeGeneration;
		try {
			const saved = await this.form.submit(
				this.validationFields,
				this.creating,
				async (values) => {
					if (!wasCreating)
						values = changedFormValues(this.validationFields, values, this.form.original, (path) =>
							this.form.isInherited(path)
						);
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
							...(savingDraft ? { draft: true } : {}),
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
								{
									revision: this.currentRevision,
									signal: request.signal,
									locale: this.contentLocale,
									...(savingDraft ? { draft: true } : {}),
								}
							);
						}
						if (publishingChanges) {
							return this.options.runtime.client.publishChanges(
								collectionSlug,
								documentID,
								values,
								{
									revision: this.currentRevision,
									signal: request.signal,
									locale: this.contentLocale,
								}
							);
						}
						return this.options.runtime.client.update(collectionSlug, documentID, values, {
							revision: this.currentRevision,
							...(savingDraft ? { draft: true } : {}),
							signal: request.signal,
							locale: this.contentLocale,
						});
					}
					if (this.collection?.capabilities.auth) {
						if (password === undefined)
							throw new Error(this.options.runtime.i18n.t("documents:enterNewAccountPassword"));
						return this.options.runtime.client.auth.createUser(
							{ collection: collectionSlug, data: values, password },
							{
								signal: request.signal,
								locale: this.contentLocale,
								...(this.versionedCollection
									? { draft: this.draftsCollection ? !publish : false }
									: {}),
							}
						);
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
					if (this.upload.file === undefined)
						throw new Error(this.options.runtime.i18n.t("uploads:chooseFileBeforeSaving"));
					return this.options.runtime.client.upload(collectionSlug, this.upload.file, {
						filename: this.upload.filename,
						image: this.upload.image,
						...(this.versionedCollection
							? { draft: this.draftsCollection ? !publish : false }
							: {}),
						data: values,
						signal: request.signal,
						locale: this.contentLocale,
					});
				},
				{
					mode: savingDraft ? "draft" : "complete",
					allowEditsDuringRequest: silent && !wasCreating && !this.uploadCollection,
					// Apply the server baseline once, after checking this request's lifetime.
					// Committing submitted values first remounts editors between two resets.
					commitBaseline: false,
				}
			);
			if (request.signal.aborted) return false;
			this.saveOutcomeUncertain = false;
			this.serverSaveConflict = false;
			const editedDuringSave = this.#formChangeGeneration !== changeGeneration;
			const preserveEditors = silent && !wasCreating && !this.uploadCollection;
			let recoveryNeeded = false;
			if (editedDuringSave || preserveEditors) {
				const savedValues = documentFormValues(this.collection?.fields ?? [], saved);
				if (this.form.acceptSavedBaseline(savedValues, submittedSnapshot)) {
					this.currentDocument = saved;
					this.form.setLocalization(this.contentLocale, saved._localization?.sources);
					if (editedDuringSave) this.checkpointDraft();
				} else {
					const collection = this.collection;
					const checkpointed =
						collection !== undefined &&
						saveFormDraft(
							collection,
							this.documentID,
							$state.snapshot(this.form.values),
							submittedSnapshot,
							formDraftBase(this.currentDocument),
							this.#draftLocale,
							this.options.runtime.manifest?.blocks
						);
					const checkpoint = checkpointed
						? peekFormDraft(collection.id, this.documentID, this.#draftLocale)
						: undefined;
					if (checkpoint === undefined) {
						this.serverSaveConflict = true;
						this.options.notifications.error({
							title: this.options.runtime.i18n.t("documents:reviewLatestUnavailable"),
						});
						return false;
					}
					this.#applyDocument(saved);
					this.recoveryConflict = checkpoint;
					recoveryNeeded = true;
				}
			} else {
				this.#applyDocument(saved);
			}
			this.lastSavedAt = Date.now();
			this.options.runtime.documentsChanged();
			if (!editedDuringSave && !recoveryNeeded)
				clearFormDraft(
					this.collection?.id ?? this.collectionSlug,
					this.documentID,
					this.#draftLocale
				);
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
				if (!request.signal.aborted) this.#loadVersionCount();
			}
			return !request.signal.aborted;
		} catch (cause) {
			if (request.signal.aborted) return false;
			if (!wasCreating && cause instanceof RiduError && cause.status === 409) {
				this.serverSaveConflict = true;
				this.checkpointDraft();
			}
			if (
				(this.uploadCollection || wasCreating) &&
				!(cause instanceof FormValidationError) &&
				(!(cause instanceof RiduError) || cause.status >= 500)
			) {
				this.saveOutcomeUncertain = true;
				if (!silent) {
					this.options.notifications.error({
						title: this.options.runtime.i18n.t("documents:saveOutcomeUnknown"),
						message: this.options.runtime.i18n.t(
							wasCreating
								? "documents:createOutcomeUnknownDescription"
								: "documents:saveOutcomeUnknownDescription"
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
			this.serverSaveConflict ||
			this.saveOutcomeUncertain
		)
			return;
		this.#cancelLoad();
		this.publicationOperation = true;
		const releaseEditing = this.form.beginManualSubmission();
		const generation = this.#routeGeneration;
		const submittedValues = $state.snapshot(this.form.values);
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
			this.#loadVersionCount();
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
			if (cause instanceof RiduError) {
				if (cause.status === 409) {
					this.serverSaveConflict = true;
					this.checkpointDraft();
				}
				if (cause.issues.length > 0) {
					this.form.issues = correlateFormIssues(
						this.validationFields,
						submittedValues,
						this.form.values,
						cause.issues
					);
				}
			}
			this.options.notifications.error({
				title: this.options.runtime.i18n.t("documents:statusNotChanged"),
				message:
					cause instanceof Error
						? cause.message
						: this.options.runtime.i18n.t("documents:statusChangeFailed"),
			});
		} finally {
			releaseEditing();
			if (generation === this.#routeGeneration) this.publicationOperation = false;
		}
	};

	discardSavedDraft = async () => {
		if (!this.canDiscardSavedDraft || this.documentID === undefined) return;
		const generation = this.#routeGeneration;
		this.publicationOperation = true;
		const releaseEditing = this.form.beginManualSubmission();
		this.#cancelLoad();
		try {
			const saved = this.globalResource
				? await this.options.runtime.client.discardGlobalDraft(this.collectionSlug, {
						revision: this.currentRevision,
						locale: this.contentLocale,
					})
				: await this.options.runtime.client.discardDraft(this.collectionSlug, this.documentID, {
						revision: this.currentRevision,
						locale: this.contentLocale,
					});
			if (generation !== this.#routeGeneration) return;
			this.#applyDocument(saved);
			this.options.runtime.documentsChanged();
			await this.#refreshAccess(this.documentID);
			if (generation !== this.#routeGeneration) return;
			this.#loadVersionCount();
			this.options.notifications.success({
				title: this.options.runtime.i18n.t("documents:savedDraftDiscarded"),
			});
		} catch (cause) {
			if (generation !== this.#routeGeneration) return;
			if (cause instanceof RiduError && cause.status === 409) this.serverSaveConflict = true;
			this.options.notifications.error({
				title: this.options.runtime.i18n.t("documents:discardSavedDraftFailed"),
				message: cause instanceof Error ? cause.message : undefined,
			});
		} finally {
			releaseEditing();
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
		this.#clearVersionCount();
		this.#appliedManifestRevision = manifestRevision;
		if (this.#activeRouteKey !== routeKey && !sameOwner) {
			// A later visit to the same URL is a different owner for mutation completions.
			this.#routeGeneration += 1;
			this.#dismissRecoveryNotification();
			this.recoveryConflict = undefined;
			this.serverSaveConflict = false;
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
		this.#clearVersionCount();
		this.#routeGeneration += 1;
		this.#dismissRecoveryNotification();
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

	get #draftLocale() {
		return (
			this.contentLocale ?? this.options.runtime.manifest?.application.localization?.defaultLocale
		);
	}

	#createAccessValues() {
		const values = $state.snapshot(this.form.values);
		const collection = this.collection;
		if (collection === undefined) return values;
		const checkpoint = peekFormDraft(collection.id, undefined, this.#draftLocale);
		return checkpoint === undefined
			? values
			: recoverFormDraft(
					{ values, original: $state.snapshot(this.form.original) },
					reconcileFormSchema(checkpoint, checkpoint.collection.fields, collection.fields),
					collection.fields
				).values;
	}

	#draftAllowed(values: Record<string, unknown>, writable: boolean) {
		const fields = this.collection?.fields ?? [];
		const current = this.creating ? values : this.form.original;
		const scopedRules = new Set<string>();
		submissionFormValues(fields, current, (path, canonicalPath) => {
			if (this.form.fieldCapabilities(path, canonicalPath) !== undefined)
				scopedRules.add(canonicalPath);
			return true;
		});
		return (path: string, canonicalPath: string) => {
			let currentPath = formDraftAccessPath(fields, values, current, path);
			// New occurrences can use the ordinary unrestricted field contract. A
			// scoped rule needs an existing identity; another row cannot prove access.
			if (currentPath === undefined) {
				if (
					scopedRules.has(canonicalPath) ||
					this.form.fieldCapabilities(canonicalPath) !== undefined
				)
					return false;
				currentPath = canonicalPath;
			}
			// Recovery filters evaluated permissions. Temporary locks and the pending
			// recovery choice still gate editing through FormController.canWrite.
			return (
				this.form.canRead(currentPath, canonicalPath) &&
				(!writable || this.form.hasWriteAccess(currentPath, canonicalPath))
			);
		};
	}

	#restoreDraft() {
		const collection = this.collection;
		if (collection === undefined) return false;
		if (
			this.form.access === undefined ||
			(this.creating
				? this.form.access.operations.create !== true
				: this.form.access.operations.read !== true)
		)
			return false;
		const checkpoint = peekFormDraft(collection.id, this.documentID, this.#draftLocale);
		if (checkpoint === undefined) return false;
		if (
			this.currentDocument !== undefined &&
			!sameFormDraftBase(checkpoint, this.currentDocument)
		) {
			this.recoveryConflict = checkpoint;
			return false;
		}
		if (!this.creating && this.form.access.operations.update !== true) return false;
		clearFormDraft(collection.id, this.documentID, this.#draftLocale);
		return this.#applyDraft(checkpoint);
	}

	#applyDraft(checkpoint: FormDraftCheckpoint) {
		const collection = this.collection;
		if (collection === undefined) return false;
		const reconciled = reconcileFormSchema(
			checkpoint,
			checkpoint.collection.fields,
			collection.fields
		);
		const allowed = this.#draftAllowed(reconciled.values, true);
		const readable = submissionFormValues(collection.fields, reconciled.values, allowed);
		const all = submissionFormValues(collection.fields, reconciled.values);
		// A partially denied container cannot replace its latest saved children. Retain
		// only complete authorized fields; the server still rechecks every mutation.
		for (const field of collection.fields) {
			if (allowed(field.path, field.path) && sameValue(readable[field.name], all[field.name]))
				continue;
			delete reconciled.values[field.name];
			delete reconciled.original[field.name];
		}
		const result = this.form.recover(reconciled, collection.fields);
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
		this.#dismissRecoveryNotification();
		const generation = this.#routeGeneration;
		// The restored edits stay discardable until the editor saves, discards or leaves.
		const notification =
			result.restoredFields === 0
				? { title, message }
				: {
						title,
						message,
						duration: Number.POSITIVE_INFINITY,
						action: {
							label: this.options.runtime.i18n.t("documents:discard"),
							onClick: () => {
								if (generation === this.#routeGeneration && !this.form.submitting)
									this.discardChanges();
							},
						},
					};
		this.#recoveryNotificationID =
			result.restoredFields === 0 || result.detached.length > 0
				? this.options.notifications.warning(notification)
				: this.options.notifications.success(notification);
		return result.restoredFields > 0;
	}

	#dismissRecoveryNotification() {
		if (this.#recoveryNotificationID === undefined) return;
		this.options.notifications.dismiss(this.#recoveryNotificationID);
		this.#recoveryNotificationID = undefined;
	}

	#cancelLoad() {
		this.#loadRequest?.abort();
		this.#loadRequest = undefined;
		this.loading = false;
	}

	#cancelVersionCount() {
		this.#versionCountRequest?.abort();
		this.#versionCountRequest = undefined;
	}

	#clearVersionCount() {
		this.#cancelVersionCount();
		this.versionCount = undefined;
	}

	async #loadVersionCount() {
		this.#cancelVersionCount();
		const slug = this.collection?.slug;
		const id = this.documentID;
		if (
			!slug ||
			id === undefined ||
			this.currentDocument === undefined ||
			!this.versionedCollection ||
			!this.canReadVersions
		) {
			this.versionCount = undefined;
			return;
		}
		const request = new AbortController();
		this.#versionCountRequest = request;
		try {
			// This is a non-blocking header read. A history failure cannot fail the editor.
			const count = this.globalResource
				? await this.options.runtime.client.countGlobalVersions(slug, {
						signal: request.signal,
						locale: this.contentLocale,
					})
				: await this.options.runtime.client.countVersions(slug, id, {
						signal: request.signal,
						locale: this.contentLocale,
					});
			if (this.#versionCountRequest === request && !request.signal.aborted)
				this.versionCount = count.totalDocs;
		} catch {
			// A count is optional; the history route presents its own actionable error.
			if (this.#versionCountRequest === request && !request.signal.aborted)
				this.versionCount = undefined;
		} finally {
			if (this.#versionCountRequest === request) this.#versionCountRequest = undefined;
		}
	}

	async #load(
		slug: string,
		documentID?: string,
		preserveDocument = false,
		prepared?: AdminDocumentDataV1 | AdminCreateDataV1
	) {
		this.#loadRequest?.abort();
		this.#loadRequest = undefined;
		this.#clearVersionCount();
		if (prepared !== undefined && this.#adoptPreparedLoad(prepared, slug, documentID)) return;
		const request = new AbortController();
		this.#loadRequest = request;
		const { signal } = request;
		const global = this.globalResource;
		const locale = this.contentLocale;
		if (this.recoveryConflict !== undefined)
			this.form.setAccess(undefined, documentID === undefined ? "create" : "update");
		// A refresh may replace the draft it started with, never edits made while it was pending.
		const revision = this.form.revision;
		const uploadRevision = this.upload.revision;
		const changeGeneration = this.#formChangeGeneration;
		this.loading = true;
		this.error = undefined;
		try {
			const accessValues =
				documentID === undefined ? this.#createAccessValues() : $state.snapshot(this.form.original);
			const [document, access] = await Promise.all([
				documentID === undefined || preserveDocument
					? undefined
					: global
						? this.options.runtime.client.global(slug, {
								signal,
								locale,
								...(this.draftsCollection ? { draft: true } : {}),
							})
						: this.options.runtime.client.find(slug, documentID, {
								signal,
								locale,
								...(this.draftsCollection ? { draft: true } : {}),
							}),
				global
					? this.options.runtime.client.globalAccess(slug, { signal, locale })
					: this.options.runtime.client.collectionAccess(slug, {
							id: documentID,
							...(documentID === undefined ? { data: accessValues } : {}),
							signal,
							locale,
						}),
			]);
			if (signal.aborted) return;
			const formUnchanged =
				this.form.revision === revision &&
				this.upload.revision === uploadRevision &&
				this.#formChangeGeneration === changeGeneration;
			// Retained edits may have changed occurrence order since access was evaluated.
			const retainForm = !formUnchanged || (documentID !== undefined && preserveDocument);
			this.form.setAccess(
				access,
				documentID === undefined ? "create" : "update",
				retainForm
					? document === undefined
						? accessValues
						: documentFormValues(this.collection?.fields ?? [], document)
					: undefined
			);
			if (document !== undefined && formUnchanged) {
				const uncertain = this.saveOutcomeUncertain;
				if (uncertain) clearFormDraft(this.collection?.id ?? slug, documentID, this.#draftLocale);
				this.#applyDocument(document, !uncertain);
			}
			this.#loadVersionCount();
			if (documentID === undefined && formUnchanged) this.#restoreDraft();
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
			this.form.setAccess(undefined, documentID === undefined ? "create" : "update");
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
			!samePreparedCreateValues(prepared.values, this.#createAccessValues())
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
		this.#loadVersionCount();
		if (documentID === undefined) this.#restoreDraft();
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
		const accessValues = $state.snapshot(this.form.original);
		try {
			const access = global
				? await this.options.runtime.client.globalAccess(collectionSlug, { locale })
				: await this.options.runtime.client.collectionAccess(collectionSlug, {
						id: documentID,
						locale,
					});
			if (generation !== this.#routeGeneration) return;
			this.form.setAccess(access, "update", accessValues);
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
		this.#dismissRecoveryNotification();
		this.recoveryConflict = undefined;
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
