import type { SchemaField } from "@riducms/protocol";
import { RiduError } from "@riducms/sdk";
import type { AdminI18n } from "@riducms/translations";

import type { AdminClient } from "@admin/core/api/admin-client";
import type { NotificationCenter } from "@admin/core/notifications/notification-center.svelte";
import { getPreferenceWriteQueue } from "@admin/core/preferences/preference-write-queue";
import {
	normalizeWorkspacePreference,
	parseListFilters,
	parseListPageSize,
	type ListFilter,
	type ListPageSize,
	type ListWorkspacePreference,
} from "@admin/features/collections/list-workspace";

export interface CollectionListPreset {
	name: string;
	q: string;
	status: string;
	folder: string;
	view: "list" | "hierarchy";
	filters: ListFilter[];
	sort: string;
	columns: string[];
	limit: ListPageSize;
	showStatus: boolean;
	showID: boolean;
	showCreated: boolean;
	showUpdated: boolean;
}

export type CollectionListPresetInput = Omit<CollectionListPreset, "name">;

interface CollectionListPreferenceRoute {
	slug: string;
	sessionID: string;
	preferenceOwnerID: string;
	columnFields: readonly SchemaField[];
	filterFields: readonly SchemaField[];
	defaultColumns: readonly string[];
}

type PreferenceClient = Pick<AdminClient, "preference" | "setPreference">;
type PreferenceNotifications = Pick<NotificationCenter, "success" | "error">;

interface ActivePreferenceRoute extends CollectionListPreferenceRoute {
	key: string;
	generation: number;
	ownerKey: string;
	presetKey: string;
	presetWriteKey: string;
	workspaceKey: string;
	workspaceWriteKey: string;
}

const initialWorkspace: ListWorkspacePreference = {
	columns: [],
	showStatus: true,
	showID: false,
	showCreated: false,
	showUpdated: true,
	limit: 25,
};

export class CollectionListPreferencesController {
	workspace = $state.raw<ListWorkspacePreference>(initialWorkspace);
	presets = $state.raw<CollectionListPreset[]>([]);
	presetsPending = $state(false);
	#active?: ActivePreferenceRoute;
	#generation = 0;
	#presetLoad?: AbortController;
	#workspaceLoad?: AbortController;
	#workspaceWrites = getPreferenceWriteQueue();
	#workspaceSaveVersions = new Map<string, number>();
	#unsubscribeWorkspace?: () => void;
	#unsubscribePresets?: () => void;

	constructor(
		private readonly client: PreferenceClient,
		private readonly notifications: PreferenceNotifications,
		private readonly sessionID: () => string,
		private readonly i18n: AdminI18n
	) {}

	enter = (route: CollectionListPreferenceRoute) => {
		const key = routeKey(route);
		if (this.#active?.key === key) return;
		this.#abortLoads();
		this.#unsubscribeWrites();
		const generation = ++this.#generation;
		this.#active = {
			...route,
			key,
			generation,
			ownerKey: ownerKey(route),
			presetKey: `collection:${route.slug}:presets`,
			presetWriteKey: JSON.stringify([route.preferenceOwnerID, route.slug, "presets"]),
			workspaceKey: `collection:${route.slug}:workspace`,
			workspaceWriteKey: JSON.stringify([route.preferenceOwnerID, route.slug, "workspace"]),
		};
		const fallback = fallbackWorkspace(route.defaultColumns);
		this.workspace = fallback;
		this.presets = [];
		this.#subscribeWrites(this.#active);
		if (route.sessionID === "" || route.slug === "") return;
		this.#loadWorkspace(this.#active, fallback);
		this.#loadPresets(this.#active);
	};

	persistWorkspace = async (next: ListWorkspacePreference) => {
		const active = this.#active;
		if (active === undefined) return false;
		this.workspace = next;
		this.#workspaceLoad?.abort();
		this.#workspaceLoad = undefined;
		if (active.sessionID === "") return true;

		const version = (this.#workspaceSaveVersions.get(active.workspaceWriteKey) ?? 0) + 1;
		this.#workspaceSaveVersions.set(active.workspaceWriteKey, version);
		try {
			const result = await this.#workspaceWrites.enqueue(
				active.workspaceWriteKey,
				active.preferenceOwnerID,
				() => this.sessionID() === active.sessionID,
				() => this.client.setPreference(active.workspaceKey, next)
			);
			return result.dispatched;
		} catch (cause) {
			if (
				this.#isSameOwner(active) &&
				this.#workspaceSaveVersions.get(active.workspaceWriteKey) === version
			) {
				this.notifications.error({
					title: this.i18n.t("collections:listPreferencesNotSaved"),
					message: cause instanceof Error ? cause.message : undefined,
				});
			}
			return false;
		}
	};

