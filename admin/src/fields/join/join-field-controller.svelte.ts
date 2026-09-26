import { untrack } from "svelte";
import type { FieldAuthoringHost } from "@riducms/plugin";
import type { SchemaField } from "@riducms/protocol";
import type { AdminDocument } from "@admin/core/api/admin-client";
import type { FormController } from "@admin/core/forms/form-controller.svelte";
import type { AdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
import type { NotificationCenter } from "@admin/core/notifications/notification-center.svelte";
import { readDocumentPath } from "@admin/core/schema/read-document-path";
import { createCollectionListCellFormatter } from "@admin/features/collections/collection-list-cell-values";
import { resolveListColumns } from "@admin/features/collections/list-columns";
import type { ListColumn, ListColumnSelection } from "@admin/features/collections/list-workspace";
import { documentLabel } from "@admin/features/documents/document-title";
import {
	defaultJoinColumns,
	joinDefaultValues,
	joinDocuments,
	joinReference,
	sortJoinDocuments,
} from "@admin/fields/join/join-field-values";

interface JoinFieldOptions {
	runtime: AdminRuntime;
	notifications: NotificationCenter;
	readonly field: SchemaField;
	readonly form: FormController;
	readonly authoring: FieldAuthoringHost | undefined;
}

type BrowserMode = "manage" | "create" | "edit";

/** One mounted join owns its saved rows, authoring requests and reference-label cache. */
export class JoinFieldController {
	#browser = $state<{ open: boolean; owner: string; mode: BrowserMode; documentID?: string }>();
	#pending = $state(false);
	#mutationError = $state<string>();
	#mutationRequest?: AbortController;
	#refreshRequest?: AbortController;
	#labelRequest = new AbortController();
	#labelPending = new Set<string>();
	#referenceDocuments = $state.raw<Record<string, AdminDocument | null>>({});
	#disposed = false;
	#columnOverride = $state<{ owner: string; columns: ListColumnSelection[] }>();
	#sortOverride = $state<{ owner: string; value: string }>();

	#owner = $derived.by(() =>
		JSON.stringify([
			this.options.form.resource?.collection,
			this.options.form.resource?.id,
			this.options.form.resource?.global,
			this.options.form.contentLocale,
			this.options.field.path,
			this.options.field.join?.collectionId,
		])
	);
	#target = $derived.by(() =>
		this.options.authoring?.collections.find(
			(collection) => collection.id === this.options.field.join?.collectionId
		)
	);
	#operations = $derived.by(() =>
		this.#target ? this.options.runtime.collectionOperations[this.#target.slug] : undefined
	);
	#documents = $derived.by(() => joinDocuments(this.options.form.get(this.options.field.path)));

	#columns = $derived.by(() => resolveListColumns(this.#target, this.options.runtime.i18n));
	#defaultColumns = $derived.by(() =>
		defaultJoinColumns(this.options.field, this.#target, this.#columns)
	);
	#columnSelection = $derived.by(() =>
		this.#columnOverride?.owner === this.#owner
			? this.#columnOverride.columns
			: this.#defaultColumns
	);
	#visibleColumns = $derived.by(() =>
		this.#columnSelection.flatMap((selection) => {
			const column =
				selection.active && this.#columns.find((column) => column.path === selection.path);
			return column ? [column] : [];
		})
	);

	#sort = $derived.by(() =>
		this.#sortOverride?.owner === this.#owner
			? this.#sortOverride.value
			: (this.options.field.join?.defaultSort ?? "")
	);
	#sortedDocuments = $derived.by(() =>
		sortJoinDocuments(
			this.#documents,
			this.#sort,
			this.#columns,
			this.options.runtime.i18n.language,
			this.referenceText
		)
	);
	#formatCell: ReturnType<typeof createCollectionListCellFormatter>;

	constructor(private readonly options: JoinFieldOptions) {
		this.#formatCell = createCollectionListCellFormatter({
			i18n: options.runtime.i18n,
			get collections() {
				return options.authoring?.collections ?? [];
			},
			// Computed join rows have already passed server-side field redaction.
			canReadField: () => true,
		});

		// Revoke callbacks and pending writes before a retained field switches document or locale.
		$effect.pre(() => {
			this.#owner;
			return () => {
				this.#browser = undefined;
				this.#mutationRequest?.abort();
				this.#refreshRequest?.abort();
				this.#mutationRequest = undefined;
				this.#refreshRequest = undefined;
				this.#pending = false;
				this.#mutationError = undefined;
			};
		});

		// Label results belong to one source, locale and saved-document revision.
		$effect.pre(() => {
			this.#owner;
			this.options.runtime.documentRevision;
			return () => {
				this.#labelRequest.abort();
				this.#labelRequest = new AbortController();
				this.#labelPending.clear();
				this.#referenceDocuments = {};
			};
		});

		$effect(() => {
			this.#owner;
			this.options.runtime.documentRevision;
			const references = this.#visibleColumns.flatMap((column) => {
				const field = column.field;
				if (!field?.relationship && !field?.upload) return [];

				return this.#documents.flatMap((document) => {
					const raw = readDocumentPath(document, column.path);
					return (Array.isArray(raw) ? raw : [raw]).flatMap((value) => {
						const reference = joinReference(
							value,
							field,
							this.options.authoring?.collections ?? []
						);
						return reference &&
							(reference.populated === undefined ||
								reference.populated === null ||
								reference.populated === "")
							? [reference]
							: [];
					});
				});
			});
			// Cache reads are bookkeeping; completed label requests must not launch another load.
			untrack(() => this.#loadReferenceLabels(references));
		});

		$effect(() => () => {
			this.#disposed = true;
			this.#mutationRequest?.abort();
			this.#refreshRequest?.abort();
			this.#labelRequest.abort();
		});
	}

	get targetCollection() {
		return this.#target;
	}
	get sourceID() {
		return this.options.form.resource?.global ? undefined : this.options.form.resource?.id;
	}

	get canCreate() {
		return (
			this.sourceID !== undefined &&
			!this.options.field.admin.readOnly &&
			this.options.field.join?.allowCreate !== false &&
			this.#operations?.create === true
		);
	}
	get canManage() {
		return (
			!this.options.field.admin.readOnly &&
			this.sourceID !== undefined &&
			this.#operations?.update === true
		);
	}
	get canOpenDocument() {
		return this.sourceID !== undefined && this.#operations?.read === true;
	}
	get editingBlocked() {
		return this.options.form.editingBlocked;
	}

	get pending() {
		return this.#pending;
	}
	get mutationError() {
		return this.#mutationError;
	}

	get documents() {
		return this.#documents;
	}
	get sortedDocuments() {
		return this.#sortedDocuments;
	}

	get availableColumns() {
		return this.#columns;
	}
	get columnSelection() {
		return this.#columnSelection;
	}
	get visibleColumns() {
		return this.#visibleColumns;
	}
	get sort() {
		return this.#sort;
	}

	get browserOpen() {
		return this.#browser?.owner === this.#owner && this.#browser.open;
	}
	get browserMode() {
		return this.#browser?.mode;
	}
	get browserDocumentID() {
		return this.#browser?.documentID;
	}
	get browserReadOnly() {
		return (
			this.options.field.admin.readOnly ||
			this.editingBlocked ||
			(this.browserMode === "create"
				? !this.canCreate
				: this.browserMode === "edit"
					? this.#operations?.update !== true
					: !this.canManage)
		);
	}
	get defaultValues() {
		return joinDefaultValues(this.options.field.join?.on ?? "", this.sourceID ?? "");
	}

	setColumns = (columns: ListColumnSelection[]) => {
		this.#columnOverride = { owner: this.#owner, columns };
	};

	setSort = (path: string, descending: boolean) => {
		this.#sortOverride = { owner: this.#owner, value: `${descending ? "-" : ""}${path}` };
	};

	setBrowserOpen = (open: boolean) => {
		if (this.#browser) this.#browser.open = open;
	};

	openBrowser = (mode: BrowserMode, documentID?: string) => {
		if (
			this.#disposed ||
			this.sourceID === undefined ||
			!this.#target ||
			!this.options.authoring?.referenceBrowser
		)
			return;
		this.#browser = { open: true, owner: this.#owner, mode, documentID };
	};

	referenceText = (value: unknown, field: SchemaField): string => {
		const i18n = this.options.runtime.i18n;
		if (Array.isArray(value))
			return i18n.formatList(value.map((value) => this.referenceText(value, field)));

		const reference = joinReference(value, field, this.options.authoring?.collections ?? []);
		if (!reference) return "—";
		if (
			reference.populated !== undefined &&
			reference.populated !== null &&
			reference.populated !== ""
		)
			return String(reference.populated);

		const document = this.#referenceDocuments[reference.key];
		const label = document ? documentLabel(reference.collection, document) : undefined;
		return label && label !== reference.id ? label : i18n.t("fields:relatedDocument");
	};

	cellText = (document: AdminDocument, column: ListColumn) => {
		if (column.field?.relationship || column.field?.upload)
			return this.referenceText(readDocumentPath(document, column.path), column.field);
		return column.field
			? (this.#formatCell(document, column.field) ?? "—")
			: String(readDocumentPath(document, column.path) ?? "—");
	};

	async #loadReferenceLabels(references: NonNullable<ReturnType<typeof joinReference>>[]) {
		const groups = new Map<string, Map<string, string>>();
		for (const reference of references) {
			if (reference.key in this.#referenceDocuments || this.#labelPending.has(reference.key))
				continue;

			this.#labelPending.add(reference.key);
			const group = groups.get(reference.collection.slug) ?? new Map<string, string>();
			group.set(reference.id, reference.key);
			groups.set(reference.collection.slug, group);
		}

		const signal = this.#labelRequest.signal;
		const locale = this.options.form.contentLocale;

		// Bound request size, not the number of labels the author can resolve.
		for (const [slug, group] of groups) {
			const ids = [...group.keys()];

			for (let offset = 0; offset < ids.length; offset += 100) {
				if (signal.aborted || this.#disposed) return;

				const batch = ids.slice(offset, offset + 100);
				const resolved: Record<string, AdminDocument | null> = Object.fromEntries(
					batch.map((id) => [group.get(id)!, null])
				);

				try {
					let page = 1;
					while (true) {
						const result = await this.options.runtime.client.list(slug, {
							where: { id: { in: batch } },
							limit: 100,
							page,
							depth: 0,
							locale,
							signal,
						});
						if (signal.aborted || this.#disposed) return;

						for (const document of result.docs) {
							const key = group.get(document.id);
							if (key) resolved[key] = document;
						}

						if (page >= result.pagination.totalPages) break;
						page++;
					}
				} catch {
					// Denied, deleted or unavailable references retain the neutral label until refresh.
				}
				if (signal.aborted || this.#disposed) return;

				this.#referenceDocuments = { ...this.#referenceDocuments, ...resolved };
				for (const id of batch) this.#labelPending.delete(group.get(id)!);
			}
		}
	}

	commit = async (ids: string[]) => {
		if (
			this.#disposed ||
			this.editingBlocked ||
			this.options.field.admin.readOnly ||
			(this.browserMode !== "create" && !this.canManage)
		)
			return false;

		this.#refreshRequest?.abort();
		const source = this.options.form.resource;
		if (source?.id === undefined || source.global || !this.#target || !this.options.field.join)
			return false;

		const owner = this.#owner;
		const request = new AbortController();
		this.#mutationRequest?.abort();
		this.#mutationRequest = request;
		const current = () =>
			!this.#disposed &&
			!request.signal.aborted &&
			this.#mutationRequest === request &&
			owner === this.#owner;

		const { collection, id } = source;
		const {
			path,
			admin: { label },
		} = this.options.field;
		const locale = this.options.form.contentLocale;
		const creating = this.browserMode === "create";

		this.#pending = true;
		this.#mutationError = undefined;

		try {
			let confirmed = this.#documents;
			if (creating) {
				// Creation already saved the backlink; attaching it again could require update access.
				const refreshed = await this.#refresh(collection, id, path, locale, request.signal);
				if (!current()) return false;
				this.#documents = refreshed;
				confirmed = refreshed;
				if (ids.every((id) => refreshed.some((document) => document.id === id))) return true;
				if (!this.canManage) return false;
			}

			const currentIDs = new Set(confirmed.map((document) => document.id));
			const requested = new Set(ids);
			const additions = ids.filter((id) => !currentIDs.has(id));
			const removals = creating
				? []
				: confirmed
						.filter((document) => !requested.has(document.id))
						.map((document) => document.id);
			if (additions.length === 0 && removals.length === 0) return true;

			const result = await this.options.runtime.client.mutateJoin(
				collection,
				id,
				path,
				{ additions, removals },
				{ signal: request.signal, locale }
			);
			if (!current()) return false;

			this.#documents = joinDocuments(readDocumentPath(result.doc, path));
			this.options.runtime.documentsChanged();
			this.options.notifications.success({
				title: this.options.runtime.i18n.t("fields:updated", { label }),
				message: this.options.runtime.i18n.t("fields:joinChanges", {
					added: result.added,
					removed: result.removed,
				}),
			});
			return true;
		} catch (cause) {
			if (!current()) return false;

			this.#mutationError =
				cause instanceof Error
					? cause.message
					: this.options.runtime.i18n.t("errors:relatedDocumentsUpdate");
			const refreshed = await this.#refresh(collection, id, path, locale, request.signal).catch(
				() => undefined
			);
			if (!current()) return false;
			if (refreshed !== undefined) this.#documents = refreshed;

			this.options.notifications.error({
				title: this.options.runtime.i18n.t("errors:updateUnconfirmed", { label }),
				message: this.#mutationError,
			});
			return false;
		} finally {
			if (current()) {
				this.#pending = false;
				this.#mutationRequest = undefined;
			}
		}
	};

	async #refresh(
		collection: string,
		id: string,
		path: string,
		locale: string | undefined,
		signal: AbortSignal
	) {
		const document = await this.options.runtime.client.find(collection, id, { locale, signal });
		return joinDocuments(readDocumentPath(document, path));
	}

	browserClosed = async () => {
		const browser = this.#browser;
		this.setBrowserOpen(false);
		const source = this.options.form.resource;
		if (
			browser?.owner !== this.#owner ||
			this.#disposed ||
			source?.id === undefined ||
			source.global
		)
			return;

		const owner = this.#owner;
		const path = this.options.field.path;
		const locale = this.options.form.contentLocale;
		const request = new AbortController();
		this.#refreshRequest?.abort();
		this.#refreshRequest = request;

		try {
			const refreshed = await this.#refresh(
				source.collection,
				source.id,
				path,
				locale,
				request.signal
			);
			if (
				this.#disposed ||
				request.signal.aborted ||
				this.#refreshRequest !== request ||
				this.#owner !== owner
			)
				return;
			this.#documents = refreshed;
		} catch {
			// A failed refresh leaves the last confirmed rows visible.
		} finally {
			if (this.#refreshRequest === request) this.#refreshRequest = undefined;
		}
	};
}
