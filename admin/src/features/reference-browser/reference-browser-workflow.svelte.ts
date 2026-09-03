import type { SchemaCollection, SchemaField } from "@riducms/protocol";
import { RiduError } from "@riducms/sdk";

import type { AdminDocument } from "@admin/core/api/admin-client";
import { FormController, FormValidationError } from "@admin/core/forms/form-controller.svelte";
import { documentFormValues, initialFormValues } from "@admin/core/forms/form-schema";
import { invalidFieldLabels } from "@admin/core/forms/form-validation";
import type { NotificationCenter } from "@admin/core/notifications/notification-center.svelte";
import type { AdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";

import { RelationshipLookupController } from "@admin/fields/relationship/relationship-lookup-controller.svelte";
import {
	combineRelationshipFilters,
	type RelationshipFilter,
} from "@admin/fields/relationship/relationship-query";

interface FilterOption {
	key: string;
	label: string;
	filter: RelationshipFilter;
}

interface ReferenceBrowserWorkflowOptions {
	runtime: AdminRuntime;
	notifications: NotificationCenter;
	field: SchemaField;
	collection: SchemaCollection;
	hasMany: boolean;
	selectedIDs: readonly string[];
	readOnly: boolean;
	initialDocument?: AdminDocument;
	initialDocumentID?: string;
	optionFilter?: RelationshipFilter;
	defaultValues?: Readonly<Record<string, unknown>>;
	allowCreate?: boolean;
	locale?: string;
	onCommit: (ids: string[]) => void | boolean | Promise<void | boolean>;
	onClose: () => void;
	get open(): boolean;
	setOpen(open: boolean): void;
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

export class ReferenceBrowserWorkflow {
	readonly form: FormController;
	readonly pageSize: number;
	readonly scopedFields: SchemaField[];
	readonly displayField: SchemaField | undefined;
	readonly headingField: SchemaField | undefined;
	readonly searchField: SchemaField | undefined;
	readonly editorFields: SchemaField[];
	readonly validationFields: SchemaField[];
	readonly filters: FilterOption[];
	screen = $state<"list" | "document">("list");
	workingSelection = $state.raw<string[]>([]);
	query = $state("");
	page = $state(1);
	filterKey = $state("");
	editorDocument = $state.raw<AdminDocument>();
	editorLoading = $state(false);
	editorError = $state<string>();
	editorView = $state<"edit" | "api">("edit");
	selectedFiles = $state<FileList>();
	newUserPassword = $state("");
	newUserPasswordConfirmation = $state("");
	credentialIssue = $state<string>();
	confirmDiscard = $state(false);
	committing = $state(false);
	#pendingDismiss: "back" | "close" = "back";
	readonly #lookup: RelationshipLookupController;
	readonly #editorLoad = new AbortController();
	#editorAccess?: AbortController;
	#saveRequest?: AbortController;

	constructor(readonly options: ReferenceBrowserWorkflowOptions) {
		this.form = new FormController({}, options.runtime.i18n);
		this.#lookup = new RelationshipLookupController(options.runtime.client);
		this.form.setLocalization(options.locale);
		this.pageSize = options.collection.capabilities.upload ? 8 : 10;
		this.scopedFields = options.collection.fields.map((field) =>
			scopeField(field, `${options.field.id}-drawer`)
		);
		this.displayField = findDisplayField(this.scopedFields);
		this.headingField = this.displayField?.admin.row === undefined ? this.displayField : undefined;
		this.searchField = findDisplayField(options.collection.fields);
		this.editorFields = this.scopedFields.filter(
			(field) => field.id !== this.headingField?.id && !uploadMetadataFields.has(field.name)
		);
		this.validationFields = options.collection.fields.filter(
			(field) => !(options.collection.capabilities.upload && uploadMetadataFields.has(field.name))
		);
		this.filters = createCollectionFilters(options.collection, options.runtime.i18n);
		this.workingSelection = [...options.selectedIDs];

		if (options.initialDocument !== undefined) {
			this.#lookup.remember(options.initialDocument);
			this.#openEditor(options.initialDocument);
		} else if (options.initialDocumentID !== undefined) {
			this.screen = "document";
			this.#loadEditor(options.initialDocumentID, this.#editorLoad.signal);
		}

		$effect(() => {
			if (!this.options.open || this.screen !== "list") return;
			this.options.runtime.documentRevision;
			const request = {
				slug: this.options.collection.slug,
				page: this.page,
				limit: this.pageSize,
				search: this.query,
				searchField: this.searchField?.name,
				filter: this.currentFilter,
				locale: this.options.locale,
			};
			const delay = request.search.trim() === "" ? 0 : 220;
			const timer = window.setTimeout(() => this.#lookup.search(request), delay);
			return () => window.clearTimeout(timer);
		});

		$effect(() => () => {
			this.#editorLoad.abort();
			this.#editorAccess?.abort();
			this.#saveRequest?.abort();
			this.#lookup.dispose();
		});
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
		return this.selectedFiles?.item(0) ?? undefined;
	}

	get creating() {
		return this.screen === "document" && this.editorDocument === undefined;
	}

	get creatingAuthUser() {
		return this.creating && this.options.collection.capabilities.auth;
	}

	get editorDirty() {
		return (
			this.form.dirty ||
			this.selectedFile !== undefined ||
			this.newUserPassword !== "" ||
			this.newUserPasswordConfirmation !== ""
		);
	}

	get currentFilter() {
		return combineRelationshipFilters(
			this.options.optionFilter,
			this.filters.find((option) => option.key === this.filterKey)?.filter
		);
	}

	get editorTitle() {
		const value =
			this.displayField === undefined ? undefined : this.form.get(this.displayField.path);
		const title = typeof value === "string" ? value.trim() : "";
		return title === ""
			? this.options.runtime.i18n.t("reference:untitledLabel", {
					label: this.options.runtime.i18n
						.text(
							this.options.collection.labels.singular,
							this.options.collection.labels.singularTranslations
						)
						.toLocaleLowerCase(this.options.runtime.i18n.language),
				})
			: title;
	}

	get headingIssues() {
		return this.headingField === undefined ? [] : this.form.issuesFor(this.headingField.path);
	}

	get canSave() {
		const operationAllowed = this.creating
			? this.form.access?.operations.create === true
			: this.form.access?.operations.update === true;
		const inputReady = this.creating
			? !this.options.collection.capabilities.upload || this.selectedFile !== undefined
			: this.form.dirty;
		return !this.options.readOnly && operationAllowed && !this.form.submitting && inputReady;
	}

	get canCreateDocument() {
		return (
			!this.options.readOnly &&
			this.options.allowCreate !== false &&
			this.options.runtime.collectionOperations[this.options.collection.slug]?.create === true
		);
	}

	get canCommitSelection() {
		return (
			this.workingSelection.length !== this.options.selectedIDs.length ||
			this.workingSelection.some((id) => !this.options.selectedIDs.includes(id))
		);
	}

	get rangeStart() {
		return this.pagination.totalDocs === 0
			? 0
			: (this.pagination.page - 1) * this.pagination.limit + 1;
	}

	get rangeEnd() {
		return Math.min(this.pagination.page * this.pagination.limit, this.pagination.totalDocs);
	}

	documentLabel = (document: AdminDocument) => {
		const value = this.options.collection.capabilities.upload
			? document.filename
			: this.searchField === undefined
				? undefined
				: document[this.searchField.name];
		return String(value ?? document.id);
	};

	documentSubtitle = (document: AdminDocument) => {
		if (this.options.collection.capabilities.upload) {
			return [document.mimeType, this.formatBytes(document.filesize)].filter(Boolean).join(" · ");
		}
		const email = typeof document.email === "string" ? document.email : undefined;
		if (email !== undefined && email !== this.documentLabel(document)) return email;
		const status = typeof document.status === "string" ? document.status : document._status;
		return typeof status === "string"
			? humanize(status, this.options.runtime.i18n.language)
			: document.id;
	};

	toggleSelection = (id: string) => {
		if (this.options.readOnly || this.committing) return;
		this.workingSelection = this.options.hasMany
			? this.workingSelection.includes(id)
				? this.workingSelection.filter((candidate) => candidate !== id)
				: [...this.workingSelection, id]
			: [id];
	};

	commitSelection = async () => {
		if (this.committing) return;
		this.committing = true;
		try {
			const committed = await this.options.onCommit([...this.workingSelection]);
			if (committed !== false) this.#closeBrowser();
		} catch (cause) {
			this.options.notifications.error({
				title: this.options.runtime.i18n.t("reference:updateNotConfirmed"),
				message:
					cause instanceof Error
						? cause.message
						: this.options.runtime.i18n.t("reference:selectedNotSaved"),
			});
		} finally {
			this.committing = false;
		}
	};

	chooseFilter = (key: string) => {
		if (this.committing) return;
		this.filterKey = key;
		this.page = 1;
	};

	handleSearch = (event: Event) => {
		if (this.committing) return;
		this.query = (event.currentTarget as HTMLInputElement).value;
		this.page = 1;
	};

	openNewDocument = () => {
		if (this.form.submitting || !this.canCreateDocument) return;
		this.editorDocument = undefined;
		const values = {
			...initialFormValues(this.options.collection.fields),
			...this.options.defaultValues,
		};
		this.form.reset(values);
		this.form.setAccess(undefined, "create");
		this.selectedFiles = undefined;
		this.newUserPassword = "";
		this.newUserPasswordConfirmation = "";
		this.credentialIssue = undefined;
		this.editorError = undefined;
		this.editorView = "edit";
		this.screen = "document";
		this.#loadEditorAccess(undefined, values);
	};

	openKnownDocument = (document: AdminDocument) => {
		this.#lookup.remember(document);
		this.#openEditor(document);
	};

	saveEditor = async () => {
		if (this.form.submitting || !this.canSave) return false;
		const creatingUpload =
			this.creating &&
			this.options.collection.capabilities.upload &&
			this.selectedFile !== undefined;
		const request = new AbortController();
		this.#saveRequest?.abort();
		this.#saveRequest = request;
		try {
			this.credentialIssue = this.#validateNewUserPassword();
			if (this.credentialIssue !== undefined) return false;
			const saved = await this.form.submit(this.validationFields, this.creating, async (values) => {
				if (this.editorDocument !== undefined) {
					return this.options.runtime.client.update(
						this.options.collection.slug,
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
				if (this.options.collection.capabilities.auth) {
					return this.options.runtime.client.createAuthUser(
						this.options.collection.slug,
						values,
						this.newUserPassword,
						{ signal: request.signal, locale: this.options.locale }
					);
				}
				if (!this.options.collection.capabilities.upload) {
					return this.options.runtime.client.create(this.options.collection.slug, values, {
						signal: request.signal,
						locale: this.options.locale,
					});
				}
				if (this.selectedFile === undefined) {
					throw new Error(this.options.runtime.i18n.t("uploads:chooseFileBeforeCreating"));
				}
				return this.options.runtime.client.upload(this.options.collection.slug, this.selectedFile, {
					data: values,
					signal: request.signal,
					locale: this.options.locale,
				});
			});
			if (request.signal.aborted) return false;
			const wasCreating = this.editorDocument === undefined;
			this.editorDocument = saved;
			this.#lookup.remember(saved);
			this.workingSelection = this.options.hasMany
				? [...new Set([...this.workingSelection, saved.id])]
				: [saved.id];
			this.options.runtime.documentsChanged();
			this.form.reset(documentFormValues(this.options.collection.fields, saved));
			this.form.setLocalization(this.options.locale, saved._localization?.sources);
			this.options.notifications.success({
				title: wasCreating
					? this.options.runtime.i18n.t("reference:createdLabel", {
							label: this.options.runtime.i18n.text(
								this.options.collection.labels.singular,
								this.options.collection.labels.singularTranslations
							),
						})
					: this.options.runtime.i18n.t("documents:updated"),
			});
			if (wasCreating) this.screen = "list";
			this.newUserPassword = "";
			this.newUserPasswordConfirmation = "";
			this.credentialIssue = undefined;
			return true;
		} catch (cause) {
			if (request.signal.aborted) return false;
			if (
				creatingUpload &&
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
		if (!this.creatingAuthUser || this.options.collection.authSettings === undefined) {
			return undefined;
		}
		if (this.newUserPassword === "")
			return this.options.runtime.i18n.t("reference:enterNewAccountPassword");
		if (
			Array.from(this.newUserPassword).length <
			this.options.collection.authSettings.passwordMinLength
		) {
			return this.options.runtime.i18n.t("reference:passwordTooShort", {
				minimum: this.options.runtime.i18n.formatNumber(
					this.options.collection.authSettings.passwordMinLength
				),
			});
		}
		if (
			new TextEncoder().encode(this.newUserPassword).length >
			this.options.collection.authSettings.passwordMaxBytes
		) {
			return this.options.runtime.i18n.t("reference:passwordTooLong", {
				maximum: this.options.runtime.i18n.formatNumber(
					this.options.collection.authSettings.passwordMaxBytes
				),
			});
		}
		if (this.newUserPassword !== this.newUserPasswordConfirmation) {
			return this.options.runtime.i18n.t("reference:passwordsDoNotMatch");
		}
		return undefined;
	}

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

	formatBytes = (value: unknown) => {
		if (typeof value !== "number" || !Number.isFinite(value)) return "";
		if (value < 1_024)
			return this.options.runtime.i18n.formatNumber(value, { style: "unit", unit: "byte" });
		if (value < 1_048_576)
			return this.options.runtime.i18n.formatNumber(value / 1_024, {
				maximumFractionDigits: 1,
				style: "unit",
				unit: "kilobyte",
			});
		return this.options.runtime.i18n.formatNumber(value / 1_048_576, {
			maximumFractionDigits: 1,
			style: "unit",
			unit: "megabyte",
		});
	};

	#loadEditor(id: string, signal?: AbortSignal) {
		this.editorLoading = true;
		this.editorError = undefined;
		this.#lookup
			.loadDocument(this.options.collection.slug, id, this.options.locale, signal)
			.then((document) => {
				if (signal?.aborted) return;
				if (document === undefined) {
					this.editorError =
						this.#lookup.error ?? this.options.runtime.i18n.t("reference:relatedLoadFailed");
					this.editorLoading = false;
					return;
				}
				this.#openEditor(document);
			});
	}

	#openEditor(document: AdminDocument) {
		this.editorDocument = document;
		this.form.reset(documentFormValues(this.options.collection.fields, document));
		this.form.setLocalization(this.options.locale, document._localization?.sources);
		this.form.setAccess(undefined, "update");
		this.selectedFiles = undefined;
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
		const request = new AbortController();
		this.#editorAccess = request;
		this.editorLoading = true;
		this.editorError = undefined;
		this.options.runtime.client
			.collectionAccess(this.options.collection.slug, {
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
		this.#editorAccess?.abort();
		this.#editorAccess = undefined;
		this.screen = "list";
		this.editorDocument = undefined;
		this.editorError = undefined;
		this.selectedFiles = undefined;
		this.newUserPassword = "";
		this.newUserPasswordConfirmation = "";
		this.credentialIssue = undefined;
		this.form.reset({});
		this.form.setAccess(undefined, "update");
	}

	#closeBrowser() {
		this.options.setOpen(false);
		this.options.onClose();
	}
}

function findDisplayField(fields: readonly SchemaField[]) {
	return (
		fields.find((field) => field.name === "title" || field.name === "name") ??
		fields.find((field) => field.type === "text" && field.name !== "email") ??
		fields.find((field) => field.type === "text" || field.type === "email")
	);
}

function createCollectionFilters(collection: SchemaCollection, i18n: AdminRuntime["i18n"]) {
	const result: FilterOption[] = [{ key: "", label: i18n.t("reference:all"), filter: undefined }];
	if (collection.capabilities.upload) {
		result.push(
			{
				key: "images",
				label: i18n.t("reference:images"),
				filter: { field: "mimeType", operator: "like", value: "image/" },
			},
			{
				key: "documents",
				label: i18n.t("reference:documents"),
				filter: { field: "mimeType", operator: "like", value: "application/" },
			}
		);
		return result;
	}
	const select =
		collection.fields.find((field) => field.name === "status" && field.select !== undefined) ??
		collection.fields.find((field) => field.select !== undefined);
	for (const choice of select?.select?.choices ?? []) {
		result.push({
			key: choice.value,
			label: i18n.text(choice.label, choice.labelTranslations),
			filter: { field: select?.name ?? "", operator: "equals", value: choice.value },
		});
	}
	return result;
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
						types: field.blocks.types.map((block) => ({
							...block,
							fields: block.fields.map((child) => scopeField(child, `${prefix}-${block.key}`)),
						})),
					},
				}),
	};
}

function humanize(value: string, language: string) {
	return value
		.replaceAll(/[_-]+/g, " ")
		.replace(/\b\w/g, (letter) => letter.toLocaleUpperCase(language));
}