	savePreset = async (name: string, input: CollectionListPresetInput) => {
		const active = this.#active;
		const normalizedName = name.trim();
		if (
			active === undefined ||
			active.sessionID === "" ||
			normalizedName === "" ||
			this.presetsPending
		) {
			return false;
		}
		this.#presetLoad?.abort();
		this.#presetLoad = undefined;
		const next = [
			...this.presets.filter((preset) => preset.name !== normalizedName),
			{ name: normalizedName, ...input },
		];
		try {
			const result = await this.#workspaceWrites.enqueue(
				active.presetWriteKey,
				active.preferenceOwnerID,
				() => this.sessionID() === active.sessionID,
				() => this.client.setPreference(active.presetKey, next)
			);
			if (!result.dispatched || !this.#isSameOwner(active)) return false;
			this.notifications.success({ title: this.i18n.t("collections:viewSaved") });
			return true;
		} catch (cause) {
			if (this.#isSameOwner(active)) {
				this.notifications.error({
					title: this.i18n.t("collections:viewNotSaved"),
					message: cause instanceof Error ? cause.message : undefined,
				});
			}
			return false;
		}
	};

	deletePreset = async (name: string) => {
		const active = this.#active;
		if (active === undefined || active.sessionID === "" || this.presetsPending) return false;
		this.#presetLoad?.abort();
		this.#presetLoad = undefined;
		const next = this.presets.filter((preset) => preset.name !== name);
		try {
			const result = await this.#workspaceWrites.enqueue(
				active.presetWriteKey,
				active.preferenceOwnerID,
				() => this.sessionID() === active.sessionID,
				() => this.client.setPreference(active.presetKey, next)
			);
			if (!result.dispatched || !this.#isSameOwner(active)) return false;
			this.notifications.success({ title: this.i18n.t("collections:savedViewDeleted") });
			return true;
		} catch (cause) {
			if (this.#isSameOwner(active)) {
				this.notifications.error({
					title: this.i18n.t("collections:savedViewNotDeleted"),
					message: cause instanceof Error ? cause.message : undefined,
				});
			}
			return false;
		}
	};

	dispose() {
		this.#generation += 1;
		this.#active = undefined;
		this.#abortLoads();
		this.#unsubscribeWrites();
	}

	#loadWorkspace(active: ActivePreferenceRoute, fallback: ListWorkspacePreference) {
		const request = new AbortController();
		this.#workspaceLoad = request;
		void this.client
			.preference<ListWorkspacePreference>(active.workspaceKey, { signal: request.signal })
			.then((value) => {
				if (
					!request.signal.aborted &&
					this.#isCurrent(active) &&
					!this.#workspaceWrites.isPending(active.workspaceWriteKey)
				) {
					this.workspace = normalizeWorkspacePreference(value, active.columnFields, fallback);
				}
			})
			.catch((cause) => {
				if (
					!request.signal.aborted &&
					this.#isCurrent(active) &&
					(!(cause instanceof RiduError) || cause.code !== "not_found")
				) {
					this.notifications.error({
						title: this.i18n.t("collections:listPreferencesUnavailable"),
						message: cause instanceof Error ? cause.message : undefined,
					});
				}
			})
			.finally(() => {
				if (this.#workspaceLoad === request) this.#workspaceLoad = undefined;
			});
	}

