import { resolveListCells, resolveListColumns } from "@admin/features/collections/list-columns";
import { createContext } from "svelte";
import type { AdminCollectionListDataV1 } from "@riducms/protocol";
import type { FieldDocument } from "@riducms/plugin";
import type { AdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
import type { NotificationCenter } from "@admin/core/notifications/notification-center.svelte";
import { preferenceOwnerID } from "@admin/core/preferences/preference-write-queue";
import {
	collectionPath,
	collectionUploadPath,
	createDocumentPath,
	documentPath,
	withContentLocale,
} from "@admin/core/routing/admin-paths";
import { documentTitleField } from "@admin/features/documents/document-title";
import { CollectionListController } from "@admin/features/collections/collection-list-controller.svelte";
import {
	CollectionListPreferencesController,
	type CollectionListPreset,
} from "@admin/features/collections/collection-list-preferences-controller.svelte";
import type { CollectionListQuery } from "@admin/features/collections/collection-list-query";
import { createCollectionListCellFormatter } from "@admin/features/collections/collection-list-cell-values";
import { ListFilterFields } from "@admin/features/collections/list-filter-fields";
import {
	bulkEditableListFields,
	defaultListColumns,
	listMetadataFields,
	type ListColumnSelection,
	type ListPageSize,
	type ListViewState,
} from "@admin/features/collections/list-workspace";

interface CollectionListOptions {
	runtime: AdminRuntime;
	notifications: NotificationCenter;
	query: CollectionListQuery;
	readonly slug: string;
	readonly trashOnly: boolean;
	readonly navigationIdle: boolean;
	prepared: () => AdminCollectionListDataV1 | undefined;
}

export const [getCollectionList, setCollectionList] = createContext<CollectionList>();

/** One mounted list owns these controllers. Route inputs stay live through the supplied getters. */
export class CollectionList {
	readonly controller: CollectionListController;
	readonly preferences: CollectionListPreferencesController;
	readonly formatCellValue: ReturnType<typeof createCollectionListCellFormatter>;

	#collection = $derived(
		this.#runtime.manifest?.collections.find((item) => item.slug === this.slug)
	);
	#titleField = $derived(documentTitleField(this.#collection));
	#searchField = $derived(this.#titleField?.queryRestricted ? undefined : this.#titleField);
	#statusOptions = $derived(
		this.#collection?.fields.find(
			(field) => field.type === "select" && field.name === "status" && !field.queryRestricted
		)?.select?.options ?? []
	);
	#contentLocale = $derived(this.#runtime.resolveContentLocale(this.query.locale));
	#folderField = $derived(
		this.#collection?.fields.find(
			(field) => field.name === this.#collection?.admin.folderField && !field.queryRestricted
		)
	);
	#parentField = $derived(
		this.#collection?.fields.find((field) => field.name === this.#collection?.admin.parentField)
	);
	#folderCollection = $derived(
		this.#runtime.manifest?.collections.find(
			(candidate) => candidate.slug === this.#folderField?.relationship?.collectionSlug
		)
	);
	#customListCells = $derived(
		resolveListCells(this.#collection, this.#runtime.config.extensions.listCellRenderers)
	);
	#metadataFields = $derived(listMetadataFields(this.#collection, this.#runtime.i18n));
	#availableColumnFields = $derived(
		resolveListColumns(this.#collection, this.#runtime.i18n, this.#customListCells)
	);
	// An immutable schema index; its lazy lookups are invisible to readers.
	#filterFields = $derived(
		new ListFilterFields({
			fields: this.#collection?.fields ?? [],
			metadata: this.#metadataFields,
			collections: this.#runtime.manifest?.collections,
			i18n: this.#runtime.i18n,
		})
	);
	#defaultColumns = $derived([
		...new Set([
			...defaultListColumns(this.#collection, this.#availableColumnFields),
			...this.#customListCells.map((cell) => cell.field.path),
		]),
	]);
	#columnSelection = $derived(
		this.query.columnSelection(this.#availableColumnFields, this.#workspace.columns)
	);
	#visibleColumns = $derived(
		this.#columnSelection
			.filter((column) => column.active)
			.flatMap((column) => {
				const field = this.#availableColumnFields.find((field) => field.path === column.path);
				return field ? [field] : [];
			})
	);
	#pageSize = $derived(this.query.pageSize(this.#workspace.limit));
	#listFilters = $derived(this.query.filters(this.#filterFields));
	#sort = $derived(
		this.query.sortFor(
			this.#availableColumnFields.flatMap((column) => (column.field ? [column.field] : []))
		)
	);
	#view = $derived<ListViewState>({
		q: this.#searchField === undefined ? "" : this.query.search,
		status: this.#statusOptions.some((option) => option.value === this.query.status)
			? this.query.status
			: "",
		folder: this.#folderField === undefined ? "" : this.query.folder,
		view:
			!this.trashOnly && this.#parentField !== undefined && this.query.hierarchy
				? "hierarchy"
				: "list",
		filters: this.#listFilters,
		sort: this.#sort,
		columns: this.#columnSelection,
		limit: this.#pageSize,
	});
	#bulkEditableFields = $derived(
		bulkEditableListFields(this.#collection?.fields ?? []).filter((field) =>
			this.controller.canBulkUpdateField(field.path)
		)
	);
	#customResults = $derived(
		this.#runtime.config.extensions.listResultsRenderers.find(
			(results) => results.collection === this.slug
		)
	);
	#filtered = $derived(
		this.#view.q !== "" ||
			this.#view.status !== "" ||
			this.#view.folder !== "" ||
			this.#view.filters.length > 0
	);
	#searchLabel = $derived(
		this.#searchField === undefined
			? this.#runtime.i18n.t("collections:searchDocuments")
			: this.#runtime.i18n.t("collections:searchBy", { label: this.#searchField.admin.label })
	);

	constructor(private readonly options: CollectionListOptions) {
		const { runtime, notifications, query } = options;
		this.preferences = new CollectionListPreferencesController(
			runtime.client,
			notifications,
			() => runtime.session?.id ?? "",
			runtime.i18n
		);
		// Resolve workspace defaults before the data controller reads the effective query.
		$effect.pre(() => {
			this.preferences.enter(
				{
					slug: this.slug,
					sessionID: runtime.session?.id ?? "",
					preferenceOwnerID: preferenceOwnerID(runtime.session),
					columnFields: this.#availableColumnFields,
					filterFields: this.#filterFields,
					defaultColumns: this.#defaultColumns,
				},
				options.prepared()?.preferences
			);
		});
		const list = this;
		this.controller = new CollectionListController({
			client: runtime.client,
			notifications,
			i18n: runtime.i18n,
			get slug() {
				return list.slug;
			},
			get locale() {
				return list.contentLocale;
			},
			get query() {
				return query.readQuery({
					limit: list.#pageSize,
					columns: list.#visibleColumns.map((field) => field.path),
					locale: list.contentLocale,
					trash: list.trashOnly,
				});
			},
			get ready() {
				return !list.preferences.workspacePending;
			},
			get prepared() {
				return options.prepared();
			},
			get collection() {
				return list.collection;
			},
			get trashOnly() {
				return list.trashOnly;
			},
		});
		this.formatCellValue = createCollectionListCellFormatter({
			i18n: runtime.i18n,
			get collections() {
				return runtime.manifest?.collections ?? [];
			},
			canReadField: this.controller.canReadField,
		});
		$effect(() => {
			const unregisterBlocker = runtime.registerContentLocaleBlocker(
				() => this.controller.bulkPending || this.controller.selectionPending
			);
			return () => {
				unregisterBlocker();
				this.preferences.dispose();
			};
		});
	}

	get slug() {
		return this.options.slug;
	}

	get query() {
		return this.options.query;
	}

	get collection() {
		return this.#collection;
	}

	get label() {
		return this.#collection?.labels.plural ?? this.slug;
	}

	get searchLabel() {
		return this.#searchLabel;
	}

	get titleField() {
		return this.#titleField;
	}

	get contentLocale() {
		return this.#contentLocale;
	}

	get trashOnly() {
		return this.options.trashOnly;
	}

	get navigationIdle() {
		return this.options.navigationIdle;
	}

	get #runtime() {
		return this.options.runtime;
	}

	get #workspace() {
		return this.preferences.workspace;
	}

	get view() {
		return this.#view;
	}

	get availableColumnFields() {
		return this.#availableColumnFields;
	}

	get visibleColumns() {
		return this.#visibleColumns;
	}

	get filterFields() {
		return this.#filterFields;
	}

	get folderCollection() {
		return this.#folderCollection;
	}

	get showFolder() {
		return this.#folderField !== undefined;
	}

	get showHierarchy() {
		return this.#parentField !== undefined && !this.trashOnly;
	}

	get parentFieldName() {
		return this.#parentField?.name;
	}

	get filtered() {
		return this.#filtered;
	}

	get bulkEditableFields() {
		return this.#bulkEditableFields;
	}

	get customListCells() {
		return this.#customListCells;
	}

	get customResults() {
		return this.#customResults;
	}

	get columnChoices() {
		return this.query.columnsForPicker(this.#availableColumnFields, this.#workspace.columns);
	}

	get createHref() {
		return withContentLocale(createDocumentPath(this.slug), this.contentLocale);
	}

	get uploadHref() {
		return !this.trashOnly && this.controller.canCreate && this.#collection?.capabilities.upload
			? withContentLocale(collectionUploadPath(this.slug), this.contentLocale)
			: undefined;
	}

	get foldersHref() {
		return this.#folderCollection && this.#folderCollection.admin.hidden !== true
			? withContentLocale(collectionPath(this.#folderCollection.slug), this.contentLocale)
			: undefined;
	}

	get ready() {
		return (
			!this.preferences.workspacePending &&
			(this.controller.status === "ready" || this.controller.status === "failed")
		);
	}

	// Keep the displayed URL and saved workspace coordinated at the feature boundary.
	setColumns = (columns: ListColumnSelection[]) => {
		this.query.setColumns(columns);
		return this.preferences.persistWorkspace({ ...this.preferences.workspace, columns });
	};

	setPageSize = (limit: ListPageSize) => {
		this.query.selectPageSize(limit);
		return this.preferences.persistWorkspace({ ...this.preferences.workspace, limit });
	};

	applySavedView = (preset: CollectionListPreset) => {
		const saved = this.preferences.persistWorkspace({
			columns: preset.columns,
			limit: preset.limit,
		});
		this.query.applyPreset(preset, this.contentLocale);
		return saved;
	};

	saveView = (name: string) => this.preferences.savePreset(name, this.#view);

	documentHref = (document: FieldDocument) =>
		withContentLocale(documentPath(this.slug, document.id), this.contentLocale);
}
