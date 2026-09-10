import { connectDocumentLiveValidation } from "@admin/core/forms/live-validation.svelte";
import type { DocumentLockEnvelope, SchemaCollection } from "@riducms/protocol";
import { RiduError } from "@riducms/sdk";
import { tick, untrack } from "svelte";

import type { AdminDocument, AdminVersion } from "@admin/core/api/admin-client";
import { FormController, FormValidationError } from "@admin/core/forms/form-controller.svelte";
import {
	clearFormDraft,
	saveFormDraft,
	takeFormDraft,
} from "@admin/core/forms/form-draft-recovery";
import { documentFormValues, initialFormValues } from "@admin/core/forms/form-schema";
import { invalidFieldLabels } from "@admin/core/forms/form-validation";
import type { NotificationCenter } from "@admin/core/notifications/notification-center.svelte";
import {
	collectionPath,
	createDocumentPath,
	documentPath,
	globalPath,
} from "@admin/core/routing/admin-paths";
import type { AdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
import { documentHeadingField } from "@admin/features/documents/document-heading";

type Navigate = (to: string, options?: { replace?: boolean }) => void;

interface DocumentControllerOptions {
	runtime: AdminRuntime;
	notifications: NotificationCenter;
	navigate: Navigate;
	get slug(): string;
	get documentID(): string | undefined;
	get global(): boolean;
	get view(): "edit" | "api";
	get locale(): string | undefined;
}

const uploadMetadataFields = new Set([
	"filename",
	"mimeType",
	"filesize",
	"url",
	"objectKey",
	"width",
	"height",
	"sizes",
	"focalX",
	"focalY",
	"cropX",
	"cropY",
	"cropWidth",
	"cropHeight",
]);

export class DocumentController {
	readonly form: FormController;
	collection = $state.raw<SchemaCollection>();
	collectionAvailable = $state(true);
	loading = $state(true);
	error = $state<string>();
	selectedFiles = $state<FileList>();
	currentRevision = $state(0);
	currentStatus = $state<"draft" | "published">("published");
	versions = $state.raw<AdminVersion[]>([]);
	versionOperation = $state(false);
	imageOperation = $state(false);
	imageOutcomeUncertain = $state(false);
	saveOutcomeUncertain = $state(false);
	duplicateOperation = $state(false);
	deleteDialogOpen = $state(false);
	restoreDialogOpen = $state(false);
	pendingRestore = $state.raw<AdminVersion>();
	currentDocument = $state.raw<AdminDocument>();
	documentView = $state("edit");
	copiedAssetURL = $state(false);
	documentLock = $state.raw<DocumentLockEnvelope>();
	lockOperation = $state(false);
	#activeRouteKey = "";
	#activeContentLocale?: string;
	#appliedManifestRevision = 0;
	#copyResetTimer?: number;
	#lockRefreshTimer?: number;
	#lockRoute?: { collection: string; documentID: string };
	#saveRequest?: AbortController;
	#imageRequest?: AbortController;

	constructor(readonly options: DocumentControllerOptions) {
		this.form = new FormController({}, options.runtime.i18n);
		connectDocumentLiveValidation(this.form, options.runtime.client);
		$effect(() => {
			const slug = this.options.slug;
			const documentID = this.options.global ? slug : this.options.documentID;
			const locale = this.options.locale;
			// Route setup reads the manifest inside an untracked controller operation, so manifest
			// readiness must remain an explicit synchronization dependency for direct lazy-route loads.
			if (this.options.runtime.manifestRevision === 0) return;
			return untrack(() => this.#enterRoute(slug, documentID, locale));
		});

		$effect(() => {
			const view = this.options.view;
			untrack(() => {
				if (view === "api") this.documentView = "api";
				else if (this.documentView === "api") this.documentView = "edit";
			});
		});

		$effect(() => {
			const revision = this.options.runtime.manifestRevision;
			if (revision === 0 || revision === this.#appliedManifestRevision) return;
			if (this.#appliedManifestRevision === 0) {
				this.#appliedManifestRevision = revision;
				return;
			}
			this.#appliedManifestRevision = revision;
			untrack(() => this.#applySchemaUpdate());
		});

		$effect(() => {
			const checkpoint = () => this.checkpointDraft();
			const leavePage = () => {
				checkpoint();
				this.#releaseLock();
			};
			window.addEventListener("pagehide", leavePage);
			const hot = import.meta.hot;
			hot?.on("vite:beforeFullReload", checkpoint);
			return () => {
				window.removeEventListener("pagehide", leavePage);
				hot?.off("vite:beforeFullReload", checkpoint);
			};
		});

		$effect(() => {
			const interval = this.collection?.versionSettings?.autosaveIntervalSeconds ?? 0;
			const documentID = this.documentID;
			if (documentID === undefined || interval <= 0) return;

			const timer = window.setInterval(async () => {
				if (!this.canSave || this.versionOperation) return;
				// A background update to a published document would make its edits public.
				// Keep a recoverable local checkpoint until the author explicitly chooses
				// Publish changes; draft documents can use the ordinary server mutation.
				if (this.currentStatus === "published") this.checkpointDraft();
				else await this.save({ silent: true });
			}, interval * 1_000);
			return () => window.clearInterval(timer);
		});

		$effect(() => () => {
			this.form.disposeBindings();
			this.#saveRequest?.abort();
			this.#imageRequest?.abort();
			if (this.#copyResetTimer !== undefined) window.clearTimeout(this.#copyResetTimer);
			if (this.#lockRefreshTimer !== undefined) window.clearInterval(this.#lockRefreshTimer);
			this.#releaseLock();
		});
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

	get selectedFile() {
		return this.selectedFiles?.item(0) ?? undefined;
	}

	get hasUnsavedChanges() {
		return this.form.dirty || (this.creating && this.selectedFile !== undefined);
	}

	get canSave() {
		const operationAllowed = this.creating
			? this.form.access?.operations.create === true
			: this.form.access?.operations.update === true;
		const publicationAllowed =
			!this.creating || !this.versionedCollection || this.draftsCollection || this.canPublish;
		const inputReady = this.creating
			? !this.uploadCollection || this.selectedFile !== undefined
			: this.hasUnsavedChanges;
		return (
			this.collectionAvailable &&
			!this.saveOutcomeUncertain &&
			!this.imageOutcomeUncertain &&
			!this.imageOperation &&
			!this.lockedByAnotherEditor &&
			operationAllowed &&
			publicationAllowed &&
			!this.form.submitting &&
			inputReady
		);
	}

	get lockedByAnotherEditor() {
		return this.documentLock?.lock !== null && this.documentLock?.owned === false;
	}

	get lockOwnerLabel() {
		return this.documentLock?.lock?.ownerLabel;
	}

	get lockUpdatedAt() {
		return this.documentLock?.lock?.updatedAt;
	}

	get canTakeOverLock() {
		return this.lockedByAnotherEditor && this.documentLock?.canTakeOver === true;
	}

	get headingField() {
		return documentHeadingField(this.collection, (path) => this.form.canRead(path));
	}

	get titleField() {
		if (this.globalResource) return undefined;
		return this.headingField?.type === "text" && this.headingField.admin.row === undefined
			? this.headingField
			: undefined;
	}

	get documentFields() {
		return (this.collection?.fields ?? []).filter(
			(field) =>
				this.form.canRead(field.path) &&
				!(this.uploadCollection && uploadMetadataFields.has(field.name))
		);
	}

	get canDelete() {
		return !this.lockedByAnotherEditor && this.form.access?.operations.delete === true;
	}

	get canDuplicate() {
		return !this.lockedByAnotherEditor && this.form.access?.operations.duplicate === true;
	}

	get canPublish() {
		return !this.lockedByAnotherEditor && this.form.access?.operations.publish === true;
	}

	get canUnpublish() {
		return !this.lockedByAnotherEditor && this.form.access?.operations.unpublish === true;
	}

	get canRestoreVersion() {
		if (this.lockedByAnotherEditor || this.form.access?.operations.update !== true) return false;
		const status = this.pendingRestore?.Status;
		return status === "draft"
			? this.form.access?.operations.unpublish === true
			: this.form.access?.operations.publish === true;
	}

	get canReadVersions() {
		return !this.creating && this.form.access?.operations.readVersions === true;
	}

	get canEditImage() {
		return (
			this.uploadCollection &&
			!this.creating &&
			!this.lockedByAnotherEditor &&
			!this.form.submitting &&
			!this.imageOutcomeUncertain &&
			this.assetMimeType?.startsWith("image/") === true &&
			this.form.access?.operations.update === true &&
			(!this.versionedCollection ||
				this.currentStatus !== "published" ||
				this.form.access?.operations.publish === true)
		);
	}

	get validationFields() {
		return (this.collection?.fields ?? []).filter(
			(field) => !(this.uploadCollection && uploadMetadataFields.has(field.name))
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
		if (this.uploadCollection) {
			const filename = this.currentDocument?.filename;
			if (typeof filename === "string" && filename.trim().length > 0) return filename;
			if (this.selectedFile !== undefined) return this.selectedFile.name;
			return this.creating
				? this.options.runtime.i18n.t("documents:newLabel", {
						label: this.collectionSingularLabel.toLocaleLowerCase(
							this.options.runtime.i18n.language
						),
					})
				: this.options.runtime.i18n.t("documents:untitledAsset");
		}
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

	get assetSize() {
		const value = this.#documentNumber("filesize");
		if (value === undefined) return undefined;
		if (value < 1_024)
			return this.options.runtime.i18n.formatNumber(value, { style: "unit", unit: "byte" });
		if (value < 1_048_576)
			return this.options.runtime.i18n.formatNumber(value / 1_024, {
				maximumFractionDigits: 0,
				style: "unit",
				unit: "kilobyte",
			});
		return this.options.runtime.i18n.formatNumber(value / 1_048_576, {
			maximumFractionDigits: value < 10_485_760 ? 1 : 0,
			style: "unit",
			unit: "megabyte",
		});
	}

	get assetDimensions() {
		const width = this.#documentNumber("width");
		const height = this.#documentNumber("height");
		return width !== undefined && height !== undefined
			? `${this.options.runtime.i18n.formatNumber(width)} × ${this.options.runtime.i18n.formatNumber(height)}`
			: undefined;
	}

	revealFirstIssue = () => {
		const issue = this.form.issues[0];
		if (issue === undefined) return undefined;
		this.documentView = "edit";
		return issue.path;
	};

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
		this.selectedFiles = undefined;
	};

	refresh = async () => {
		const documentID = this.documentID;
		if (documentID === undefined || this.collection === undefined) return;
		const request = new AbortController();
		await this.#load(this.collection.slug, documentID, request.signal);
	};

	takeOverLock = async () => {
		const documentID = this.documentID;
		if (
			documentID === undefined ||
			this.collection?.capabilities.locking !== true ||
			!this.canTakeOverLock
		)
			return false;
		this.lockOperation = true;
		try {
			const state = await this.options.runtime.client.acquireDocumentLock(
				this.collectionSlug,
				documentID,
				true
			);
			this.#applyLock(state);
			this.options.notifications.success({
				title: this.options.runtime.i18n.t("documents:lockTakenOver"),
			});
			return state.owned;
		} catch (cause) {
			this.options.notifications.error({
				title: this.options.runtime.i18n.t("documents:lockTakeoverFailed"),
				message: cause instanceof Error ? cause.message : undefined,
			});
			return false;
		} finally {
			this.lockOperation = false;
		}
	};

	updateUploadImage = async (input: {
		focalX: number;
		focalY: number;
		cropX: number;
		cropY: number;
		cropWidth: number;
		cropHeight: number;
	}) => {
		const documentID = this.documentID;
		if (documentID === undefined || !this.canEditImage || this.imageOperation) return false;
		const routeKey = this.#activeRouteKey;
		const collectionSlug = this.collectionSlug;
		const request = new AbortController();
		this.#imageRequest?.abort();
		this.#imageRequest = request;
		this.imageOperation = true;
		try {
			const updated = await this.options.runtime.client.updateUploadImage(
				collectionSlug,
				documentID,
				input,
				{ revision: this.currentRevision, signal: request.signal }
			);
			if (request.signal.aborted || this.#activeRouteKey !== routeKey) return false;
			this.#applyImageDocument(updated);
			this.imageOutcomeUncertain = false;
			this.options.runtime.documentsChanged();
			this.options.notifications.success({
				title: this.options.runtime.i18n.t("uploads:imageSizesRegenerated"),
				message:
					input.cropWidth > 0
						? this.options.runtime.i18n.t("uploads:cropAndFocalUpdated", {
								cropWidth: this.options.runtime.i18n.formatNumber(Math.round(input.cropWidth)),
								cropHeight: this.options.runtime.i18n.formatNumber(Math.round(input.cropHeight)),
								focalX: this.options.runtime.i18n.formatNumber(Math.round(input.focalX)),
								focalY: this.options.runtime.i18n.formatNumber(Math.round(input.focalY)),
							})
						: this.options.runtime.i18n.t("uploads:focalUpdated", {
								focalX: this.options.runtime.i18n.formatNumber(Math.round(input.focalX)),
								focalY: this.options.runtime.i18n.formatNumber(Math.round(input.focalY)),
							}),
			});
			await Promise.allSettled([
				this.#refreshVersions(documentID),
				this.#refreshAccess(documentID),
			]);
			return true;
		} catch (cause) {
			if (request.signal.aborted || this.#activeRouteKey !== routeKey) return false;
			const confirmedFailure = cause instanceof RiduError && cause.status < 500;
			if (!confirmedFailure) {
				try {
					const refreshed = await this.options.runtime.client.find(collectionSlug, documentID, {
						signal: request.signal,
						locale: this.contentLocale,
					});
					if (request.signal.aborted || this.#activeRouteKey !== routeKey) return false;
					this.#applyImageDocument(refreshed);
					this.imageOutcomeUncertain = false;
					await Promise.allSettled([
						this.#refreshVersions(documentID),
						this.#refreshAccess(documentID),
					]);
					if (imageEditMatches(refreshed, input)) {
						this.options.notifications.success({
							title: this.options.runtime.i18n.t("uploads:imageSizesRegenerated"),
							message: this.options.runtime.i18n.t("uploads:imageStateRecovered"),
						});
						return true;
					}
					this.options.notifications.error({
						title: this.options.runtime.i18n.t("uploads:imageSizesNotRegenerated"),
						message: this.options.runtime.i18n.t("uploads:serverStateRefreshed"),
					});
					return false;
				} catch {
					if (request.signal.aborted || this.#activeRouteKey !== routeKey) return false;
					this.imageOutcomeUncertain = true;
					this.options.notifications.error({
						title: this.options.runtime.i18n.t("uploads:imageOutcomeUnknown"),
						message: this.options.runtime.i18n.t("uploads:imageOutcomeUnknownDescription"),
					});
					return false;
				}
			}
			this.options.notifications.error({
				title: this.options.runtime.i18n.t("uploads:imageSizesNotRegenerated"),
				message:
					cause instanceof Error
						? cause.message
						: this.options.runtime.i18n.t("uploads:focalUpdateFailed"),
			});
			return false;
		} finally {
			if (this.#imageRequest === request) {
				this.#imageRequest = undefined;
				this.imageOperation = false;
			}
		}
	};

	save = async ({
		silent = false,
		password,
		publish = false,
	}: { silent?: boolean; password?: string; publish?: boolean } = {}) => {
		if (!this.canSave) return false;
		const publishingChanges =
			publish && !this.creating && this.versionedCollection && this.currentStatus === "published";
		if (publishingChanges && !this.canPublish) return false;
		const creatingUpload = this.creating && this.uploadCollection;
		const routeKey = this.#activeRouteKey;
		const collectionSlug = this.collectionSlug;
		const documentID = this.documentID;
		const request = new AbortController();
		this.#saveRequest?.abort();
		this.#saveRequest = request;
		try {
			const wasCreating = this.creating;
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
					data: values,
					signal: request.signal,
					locale: this.contentLocale,
				});
			});
			if (request.signal.aborted || this.#activeRouteKey !== routeKey) return false;
			this.saveOutcomeUncertain = false;
			this.#applyDocument(saved);
			if (creatingUpload) this.selectedFiles = undefined;
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
				this.options.navigate(
					withContentLocale(documentPath(this.collectionSlug, saved.id), this.contentLocale),
					{ replace: true }
				);
			} else {
				await Promise.allSettled([this.#refreshVersions(saved.id), this.#refreshAccess(saved.id)]);
			}
			return true;
		} catch (cause) {
			if (request.signal.aborted || this.#activeRouteKey !== routeKey) return false;
			if (
				creatingUpload &&
				!(cause instanceof FormValidationError) &&
				(!(cause instanceof RiduError) || cause.status >= 500)
			) {
				this.saveOutcomeUncertain = true;
				if (!silent) {
					this.options.notifications.error({
						title: this.options.runtime.i18n.t("uploads:outcomeUnknown"),
						message: this.options.runtime.i18n.t("uploads:outcomeUnknownDescription"),
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
		if (this.documentID === undefined || this.lockedByAnotherEditor) return;
		this.versionOperation = true;
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
			this.#applyDocument(saved);
			this.options.runtime.documentsChanged();
			await Promise.allSettled([
				this.#refreshVersions(this.documentID),
				this.#refreshAccess(this.documentID),
			]);
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
			this.options.notifications.error({
				title: this.options.runtime.i18n.t("documents:statusNotChanged"),
				message:
					cause instanceof Error
						? cause.message
						: this.options.runtime.i18n.t("documents:statusChangeFailed"),
			});
		} finally {
			this.versionOperation = false;
		}
	};

	duplicate = async () => {
		if (this.documentID === undefined || this.globalResource || this.lockedByAnotherEditor) return;
		this.duplicateOperation = true;
		try {
			const duplicated = await this.options.runtime.client.duplicate(
				this.collectionSlug,
				this.documentID,
				{},
				{ locale: this.contentLocale }
			);
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
			this.options.notifications.error({
				title: this.options.runtime.i18n.t("documents:notDuplicated"),
				message:
					cause instanceof Error
						? cause.message
						: this.options.runtime.i18n.t("documents:duplicateFailed"),
			});
		} finally {
			this.duplicateOperation = false;
		}
	};

	restore = async (version: AdminVersion) => {
		if (this.documentID === undefined || this.lockedByAnotherEditor || !this.canRestoreVersion)
			return;
		this.versionOperation = true;
		try {
			const saved = this.globalResource
				? await this.options.runtime.client.restoreGlobal(this.collectionSlug, version.Revision, {
						revision: this.currentRevision,
						locale: this.contentLocale,
					})
				: await this.options.runtime.client.restore(
						this.collectionSlug,
						this.documentID,
						version.Revision,
						{ revision: this.currentRevision, locale: this.contentLocale }
					);
			this.#applyDocument(saved);
			this.options.runtime.documentsChanged();
			await Promise.allSettled([
				this.#refreshVersions(this.documentID),
				this.#refreshAccess(this.documentID),
			]);
			this.options.notifications.success({
				title: this.options.runtime.i18n.t("versions:revisionRestoredTitle"),
				message: this.options.runtime.i18n.t("versions:revisionIsCurrent", {
					revision: this.options.runtime.i18n.formatNumber(version.Revision),
				}),
			});
		} catch (cause) {
			this.options.notifications.error({
				title: this.options.runtime.i18n.t("versions:revisionNotRestored"),
				message:
					cause instanceof Error
						? cause.message
						: this.options.runtime.i18n.t("versions:revisionRestoreFailed"),
			});
		} finally {
			this.versionOperation = false;
			this.restoreDialogOpen = false;
			this.pendingRestore = undefined;
		}
	};

	remove = async () => {
		if (this.documentID === undefined || this.lockedByAnotherEditor) return;
		try {
			await this.options.runtime.client.delete(this.collectionSlug, this.documentID);
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
			this.options.notifications.error({
				title: this.options.runtime.i18n.t("documents:notDeleted"),
				message:
					cause instanceof Error
						? cause.message
						: this.options.runtime.i18n.t("documents:deleteFailed"),
			});
		}
	};

	requestRestore = (version: AdminVersion) => {
		this.pendingRestore = version;
		this.restoreDialogOpen = true;
	};

	versionDate = (version: AdminVersion) => this.#formatDate(version.CreatedAt);

	documentDate = (value: string | undefined) =>
		value === undefined ? "—" : this.#formatDate(value);

	copyAssetURL = async () => {
		if (this.assetURL === undefined) return;
		await navigator.clipboard.writeText(new URL(this.assetURL, window.location.origin).href);
		this.copiedAssetURL = true;
		this.options.notifications.success({
			title: this.options.runtime.i18n.t("uploads:urlCopied"),
			message: this.options.runtime.i18n.t("uploads:urlCopiedDescription"),
		});
		if (this.#copyResetTimer !== undefined) window.clearTimeout(this.#copyResetTimer);
		this.#copyResetTimer = window.setTimeout(() => {
			this.copiedAssetURL = false;
			this.#copyResetTimer = undefined;
		}, 1_500);
	};

	#enterRoute(slug: string, documentID: string | undefined, locale: string | undefined) {
		const resourceKey = `${this.options.global ? "global" : "collection"}:${slug}:${documentID ?? "new"}`;
		const routeKey = `${resourceKey}:${locale ?? "default"}`;
		if (
			this.#activeRouteKey.startsWith(`${resourceKey}:`) &&
			this.#activeRouteKey !== routeKey &&
			this.hasUnsavedChanges
		) {
			return;
		}
		this.#activeContentLocale = locale;
		if (this.#activeRouteKey !== "" && this.#activeRouteKey !== routeKey) {
			this.#saveRequest?.abort();
			this.#saveRequest = undefined;
			this.#imageRequest?.abort();
			this.#imageRequest = undefined;
			this.imageOperation = false;
		}
		const collection = (
			this.options.global
				? this.options.runtime.manifest?.globals
				: this.options.runtime.manifest?.collections
		)?.find((item) => item.slug === slug);
		if (collection === undefined) {
			this.#activeRouteKey = routeKey;
			this.#releaseLock();
			this.collection = undefined;
			this.collectionAvailable = false;
			this.currentDocument = undefined;
			this.selectedFiles = undefined;
			this.versions = [];
			this.form.reset({});
			this.form.setResource({ collection: slug, id: documentID, global: this.options.global });
			this.loading = false;
			this.error = this.options.runtime.i18n.t("documents:collectionUnavailable");
			this.form.setAccess(undefined, documentID === undefined ? "create" : "update");
			return;
		}
		this.collection = collection;
		this.form.setResource({
			collection: collection.slug,
			id: documentID,
			global: this.options.global,
		});
		if (
			this.#lockRoute !== undefined &&
			(this.#lockRoute.collection !== collection.slug || this.#lockRoute.documentID !== documentID)
		) {
			this.#releaseLock();
		}
		this.collectionAvailable = true;
		if (this.#activeRouteKey === routeKey) {
			if (!this.loading || documentID === undefined) return;
			const request = new AbortController();
			this.#load(collection.slug, documentID, request.signal);
			return () => request.abort();
		}

		this.#activeRouteKey = routeKey;
		this.saveOutcomeUncertain = false;
		this.imageOutcomeUncertain = false;
		this.form.setAccess(undefined, documentID === undefined ? "create" : "update");
		if (documentID === undefined) {
			this.form.reset(initialFormValues(collection.fields), collection.fields);
			this.form.setLocalization(locale);
			this.versions = [];
			this.currentDocument = undefined;
			this.currentRevision = 0;
			this.currentStatus = this.draftsCollection ? "draft" : "published";
			this.documentLock = undefined;
			this.form.writeBlocked = false;
			this.#restoreDraft();
			const request = new AbortController();
			this.#loadCreateAccess(collection.slug, request.signal);
			return () => request.abort();
		}
		const request = new AbortController();
		this.#load(collection.slug, documentID, request.signal);
		return () => request.abort();
	}

	#applySchemaUpdate() {
		const previous = this.collection;
		if (previous === undefined) return;
		const next = (
			this.globalResource
				? this.options.runtime.manifest?.globals
				: this.options.runtime.manifest?.collections
		)?.find((item) => item.id === previous.id);
		if (next === undefined) {
			this.collectionAvailable = false;
			this.options.notifications.error({
				title: this.options.runtime.i18n.t("documents:collectionRemoved"),
			});
			return;
		}

		const result = this.form.reconcile(previous.fields, next.fields, this.creating);
		this.collection = next;
		this.collectionAvailable = true;
		if (result.detached.length > 0) {
			this.options.notifications.warning({
				title: this.options.runtime.i18n.t("documents:schemaUpdatedWithDraft"),
				message: this.options.runtime.i18n.t("documents:incompatibleDraftValues", {
					count: result.detached.length,
				}),
			});
		}
		if (previous.slug !== next.slug) {
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
	}

	#restoreDraft() {
		const collection = this.collection;
		if (collection === undefined) return;
		const checkpoint = takeFormDraft(collection.id, this.documentID);
		if (checkpoint === undefined) return;
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
	}

	async #load(slug: string, documentID: string, signal: AbortSignal) {
		this.loading = true;
		this.error = undefined;
		try {
			const [document, access] = await Promise.all([
				this.globalResource
					? this.options.runtime.client.global(slug, {
							signal,
							locale: this.contentLocale,
						})
					: this.options.runtime.client.find(slug, documentID, {
							signal,
							locale: this.contentLocale,
						}),
				this.globalResource
					? this.options.runtime.client.globalAccess(slug, {
							signal,
							locale: this.contentLocale,
						})
					: this.options.runtime.client.collectionAccess(slug, {
							id: documentID,
							signal,
							locale: this.contentLocale,
						}),
			]);
			this.form.setAccess(access, "update");
			this.#applyDocument(document, true);
			if (this.versionedCollection && access.operations.readVersions) {
				this.versions = this.globalResource
					? await this.options.runtime.client.globalVersions(slug, {
							signal,
							locale: this.contentLocale,
						})
					: await this.options.runtime.client.versions(slug, documentID, {
							signal,
							locale: this.contentLocale,
						});
			} else {
				this.versions = [];
			}
			if (!this.globalResource && this.collection?.capabilities.locking === true) {
				await this.#acquireLock(slug, documentID, signal);
			} else {
				this.documentLock = undefined;
				this.form.writeBlocked = false;
			}
		} catch (cause) {
			if (signal.aborted) return;
			this.error =
				cause instanceof Error
					? cause.message
					: this.options.runtime.i18n.t("documents:loadFailed");
		} finally {
			if (!signal.aborted) this.loading = false;
		}
	}

	async #acquireLock(slug: string, documentID: string, signal?: AbortSignal) {
		const state = await this.options.runtime.client.acquireDocumentLock(slug, documentID, false, {
			signal,
		});
		if (signal?.aborted) return;
		this.#lockRoute = { collection: slug, documentID };
		this.#applyLock(state);
		if (state.owned) this.#startLockRefresh(slug, documentID);
	}

	#applyLock(state: DocumentLockEnvelope) {
		this.documentLock = state;
		this.form.writeBlocked = state.lock !== null && !state.owned;
		if (state.owned && this.#lockRoute !== undefined) {
			this.#startLockRefresh(this.#lockRoute.collection, this.#lockRoute.documentID);
		}
	}

	#startLockRefresh(collection: string, documentID: string) {
		if (this.#lockRefreshTimer !== undefined) window.clearInterval(this.#lockRefreshTimer);
		const interval = Math.max(
			5,
			Math.floor((this.collection?.documentLockSettings?.durationSeconds ?? 120) / 3)
		);
		this.#lockRefreshTimer = window.setInterval(async () => {
			try {
				const state = await this.options.runtime.client.acquireDocumentLock(collection, documentID);
				this.#applyLock(state);
			} catch {
				// A failed heartbeat is reflected by the next explicit request or save conflict.
			}
		}, interval * 1_000);
	}

	#releaseLock() {
		if (this.#lockRefreshTimer !== undefined) {
			window.clearInterval(this.#lockRefreshTimer);
			this.#lockRefreshTimer = undefined;
		}
		const route = this.#lockRoute;
		const owned = this.documentLock?.owned === true;
		this.#lockRoute = undefined;
		this.documentLock = undefined;
		this.form.writeBlocked = false;
		if (route !== undefined && owned) {
			this.options.runtime.client
				.releaseDocumentLock(route.collection, route.documentID, { keepalive: true })
				.catch(() => {});
		}
	}

	async #loadCreateAccess(slug: string, signal: AbortSignal) {
		this.loading = true;
		this.error = undefined;
		try {
			const access = await this.options.runtime.client.collectionAccess(slug, {
				data: $state.snapshot(this.form.values),
				signal,
				locale: this.contentLocale,
			});
			if (!signal.aborted) this.form.setAccess(access, "create");
		} catch (cause) {
			if (!signal.aborted) {
				this.error =
					cause instanceof Error
						? cause.message
						: this.options.runtime.i18n.t("documents:accessEvaluationFailed");
			}
		} finally {
			if (!signal.aborted) this.loading = false;
		}
	}

	async #refreshAccess(documentID: string) {
		const routeKey = this.#activeRouteKey;
		const collectionSlug = this.collectionSlug;
		const access = this.globalResource
			? await this.options.runtime.client.globalAccess(collectionSlug, {
					locale: this.contentLocale,
				})
			: await this.options.runtime.client.collectionAccess(collectionSlug, {
					id: documentID,
					locale: this.contentLocale,
				});
		if (this.#activeRouteKey === routeKey) this.form.setAccess(access, "update");
	}

	async #refreshVersions(documentID: string) {
		const routeKey = this.#activeRouteKey;
		const collectionSlug = this.collectionSlug;
		if (this.versionedCollection && this.canReadVersions) {
			const versions = this.globalResource
				? await this.options.runtime.client.globalVersions(collectionSlug, {
						locale: this.contentLocale,
					})
				: await this.options.runtime.client.versions(collectionSlug, documentID, {
						locale: this.contentLocale,
					});
			if (this.#activeRouteKey === routeKey) this.versions = versions;
		} else {
			if (this.#activeRouteKey === routeKey) this.versions = [];
		}
	}

	#applyDocument(document: AdminDocument, recoverDraft = false) {
		this.currentDocument = document;
		this.form.reset(
			documentFormValues(this.collection?.fields ?? [], document),
			this.collection?.fields ?? []
		);
		this.form.setLocalization(this.contentLocale, document._localization?.sources);
		this.currentRevision = typeof document._revision === "number" ? document._revision : 0;
		this.currentStatus = document._status === "draft" ? "draft" : "published";
		if (recoverDraft) this.#restoreDraft();
	}

	#applyImageDocument(document: AdminDocument) {
		this.currentDocument = document;
		this.form.setLocalization(this.contentLocale, document._localization?.sources);
		for (const name of uploadMetadataFields) {
			this.form.values[name] = document[name];
			this.form.original[name] = document[name];
		}
		this.currentRevision = typeof document._revision === "number" ? document._revision : 0;
		this.currentStatus = document._status === "draft" ? "draft" : "published";
	}

	#documentString(name: string) {
		const value = this.currentDocument?.[name];
		return typeof value === "string" && value.length > 0 ? value : undefined;
	}

	#documentNumber(name: string) {
		const value = this.currentDocument?.[name];
		return typeof value === "number" && Number.isFinite(value) ? value : undefined;
	}

	#formatDate(value: string) {
		const date = new Date(value);
		if (Number.isNaN(date.valueOf())) return value;
		return this.options.runtime.i18n.formatDate(date, {
			day: "2-digit",
			month: "short",
			year: "numeric",
		});
	}

	#fieldForIssue(path: string) {
		return this.documentFields.find(
			(field) => path === field.path || path.startsWith(`${field.name}.`)
		);
	}
}

function imageEditMatches(
	document: AdminDocument,
	input: {
		focalX: number;
		focalY: number;
		cropX: number;
		cropY: number;
		cropWidth: number;
		cropHeight: number;
	}
) {
	return (
		document.focalX === input.focalX &&
		document.focalY === input.focalY &&
		document.cropX === input.cropX &&
		document.cropY === input.cropY &&
		document.cropWidth === input.cropWidth &&
		document.cropHeight === input.cropHeight
	);
}

function withContentLocale(path: string, locale: string | undefined) {
	if (locale === undefined) return path;
	const search = new URLSearchParams({ locale });
	return `${path}?${search.toString()}`;
}
