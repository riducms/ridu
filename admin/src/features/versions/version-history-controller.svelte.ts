import { RiduError } from "@riducms/sdk";
import type { AdminVersionsDataV1, OperationCapabilities } from "@riducms/protocol";
import type { AdminDocument, AdminVersion } from "@admin/core/api/admin-client";
import type { AdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
import type { NotificationCenter } from "@admin/core/notifications/notification-center.svelte";

export interface VersionHistoryTarget {
	slug: string;
	id: string;
	global: boolean;
	revision?: number;
	locale?: string;
}

export type VersionComparisonState =
	| { revision: number; status: "loading" }
	| { revision: number; status: "ready"; version: AdminVersion }
	| { revision: number; status: "error"; error: string };

/** One visit owns its reads and restore result, including when the route component is retained. */
export class VersionHistoryController {
	#runtime: AdminRuntime;
	#notifications: NotificationCenter;
	#target?: VersionHistoryTarget;
	#loadRequest?: AbortController;
	#comparisonRequest?: AbortController;
	#comparisonSummary?: AdminVersion;
	#visit = 0;
	#key = "";
	#history = $state.raw<AdminVersion[]>([]);
	#detail = $state.raw<AdminVersion>();
	#document = $state.raw<AdminDocument>();
	#operations = $state.raw<OperationCapabilities>();
	#loading = $state(true);
	#error = $state<string>();
	#failureStatus = $state<number>();
	#restoring = $state(false);
	#comparisonState = $state.raw<VersionComparisonState>();

	constructor(runtime: AdminRuntime, notifications: NotificationCenter) {
		this.#runtime = runtime;
		this.#notifications = notifications;
	}

	get versions() {
		return this.#history;
	}
	get selectedVersion() {
		return this.#detail;
	}
	get document() {
		return this.#document;
	}
	get loading() {
		return this.#loading;
	}
	get error() {
		return this.#error;
	}
	get canRetry() {
		return ![401, 403, 404].includes(this.#failureStatus ?? 0);
	}
	get restoring() {
		return this.#restoring;
	}
	get comparisonState() {
		return this.#comparisonState;
	}

	sync = (
		target: VersionHistoryTarget,
		manifestRevision: number,
		prepared?: AdminVersionsDataV1
	) => {
		const key = JSON.stringify([target, manifestRevision]);
		if (key === this.#key) return;
		this.#key = key;
		this.#target = target;
		this.#visit++;
		this.#restoring = false;
		this.#loadRequest?.abort();
		this.#clear();

		if (prepared) {
			const { history, detail, document } = prepared;
			const detailMissing = detail?.error?.code === "not_found";
			const failure =
				history.error ??
				(detailMissing ? undefined : detail?.error) ??
				document.document.error ??
				document.access.error;
			if (failure) {
				this.#error = failure.message;
				this.#failureStatus = failure.status;
				this.#loading = false;
				return;
			}
			if (
				history.value &&
				document.document.value &&
				document.access.value &&
				(target.revision === undefined || detail?.value || detailMissing)
			) {
				this.#apply(
					history.value,
					detail?.value,
					document.document.value,
					document.access.value.operations
				);
				return;
			}
		}
		this.#load(target);
	};

	retry = () => {
		if (this.#target && !this.#restoring) this.#load(this.#target);
	};

	syncComparison = async (summary: AdminVersion | undefined) => {
		const target = this.#target;
		if (!summary || !target || summary.Revision === this.#detail?.Revision) {
			this.#clearComparison();
			return;
		}
		if (this.#comparisonState?.revision === summary.Revision) return;

		this.#comparisonRequest?.abort();
		this.#comparisonSummary = summary;
		if (!this.#runtime.contentLocales.length) {
			this.#comparisonState = { revision: summary.Revision, status: "ready", version: summary };
			this.#comparisonRequest = undefined;
			return;
		}

		this.#comparisonState = { revision: summary.Revision, status: "loading" };
		const request = new AbortController();
		this.#comparisonRequest = request;
		const options = { signal: request.signal, locale: "all" };
		try {
			const version = target.global
				? await this.#runtime.client.globalVersion(target.slug, summary.Revision, options)
				: await this.#runtime.client.version(target.slug, target.id, summary.Revision, options);
			if (!request.signal.aborted)
				this.#comparisonState = { revision: summary.Revision, status: "ready", version };
		} catch (cause) {
			if (!request.signal.aborted)
				this.#comparisonState = {
					revision: summary.Revision,
					status: "error",
					error:
						cause instanceof Error
							? cause.message
							: this.#runtime.i18n.t("versions:historyLoadFailed"),
				};
		} finally {
			if (this.#comparisonRequest === request) this.#comparisonRequest = undefined;
		}
	};

	retryComparison = async () => {
		const state = this.#comparisonState;
		const summary = this.#comparisonSummary;
		if (state?.status !== "error" || !summary) return;
		this.#comparisonState = undefined;
		await this.syncComparison(summary);
	};

	async #load(target: VersionHistoryTarget) {
		this.#loadRequest?.abort();
		const request = new AbortController();
		this.#loadRequest = request;
		this.#loading = true;
		this.#error = undefined;
		const { client, i18n } = this.#runtime;
		const { slug, id, global, revision, locale } = target;
		const historyOptions = {
			signal: request.signal,
			locale,
		};
		const detailOptions = {
			signal: request.signal,
			locale: this.#runtime.contentLocales.length ? "all" : undefined,
		};
		const detailRequest =
			revision === undefined
				? Promise.resolve(undefined)
				: (global
						? client.globalVersion(slug, revision, detailOptions)
						: client.version(slug, id, revision, detailOptions)
					).catch((cause) => {
						if (cause instanceof RiduError && cause.code === "not_found") return undefined;
						throw cause;
					});
		try {
			const [history, detail, document, access] = await Promise.all([
				global
					? client.globalVersions(slug, historyOptions)
					: client.versions(slug, id, historyOptions),
				detailRequest,
				global
					? client.global(slug, { signal: request.signal, locale })
					: client.find(slug, id, { signal: request.signal, locale }),
				global
					? client.globalAccess(slug, { signal: request.signal, locale })
					: client.collectionAccess(slug, { id, signal: request.signal, locale }),
			]);
			if (request.signal.aborted) return;
			this.#apply(history, detail, document, access.operations);
		} catch (cause) {
			if (request.signal.aborted) return;
			this.#clear();
			this.#failureStatus = cause instanceof RiduError ? cause.status : undefined;
			this.#error = cause instanceof Error ? cause.message : i18n.t("versions:historyLoadFailed");
		} finally {
			if (!request.signal.aborted) this.#loading = false;
		}
	}

	#clear() {
		this.#clearComparison();
		this.#history = [];
		this.#detail = undefined;
		this.#document = undefined;
		this.#operations = undefined;
		this.#error = undefined;
		this.#failureStatus = undefined;
		this.#loading = true;
	}

	#clearComparison() {
		this.#comparisonRequest?.abort();
		this.#comparisonRequest = undefined;
		this.#comparisonSummary = undefined;
		this.#comparisonState = undefined;
	}

	#apply(
		history: AdminVersion[],
		detail: AdminVersion | undefined,
		document: AdminDocument,
		operations: OperationCapabilities
	) {
		// Detail is an exact authorized read, not a fallback to a potentially stale list snapshot.
		this.#detail = detail;
		this.#history = [...history].sort((a, b) => b.Revision - a.Revision);
		this.#document = document;
		this.#operations = operations;
		this.#loading = false;
	}

	canRestore(draft: boolean) {
		const revision = this.#document?._revision;
		const operation = draft || this.#detail?.Status === "draft" ? "unpublish" : "publish";
		return (
			Number.isInteger(revision) && Number(revision) > 0 && this.#operations?.[operation] === true
		);
	}

	restore = async (draft: boolean, onRestored: () => void) => {
		const target = this.#target;
		const selected = this.#detail;
		const document = this.#document;
		if (
			!target ||
			!selected ||
			!document ||
			this.#loading ||
			this.#restoring ||
			!this.canRestore(draft)
		)
			return;
		const { slug, id, global, locale } = target;
		const visit = this.#visit;
		const { client, i18n } = this.#runtime;
		this.#restoring = true;
		this.#loadRequest?.abort();
		try {
			const options = {
				revision: document._revision as number,
				draft,
				locale,
			};
			if (global) await client.restoreGlobal(slug, selected.Revision, options);
			else await client.restore(slug, id, selected.Revision, options);
			if (visit !== this.#visit) return;
			this.#runtime.documentsChanged();
			this.#notifications.success({
				title: i18n.t(draft ? "versions:revisionRestoredAsDraft" : "versions:revisionRestored", {
					revision: i18n.formatNumber(selected.Revision),
				}),
				message: i18n.t("versions:previousCurrentPreserved"),
			});
			onRestored();
		} catch (cause) {
			if (visit !== this.#visit) return;
			this.#notifications.error({
				title: i18n.t("versions:revisionNotRestored"),
				message: cause instanceof Error ? cause.message : i18n.t("versions:revisionRestoreFailed"),
			});
		} finally {
			if (visit === this.#visit) this.#restoring = false;
		}
	};

	dispose = () => {
		this.#visit++;
		this.#loadRequest?.abort();
		this.#comparisonRequest?.abort();
	};
}