	#loadPresets(active: ActivePreferenceRoute) {
		const request = new AbortController();
		this.#presetLoad = request;
		void this.client
			.preference<CollectionListPreset[]>(active.presetKey, { signal: request.signal })
			.then((value) => {
				if (
					!request.signal.aborted &&
					this.#isCurrent(active) &&
					!this.#workspaceWrites.isPending(active.presetWriteKey)
				) {
					this.presets = validPresets(value, active.filterFields, active.defaultColumns);
				}
			})
			.catch((cause) => {
				if (
					!request.signal.aborted &&
					this.#isCurrent(active) &&
					(!(cause instanceof RiduError) || cause.code !== "not_found")
				) {
					this.notifications.error({
						title: this.i18n.t("collections:savedViewsUnavailable"),
						message: cause instanceof Error ? cause.message : undefined,
					});
				}
			})
			.finally(() => {
				if (this.#presetLoad === request) this.#presetLoad = undefined;
			});
	}

	#subscribeWrites(active: ActivePreferenceRoute) {
		if (active.sessionID === "" || active.slug === "") {
			this.presetsPending = false;
			return;
		}
		this.#unsubscribeWorkspace = this.#workspaceWrites.subscribe(
			active.workspaceWriteKey,
			(state) => {
				if (state.pending || !state.hasValue || !this.#isSameOwner(active)) return;
				const current = this.#active;
				if (current === undefined) return;
				this.workspace = normalizeWorkspacePreference(
					state.value,
					current.columnFields,
					fallbackWorkspace(current.defaultColumns)
				);
			}
		);
		this.#unsubscribePresets = this.#workspaceWrites.subscribe(active.presetWriteKey, (state) => {
			if (!this.#isSameOwner(active)) return;
			this.presetsPending = state.pending;
			if (state.pending || !state.hasValue) return;
			const current = this.#active;
			if (current === undefined) return;
			this.presets = validPresets(state.value, current.filterFields, current.defaultColumns);
		});
	}

	#unsubscribeWrites() {
		this.#unsubscribeWorkspace?.();
		this.#unsubscribeWorkspace = undefined;
		this.#unsubscribePresets?.();
		this.#unsubscribePresets = undefined;
	}

	#isCurrent(active: ActivePreferenceRoute) {
		return this.#active?.key === active.key && this.#active.generation === active.generation;
	}

	#isSameOwner(active: ActivePreferenceRoute) {
		return this.#active?.ownerKey === active.ownerKey;
	}

	#abortLoads() {
		this.#presetLoad?.abort();
		this.#presetLoad = undefined;
		this.#workspaceLoad?.abort();
		this.#workspaceLoad = undefined;
	}
}

function routeKey(route: CollectionListPreferenceRoute) {
	return JSON.stringify([
		route.slug,
		route.sessionID,
		route.preferenceOwnerID,
		route.columnFields.map((field) => field.path),
		route.filterFields.map((field) => field.path),
		route.defaultColumns,
	]);
}

function ownerKey(route: CollectionListPreferenceRoute) {
	return JSON.stringify([route.preferenceOwnerID, route.slug]);
}

function fallbackWorkspace(defaultColumns: readonly string[]): ListWorkspacePreference {
	return { ...initialWorkspace, columns: [...defaultColumns] };
}

function validPresets(
	value: unknown,
	fields: readonly SchemaField[],
	defaultColumns: readonly string[]
): CollectionListPreset[] {
	if (!Array.isArray(value)) return [];
	return value.flatMap((candidate) => {
		if (typeof candidate !== "object" || candidate === null) return [];
		const record = candidate as Partial<CollectionListPreset>;
		if (
			typeof record.name !== "string" ||
			typeof record.q !== "string" ||
			typeof record.status !== "string" ||
			typeof record.folder !== "string" ||
			(record.view !== "list" && record.view !== "hierarchy") ||
			typeof record.showStatus !== "boolean" ||
			typeof record.showUpdated !== "boolean"
		) {
			return [];
		}
		return [
			{
				name: record.name,
				q: record.q,
				status: record.status,
				folder: record.folder,
				view: record.view,
				filters: parseListFilters(
					Array.isArray(record.filters) ? JSON.stringify(record.filters) : null,
					fields
				),
				sort: typeof record.sort === "string" ? record.sort : "",
				columns: Array.isArray(record.columns)
					? record.columns.filter((name): name is string => typeof name === "string")
					: [...defaultColumns],
				limit: parseListPageSize(
					typeof record.limit === "number" ? String(record.limit) : null,
					25
				),
				showStatus: record.showStatus,
				showID: record.showID === true,
				showCreated: record.showCreated === true,
				showUpdated: record.showUpdated,
			},
		];
	});
}
