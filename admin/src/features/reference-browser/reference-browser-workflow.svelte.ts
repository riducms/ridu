import { documentLabel, documentTitleField } from "@admin/features/documents/document-title";
import { resolveBlockTypes } from "@riducms/protocol";
import type { FieldReferenceBrowserProps } from "@riducms/plugin";
import { connectDocumentLiveValidation } from "@admin/core/forms/live-validation.svelte";
import type { SchemaCollection, SchemaField } from "@riducms/protocol";
import { RiduError } from "@riducms/sdk";

import type { AdminDocument } from "@admin/core/api/admin-client";
import { FormController, FormValidationError } from "@admin/core/forms/form-controller.svelte";
import { documentFormValues, initialFormValues } from "@admin/core/forms/form-schema";
import { invalidFieldLabels } from "@admin/core/forms/form-validation";
import type { NotificationCenter } from "@admin/core/notifications/notification-center.svelte";
import type { AdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
import { UploadDraft } from "@admin/features/uploads/upload-draft.svelte";
import {
	defaultListColumns,
	listColumnFields,
	listMetadataFields,
	filterableFields,
	type ListColumnSelection,
	type ListFilterGroup,
	type ListPageSize,
} from "@admin/features/collections/list-workspace";
import { referenceListWhere } from "@admin/features/reference-browser/reference-list-query";
import { isUploadMetadataField } from "@admin/features/uploads/upload-document-contracts";

import { RelationshipLookupController } from "@admin/fields/relationship/relationship-lookup-controller.svelte";

interface ReferenceBrowserWorkflowOptions {
	runtime: AdminRuntime;
	notifications: NotificationCenter;
	field: SchemaField;
	collection: SchemaCollection;
	collections?: readonly SchemaCollection[];
	hasMany: boolean;
	selectedIDs: readonly string[];
	readOnly: boolean;
	initialDocument?: AdminDocument;
	initialDocumentID?: string;
	initialCreate?: boolean;
	initialFile?: File;
	optionFilter?: FieldReferenceBrowserProps["optionFilter"];
	defaultValues?: Readonly<Record<string, unknown>>;
	allowCreate?: boolean;
	locale?: string;
	onCommit: (ids: string[], collectionSlug: string) => void | boolean | Promise<void | boolean>;
	onClose: () => void;
	get open(): boolean;
	setOpen(open: boolean): void;
}

export class ReferenceBrowserWorkflow {
	readonly form: FormController;
	readonly upload: UploadDraft;
	readonly availableCollections: readonly SchemaCollection[];
	pageSize = $state<ListPageSize>(10);
	columns = $state.raw<ListColumnSelection[]>([]);
	listFilters = $state.raw<ListFilterGroup[]>([]);
	sort = $state("");
	#collection: SchemaCollection;
	readonly scopedFields = $derived(
		this.collection.fields.map((field) => scopeField(field, `${this.options.field.id}-drawer`))
	);
	readonly displayField = $derived(
		this.scopedFields.find((field) => field.path === documentTitleField(this.collection)?.path)
	);
	readonly searchField = $derived.by(() => {
		const field = this.collection.capabilities.upload
			? this.collection.fields.find((field) => field.name === "filename")
			: documentTitleField(this.collection);
		return field?.queryRestricted ? undefined : field;
	});
	readonly editorFields = $derived(
		this.scopedFields.filter(
			(field) => !(this.collection.capabilities.upload && isUploadMetadataField(field.name))
		)
	);
	readonly validationFields = $derived(
		this.collection.fields.filter(
			(field) => !(this.collection.capabilities.upload && isUploadMetadataField(field.name))
		)
	);
	screen = $state<"list" | "document">("list");
	workingSelection = $state.raw<string[]>([]);
	query = $state("");
	page = $state(1);
	editorDocument = $state.raw<AdminDocument>();
	editorLoading = $state(false);
	editorError = $state<string>();
	editorView = $state<"edit" | "api">("edit");
	newUserPassword = $state("");
	newUserPasswordConfirmation = $state("");
	credentialIssue = $state<string>();
	confirmDiscard = $state(false);
	committing = $state(false);
	#pendingDismiss: "back" | "close" | "navigate" = "back";
	#pendingNavigation?: () => void;
	#directEntry = $state(false);
	readonly #lookup: RelationshipLookupController;
	#editorLoad?: AbortController;
	#editorAccess?: AbortController;
	#saveRequest?: AbortController;
	#disposed = false;

	constructor(readonly options: ReferenceBrowserWorkflowOptions) {
		this.#directEntry =
			options.initialCreate === true ||
			options.initialFile !== undefined ||
			options.initialDocument !== undefined ||
			options.initialDocumentID !== undefined;
		this.availableCollections =
			options.collections?.filter(
				(collection) => options.runtime.collectionOperations[collection.slug]?.read === true
			) ?? [];
		this.#collection = $state.raw(
			this.#directEntry
				? options.collection
				: (this.availableCollections.find(
						(collection) => collection.slug === options.collection.slug
					) ??
						this.availableCollections[0] ??
						options.collection)
		);
		this.form = new FormController({}, options.runtime.i18n);
		connectDocumentLiveValidation(this.form, options.runtime.client);
		this.#lookup = new RelationshipLookupController(options.runtime.client, options.runtime.i18n);
		this.upload = new UploadDraft(
			options.runtime.client,
			() => this.collection.slug,
			options.runtime.i18n
		);
		this.form.setLocalization(options.locale);
		this.columns = this.#defaultColumns();
		this.workingSelection = [...this.selectedIDs];
		if (options.initialDocument !== undefined) {
			this.#lookup.remember(options.initialDocument);
			this.#openEditor(options.initialDocument);
		} else if (options.initialDocumentID !== undefined) {
			this.screen = "document";
			this.#loadEditor(options.initialDocumentID);
		} else if (options.initialCreate || options.initialFile) {
			this.openNewDocument();
			if (this.canCreateDocument && options.initialFile && options.collection.uploadSettings) {
				this.upload.select(options.initialFile, options.collection.uploadSettings);
			}
		}

		$effect(() => {
			if (!this.options.open || this.screen !== "list" || this.browsingUnavailable) return;
			this.options.runtime.documentRevision;
			const request = {
				slug: this.collection.slug,
				page: this.page,
				limit: this.pageSize,
				search: this.query,
				searchField: this.searchField?.path,
				sort: this.sort,
				where: referenceListWhere(this.listFilters, [
					...filterableFields(this.collection.fields),
					...listMetadataFields(this.collection, this.options.runtime.i18n),
				]),
				includeAccess: true,
				depth: 1,
				filter:
					typeof this.options.optionFilter === "function"
						? this.options.optionFilter(this.collection)
						: this.options.optionFilter,
				locale: this.options.locale,
			};
			const delay = request.search.trim() === "" ? 0 : 220;
			const timer = window.setTimeout(() => this.#lookup.search(request), delay);
			return () => {
				window.clearTimeout(timer);
				this.#lookup.cancelSearch();
			};
		});

		$effect(() => () => {
			this.#disposed = true;
			this.form.disposeBindings();
			this.#cancelEditorLoad();
			this.#editorAccess?.abort();
			this.#saveRequest?.abort();
			this.#lookup.dispose();
			this.upload.dispose();
		});
	}

	get collection() {
		return this.#collection;
	}

	get selectedIDs() {
		return this.collection.slug === this.options.collection.slug ? this.options.selectedIDs : [];
	}

	get browsingUnavailable() {
		return (
			!this.directEntry &&
			this.options.collections !== undefined &&
			this.availableCollections.length === 0
		);
	}

	selectCollection = (slug: string) => {
		if (this.screen !== "list" || this.committing || this.form.submitting) return;
		const collection = this.availableCollections.find((collection) => collection.slug === slug);
		if (collection === undefined || collection.slug === this.collection.slug) return;

		this.#returnToList();
		this.#lookup.reset();
		this.#collection = collection;
		this.query = "";
		this.page = 1;
		this.sort = "";
		this.listFilters = [];
		this.columns = this.#defaultColumns();
		this.workingSelection = [...this.selectedIDs];
	};

	#defaultColumns() {
		const columns = [
			...listColumnFields(this.collection),
			...listMetadataFields(this.collection, this.options.runtime.i18n),
		];
		const defaults = new Set([
			...(this.collection.capabilities.upload ? ["filename"] : []),
			...defaultListColumns(
				this.collection,
				columns.map((field) => ({
					path: field.path,
					label: field.admin.label,
					field,
				}))
			),
		]);
		return [...new Set([...defaults, ...columns.map((field) => field.path)])].map((path) => ({
			path,
			active: defaults.has(path),
		}));
	}

	get status() {
		return this.#lookup.status;
	}

	get docs() {
		return this.#lookup.docs;
	}

	get pagination() {
		return this.#lookup.pagination;
	}

	get error() {
		return this.#lookup.error;
	}

	get selectedFile() {
		return this.upload.file;
	}

	get creating() {
		return this.screen === "document" && this.editorDocument === undefined;
	}

	get creatingAuthUser() {
		return this.creating && this.collection.capabilities.auth;
	}

	get editorDirty() {
		return (
			this.form.dirty ||
			this.upload.dirty ||
			this.newUserPassword !== "" ||
			this.newUserPasswordConfirmation !== ""
		);
	}

	get editorTitle() {
		if (this.collection.capabilities.upload)
			return this.upload.filename || this.options.runtime.i18n.t("documents:untitled");
		const value =
			this.displayField === undefined ? undefined : this.form.get(this.displayField.path);
		const title = typeof value === "string" ? value.trim() : "";
		return title === ""
			? this.options.runtime.i18n.t("reference:untitledLabel", {
					label: this.options.runtime.i18n
						.text(this.collection.labels.singular, this.collection.labels.singularTranslations)
						.toLocaleLowerCase(this.options.runtime.i18n.language),
				})
			: title;
	}

	get editableUpload() {
		return (
			!this.options.readOnly &&
			!this.form.submitting &&
			this.form.access?.operations[this.creating ? "create" : "update"] === true
		);
	}

	get directEntry() {
		return this.#directEntry;
	}

	canReadField = (path: string, id?: string) => this.#lookup.canReadField(path, id);

	setColumns = (columns: ListColumnSelection[]) => {
		this.columns = columns;
	};

	setListFilters = (filters: ListFilterGroup[]) => {
		this.listFilters = filters;
		this.page = 1;
	};

	setSort = (path: string, descending: boolean) => {
		this.sort = descending ? `-${path}` : path;
		this.page = 1;
	};

	setPageSize = (size: ListPageSize) => {
		this.pageSize = size;
		this.page = 1;
	};

	selectDocument = (id: string) => this.commitSelection([id]);

	get canSave() {
		const operationAllowed = this.creating
			? this.form.access?.operations.create === true
			: this.form.access?.operations.update === true;
		const inputReady = this.creating
			? !this.collection.capabilities.upload || this.selectedFile !== undefined
			: this.editorDirty;
		return (
			!this.options.readOnly &&
			operationAllowed &&
			!this.editorLoading &&
			!this.form.submitting &&
			!this.upload.busy &&
			!this.upload.editingImage &&
			inputReady &&
			(!this.collection.capabilities.upload || this.upload.present)
		);
	}

	get canCreateDocument() {
		return (
			!this.options.readOnly &&
			!this.browsingUnavailable &&
			this.options.allowCreate !== false &&
			this.options.runtime.collectionOperations[this.collection.slug]?.create === true
		);
	}

	get canCommitSelection() {
		return (
			!this.options.readOnly &&
			!this.browsingUnavailable &&
			(this.workingSelection.length !== this.selectedIDs.length ||
				this.workingSelection.some((id) => !this.selectedIDs.includes(id)))
		);
	}

	setPage = (page: number) => {
		this.page = page;
	};

	documentLabel = (document: AdminDocument) =>
		this.collection.capabilities.upload
			? String(document.filename ?? document.id)
			: documentLabel(this.collection, document);

	toggleSelection = (id: string) => {
		if (this.options.readOnly || this.committing) return;
		this.workingSelection = this.options.hasMany
			? this.workingSelection.includes(id)
				? this.workingSelection.filter((candidate) => candidate !== id)
				: [...this.workingSelection, id]
			: [id];
	};

	commitSelection = async (selection: readonly string[] = this.workingSelection) => {
		if (this.#disposed || this.committing || this.options.readOnly || this.browsingUnavailable)
			return false;
		this.committing = true;
		try {
			const committed = await this.options.onCommit([...selection], this.collection.slug);
			if (this.#disposed || committed === false || this.options.readOnly) return false;
			this.#closeBrowser();
			return true;
		} catch (cause) {
			if (this.#disposed) return false;
			this.options.notifications.error({
				title: this.options.runtime.i18n.t("reference:updateNotConfirmed"),
				message:
					cause instanceof Error
						? cause.message
						: this.options.runtime.i18n.t("reference:selectedNotSaved"),
			});
			return false;
		} finally {
			if (!this.#disposed) this.committing = false;
		}
	};

	handleSearch = (event: Event) => {
		if (this.committing) return;
		this.query = (event.currentTarget as HTMLInputElement).value;
		this.page = 1;
	};

	openNewDocument = () => {
		if (this.form.submitting || !this.canCreateDocument) return;
		this.#cancelEditorLoad();
		this.editorDocument = undefined;
		this.form.setResource({ collection: this.collection.slug });
		const values = {
			...initialFormValues(this.collection.fields),
			...this.options.defaultValues,
		};
		this.form.reset(values, this.collection.fields);
		this.form.setAccess(undefined, "create");
		this.upload.reset();
		this.newUserPassword = "";
		this.newUserPasswordConfirmation = "";
		this.credentialIssue = undefined;
		this.editorError = undefined;
		this.editorView = "edit";
		this.screen = "document";
		this.#loadEditorAccess(undefined, values);
	};

	openKnownDocument = (document: AdminDocument) => {
		// List rows use populated relationships for display. Fetch editable wire values.
		this.screen = "document";
		this.#loadEditor(document.id);
	};

	saveEditor = async () => {
		if (this.form.submitting || !this.canSave) return false;
		const request = new AbortController();
		this.#saveRequest?.abort();
		this.#saveRequest = request;
		try {
			this.credentialIssue = this.#validateNewUserPassword();
			if (this.credentialIssue !== undefined) return false;
			const saved = await this.form.submit(this.validationFields, this.creating, async (values) => {
				if (this.options.readOnly) throw new Error("This field is read-only.");
				if (this.editorDocument !== undefined) {
					if (this.collection.capabilities.upload) {
						return this.options.runtime.client.updateUpload(
							this.collection.slug,
							this.editorDocument.id,
							{
								data: values,
								file: this.upload.file,
								filename: this.upload.filename,
								image: this.upload.image,
							},
							{
								revision:
									typeof this.editorDocument._revision === "number"
										? this.editorDocument._revision
										: undefined,
								signal: request.signal,
								locale: this.options.locale,
							}
						);
					}
					return this.options.runtime.client.update(
						this.collection.slug,
						this.editorDocument.id,
						values,
						{
							revision:
								typeof this.editorDocument._revision === "number"
									? this.editorDocument._revision
									: undefined,
							signal: request.signal,
							locale: this.options.locale,
						}
					);
				}
				if (this.collection.capabilities.auth) {
					return this.options.runtime.client.createAuthUser(
						this.collection.slug,
						values,
						this.newUserPassword,
						{ signal: request.signal, locale: this.options.locale }
					);
				}
				if (!this.collection.capabilities.upload) {
					return this.options.runtime.client.create(this.collection.slug, values, {
						signal: request.signal,
						locale: this.options.locale,
					});
				}
				if (this.selectedFile === undefined) {
					throw new Error(this.options.runtime.i18n.t("uploads:chooseFileBeforeCreating"));
				}
				return this.options.runtime.client.upload(this.collection.slug, this.selectedFile, {
					filename: this.upload.filename,
					image: this.upload.image,
					data: values,
					signal: request.signal,
					locale: this.options.locale,
				});
			});
			if (request.signal.aborted || this.options.readOnly) return false;
			const wasCreating = this.editorDocument === undefined;
			this.editorDocument = saved;
			this.upload.reset(saved);
			this.form.setResource({ collection: this.collection.slug, id: saved.id });
			this.#lookup.remember(saved);
			this.workingSelection = this.options.hasMany
				? [...new Set([...this.workingSelection, saved.id])]
				: [saved.id];
			this.options.runtime.documentsChanged();
			this.form.reset(documentFormValues(this.collection.fields, saved), this.collection.fields);
			this.form.setLocalization(this.options.locale, saved._localization?.sources);
			this.options.notifications.success({
				title: wasCreating
					? this.options.runtime.i18n.t("reference:createdLabel", {
							label: this.options.runtime.i18n.text(
								this.collection.labels.singular,
								this.collection.labels.singularTranslations
							),
						})
					: this.options.runtime.i18n.t("documents:updated"),
			});
			if (wasCreating && !this.directEntry) this.screen = "list";
			this.newUserPassword = "";
			this.newUserPasswordConfirmation = "";
			this.credentialIssue = undefined;
			if (!wasCreating) this.#loadEditorAccess(saved.id);
			if (wasCreating && this.directEntry && !(await this.commitSelection())) this.#returnToList();
			return true;
		} catch (cause) {
			if (request.signal.aborted || this.options.readOnly) return false;
			if (
				this.collection.capabilities.upload &&
				!(cause instanceof FormValidationError) &&
				(!(cause instanceof RiduError) || cause.status >= 500)
			) {
				this.options.notifications.error({
					title: this.options.runtime.i18n.t("uploads:outcomeUnknown"),
					message: this.options.runtime.i18n.t("uploads:outcomeUnknownListRefreshed"),
				});
				this.#returnToList();
				this.options.runtime.documentsChanged();
				return false;
			}
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
					title: this.options.runtime.i18n.t("reference:relatedNotSaved"),
					message:
						cause instanceof Error
							? cause.message
							: this.options.runtime.i18n.t("reference:relatedSaveFailed"),
				});
			}
			return false;
		} finally {
			if (this.#saveRequest === request) this.#saveRequest = undefined;
		}
	};

	#validateNewUserPassword() {
		if (!this.creatingAuthUser || this.collection.authSettings === undefined) {
			return undefined;
		}
		if (this.newUserPassword === "")
			return this.options.runtime.i18n.t("reference:enterNewAccountPassword");
		if (Array.from(this.newUserPassword).length < this.collection.authSettings.passwordMinLength) {
			return this.options.runtime.i18n.t("reference:passwordTooShort", {
				minimum: this.options.runtime.i18n.formatNumber(
					this.collection.authSettings.passwordMinLength
				),
			});
		}
		if (
			new TextEncoder().encode(this.newUserPassword).length >
			this.collection.authSettings.passwordMaxBytes
		) {
			return this.options.runtime.i18n.t("reference:passwordTooLong", {
				maximum: this.options.runtime.i18n.formatNumber(
					this.collection.authSettings.passwordMaxBytes
				),
			});
		}
		if (this.newUserPassword !== this.newUserPasswordConfirmation) {
			return this.options.runtime.i18n.t("reference:passwordsDoNotMatch");
		}
		return undefined;
	}

	requestNavigation = (navigate: () => void) => {
		if (this.committing || this.form.submitting) return;
		if (this.editorDirty) {
			this.#pendingDismiss = "navigate";
			this.#pendingNavigation = navigate;
			this.confirmDiscard = true;
			return;
		}
		navigate();
	};

	requestBack = () => {
		if (this.form.submitting) return;
		if (this.editorDirty) {
			this.#pendingDismiss = "back";
			this.confirmDiscard = true;
			return;
		}
		this.#returnToList();
	};

	requestOpenChange = (open: boolean) => {
		if (open) {
			this.options.setOpen(true);
			return;
		}
		if (this.committing || this.form.submitting) return;
		if (this.screen === "document" && this.editorDirty) {
			this.#pendingDismiss = "close";
			this.confirmDiscard = true;
			return;
		}
		this.#closeBrowser();
	};

	discardChanges = () => {
		if (this.form.submitting) return;
		this.confirmDiscard = false;
		if (this.#pendingDismiss === "navigate") {
			const navigate = this.#pendingNavigation;
			this.#pendingNavigation = undefined;
			navigate?.();
			return;
		}
		if (this.#pendingDismiss === "close") {
			this.#closeBrowser();
			return;
		}
		this.#returnToList();
	};

	mediaURL = (document: AdminDocument | undefined) =>
		typeof document?.url === "string" ? document.url : undefined;

	isImage = (document: AdminDocument | undefined) =>
		typeof document?.mimeType === "string" && document.mimeType.startsWith("image/");

	#cancelEditorLoad() {
		this.#editorLoad?.abort();
		this.#editorLoad = undefined;
	}

	#loadEditor(id: string) {
		this.#cancelEditorLoad();
		const request = new AbortController();
		this.#editorLoad = request;
		this.editorLoading = true;
		this.editorError = undefined;
		this.options.runtime.client
			.find(this.collection.slug, id, {
				depth: 0,
				locale: this.options.locale,
				signal: request.signal,
			})
			.then((document) => {
				if (request.signal.aborted) return;
				this.#editorLoad = undefined;
				this.#openEditor(document);
			})
			.catch((cause: unknown) => {
				if (request.signal.aborted) return;
				this.#editorLoad = undefined;
				this.editorLoading = false;
				this.editorError =
					cause instanceof Error
						? cause.message
						: this.options.runtime.i18n.t("reference:relatedLoadFailed");
			});
	}

	#openEditor(document: AdminDocument) {
		this.#cancelEditorLoad();
		this.form.setResource({ collection: this.collection.slug, id: document.id });
		this.editorDocument = document;
		this.form.reset(documentFormValues(this.collection.fields, document), this.collection.fields);
		this.form.setLocalization(this.options.locale, document._localization?.sources);
		this.form.setAccess(undefined, "update");
		this.upload.reset(document);
		this.newUserPassword = "";
		this.newUserPasswordConfirmation = "";
		this.credentialIssue = undefined;
		this.editorError = undefined;
		this.editorView = "edit";
		this.screen = "document";
		this.#loadEditorAccess(document.id);
	}

	#loadEditorAccess(id: string | undefined, data?: Record<string, unknown>) {
		this.#editorAccess?.abort();
		this.form.setAccess(undefined, id === undefined ? "create" : "update");
		const request = new AbortController();
		this.#editorAccess = request;
		this.editorLoading = true;
		this.editorError = undefined;
		this.options.runtime.client
			.collectionAccess(this.collection.slug, {
				...(id === undefined ? { data } : { id }),
				signal: request.signal,
				locale: this.options.locale,
			})
			.then((access) => {
				if (!request.signal.aborted) {
					this.form.setAccess(access, id === undefined ? "create" : "update");
				}
			})
			.catch((cause: unknown) => {
				if (!request.signal.aborted) {
					this.editorError =
						cause instanceof Error
							? cause.message
							: this.options.runtime.i18n.t("reference:accessCheckFailed");
				}
			})
			.finally(() => {
				if (!request.signal.aborted) {
					this.editorLoading = false;
					this.#editorAccess = undefined;
				}
			});
	}

	#returnToList() {
		this.#cancelEditorLoad();
		this.#editorAccess?.abort();
		this.#editorAccess = undefined;
		this.screen = "list";
		this.#directEntry = false;
		this.editorDocument = undefined;
		this.editorLoading = false;
		this.editorError = undefined;
		this.upload.reset();
		this.newUserPassword = "";
		this.newUserPasswordConfirmation = "";
		this.credentialIssue = undefined;
		this.form.reset({});
		this.form.setAccess(undefined, "update");
	}

	#closeBrowser() {
		this.#cancelEditorLoad();
		this.form.disposeBindings();
		this.options.setOpen(false);
		this.options.onClose();
	}
}

function scopeField(field: SchemaField, prefix: string): SchemaField {
	return {
		...field,
		id: `${prefix}-${field.id}`,
		admin: {
			...field.admin,
			...(field.admin.row === undefined ? {} : { row: { id: `${prefix}-${field.admin.row.id}` } }),
		},
		...(field.nested === undefined
			? {}
			: {
					nested: {
						...field.nested,
						fields: field.nested.fields.map((child) => scopeField(child, prefix)),
					},
				}),
		...(field.blocks === undefined
			? {}
			: {
					blocks: {
						types: resolveBlockTypes(field.blocks).map((block) => ({
							...block,
							fields: block.fields.map((child) => scopeField(child, `${prefix}-${block.slug}`)),
						})),
					},
				}),
	};
}
