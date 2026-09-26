import {
	isRecord,
	ADMIN_PREPARED_ROUTE_STATE_MEDIA_TYPE,
	ADMIN_PREPARED_ROUTE_STATE_VERSION,
	type AdminPreparedRouteDataV1,
	type AdminCreateDataV1,
	type AdminPreparedRouteStateV1,
} from "@riducms/protocol";
import { RiduError, type AdminLoader } from "@riducms/sdk";
import { createContext } from "svelte";

import type { AdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
import { peekFormDraft } from "@admin/core/forms/form-draft-recovery";
import { recoverFormDraft } from "@admin/core/forms/form-schema";
import { preparedCustomView } from "@admin/core/routing/admin-view-selection";

import {
	readEmbeddedAdminState,
	validateAdminPreparedState,
	normalizedURLIdentity,
} from "@admin/core/bootstrap/admin-prepared-state";
import {
	moduleGroupsForRelativeURL,
	preloadAdminModuleGroups,
} from "@admin/core/bootstrap/admin-route-modules";

export class AdminBootstrapCoordinator {
	#initialState = readEmbeddedAdminState();
	#contextKey = "";
	#routeKey = "";
	#routeData?: {
		identity: string;
		data: AdminPreparedRouteDataV1;
		loaders: AdminPreparedRouteStateV1["loaders"];
	};
	#runtime?: AdminRuntime;
	#basePath = "/admin";

	get initialState() {
		return this.#initialState;
	}

	configure(runtime: AdminRuntime) {
		this.#runtime = runtime;
		this.#basePath = normalizeAdminBasePath(runtime.adminBasePath);
	}

	get contextKey() {
		return this.#contextKey;
	}

	async prepareInitial(options: { stageRoute?: boolean } = {}) {
		const state = this.initialState;
		if (state === undefined) return;

		if (state.outcome === "redirect") {
			window.location.replace(state.location);
			return neverSettles();
		}
		if (state.outcome === "reload") {
			window.location.reload();
			return neverSettles();
		}

		if (options.stageRoute === false) {
			this.clear();
			this.#initialState = undefined;
			this.#clearContext();
			return;
		}

		this.#adoptContext(state);
		const preparedState = await this.#prepareRestoredCreateDraft(state);
		await preloadAdminModuleGroups(preparedState.moduleGroups);
		if (preparedState.outcome === "prepared") this.stage(preparedState);
	}

	loader = async ({ request }: { request: Request }) => {
		const url = new URL(request.url);

		try {
			// Go marks its HTML even when the initial snapshot is missing or invalid.
			// Browser-only hosts such as Vite have neither that marker nor the endpoint.
			if (
				this.#contextKey === "" &&
				document.querySelector('meta[name="ridu-admin-build-id"]') === null
			) {
				await preloadAdminModuleGroups(
					moduleGroupsForRelativeURL(this.#relativePath(url.pathname))
				);
				return null;
			}

			// Start URL-known chunks while Go reads the data. The response may name
			// additional chunks (such as an upload preview), which we await below.
			const [state] = await Promise.all([
				this.#fetchRouteState(url, request.signal),
				preloadAdminModuleGroups(moduleGroupsForRelativeURL(this.#relativePath(url.pathname))),
			]);
			if (request.signal.aborted) return null;

			// Recover a missing or invalidated Go context through a fresh document;
			// navigation data cannot establish an authoritative runtime on its own.
			if (state.outcome === "reload" || this.#contextKey === "") {
				window.location.assign(url.href);
				return neverSettles();
			}
			if (state.outcome === "redirect") {
				window.location.assign(state.location);
				return neverSettles();
			}

			const preparedState = await this.#prepareRestoredCreateDraft(state, request.signal);
			await preloadAdminModuleGroups(preparedState.moduleGroups);
			if (request.signal.aborted) return null;

			// Returning data does not publish it. Only commit() may give it to the
			// destination; a newer navigation can still supersede this loader.
			return preparedState;
		} catch {
			if (request.signal.aborted) return null;

			// Preparation is an optimization. Any fetch, validation, or decode failure leaves the
			// destination's existing controller loader as the authoritative fallback.
			this.clear();
			return null;
		}
	};

	shouldRevalidate = ({ currentUrl, nextUrl }: { currentUrl: URL; nextUrl: URL }) =>
		normalizedURLIdentity(currentUrl) !== normalizedURLIdentity(nextUrl);

	stage(
		state: AdminPreparedRouteStateV1,
		expectedIdentity = normalizedURLIdentity(new URL(window.location.href))
	) {
		const identity = `${state.pathname}${state.search}`;
		if (
			state.version !== ADMIN_PREPARED_ROUTE_STATE_VERSION ||
			expectedIdentity !== identity ||
			state.fingerprint === "" ||
			(this.#contextKey !== "" && state.contextKey !== this.#contextKey) ||
			state.route === undefined ||
			!this.#canSeedRoute(state)
		) {
			this.clear();
			return false;
		}

		this.#routeData = { identity, data: state.route, loaders: state.loaders };
		this.#routeKey = routeStateKey(state);
		this.#adoptContext(state);
		return true;
	}

	commit(state: AdminPreparedRouteStateV1 | null | undefined, url: URL) {
		if (state == null) {
			this.clear();
			return false;
		}

		const identity = normalizedURLIdentity(url);
		if (`${state.pathname}${state.search}` !== identity) return false;
		if (this.#contextKey !== "" && state.contextKey !== this.#contextKey) return false;

		const navigation = state.navigation ?? state.runtime;
		if (navigation !== undefined) this.#runtime?.adoptPreparedNavigation(navigation);
		this.#adoptContext(state);
		if (state.outcome === "prepared") {
			return this.#routeKey === routeStateKey(state) || this.stage(state, identity);
		}

		this.clear();
		return false;
	}

	clear() {
		this.#routeKey = "";
		this.#routeData = undefined;
	}

	routeData(pathname: string, search = "") {
		const base = this.#basePath === "/" ? "" : this.#basePath;
		const url = new URL(`${base}${pathname}${search}`, window.location.origin);
		return this.#routeData?.identity === normalizedURLIdentity(url)
			? this.#routeData.data
			: undefined;
	}

	discardRouteData(state: AdminPreparedRouteStateV1) {
		if (this.#routeKey === routeStateKey(state)) this.clear();
	}

	loaderData(key: string, pathname: string, search = "") {
		return this.routeData(pathname, search) === undefined
			? undefined
			: this.#routeData?.loaders?.[key];
	}

	isRouteStaged(state: AdminPreparedRouteStateV1) {
		return this.#routeKey === routeStateKey(state);
	}

	async #fetchRouteState(url: URL, signal: AbortSignal) {
		const response = await fetch(url, {
			headers: {
				Accept: ADMIN_PREPARED_ROUTE_STATE_MEDIA_TYPE,
				"Ridu-Admin-Context": this.#contextKey,
			},
			credentials: "include",
			signal,
		});
		if (!response.ok) throw new Error(`Admin route preparation failed with ${response.status}.`);

		return validateAdminPreparedState(await response.json(), url, this.#contextKey);
	}

	async #prepareRestoredCreateDraft(state: AdminPreparedRouteStateV1, signal?: AbortSignal) {
		if (state.outcome !== "prepared" || state.route?.kind !== "collection-create") return state;

		const match = /^\/collections\/([^/]+)\/create(?:\/api)?$/.exec(
			this.#relativePath(state.pathname)
		);
		const manifest = state.runtime?.manifest ?? this.#runtime?.manifest;
		const navigation = state.navigation ?? state.runtime;
		const client = this.#runtime?.client;
		if (match === null || manifest === undefined || client === undefined) return state;

		const slug = decodeURIComponent(match[1]);
		const collection = manifest.collections.find((candidate) => candidate.slug === slug);
		if (collection === undefined) return state;
		const checkpoint = peekFormDraft(collection.id, undefined);
		if (checkpoint === undefined) return state;

		const defaults = state.route.create.values;
		const recovered = recoverFormDraft(
			{ values: defaults, original: defaults },
			{ values: checkpoint.values, original: checkpoint.original },
			checkpoint.collection.fields,
			collection.fields
		);
		if (samePreparedCreateValues(recovered.values, defaults)) {
			return state;
		}

		// Go checked access against its defaults. A local draft can change a
		// data-dependent rule, so settle access for the recovered values before reveal.
		let access: AdminCreateDataV1["access"];
		try {
			const value = await client.collectionAccess(slug, {
				data: recovered.values,
				locale: navigation.contentLocale,
				signal,
			});
			access = { value };
		} catch (cause) {
			if (!(cause instanceof RiduError)) throw cause;
			access = {
				error: {
					code: cause.code,
					status: cause.status,
					message: cause.message,
					issues: [...cause.issues],
					...(cause.requestId === undefined ? {} : { requestId: cause.requestId }),
					...(cause.details === undefined ? {} : { details: cause.details }),
				},
			};
		}

		return {
			...state,
			route: {
				...state.route,
				create: { values: recovered.values, access },
			},
		};
	}

	#adoptContext(state: AdminPreparedRouteStateV1) {
		if (state.contextKey === "") return;
		if (this.#contextKey !== "" && this.#contextKey !== state.contextKey) return;
		this.#contextKey = state.contextKey;
		document.documentElement.dataset.riduContext = state.contextKey;
	}

	#clearContext() {
		this.#contextKey = "";
		delete document.documentElement.dataset.riduContext;
	}

	#canSeedRoute(state: AdminPreparedRouteStateV1) {
		const config = this.#runtime?.config;
		const route = state.route;
		if (config === undefined || route === undefined) return route !== undefined;

		const selected = preparedCustomView(
			config,
			this.#relativePath(state.pathname),
			this.#runtime?.manifest
		);

		if (route.kind === "custom") {
			const loader = selected?.loader;
			return loader !== undefined && validLoaderSeed(loader, state.loaders);
		}

		// A core seed belongs to the core view, never to its custom replacement.
		if (selected !== undefined) return false;

		switch (route.kind) {
			case "dashboard": {
				const panels = config.extensions.dashboardPanels;
				const replacement = panels.find((panel) => panel.position === "replace");
				if (replacement !== undefined) {
					return (
						replacement.loader !== undefined && validLoaderSeed(replacement.loader, state.loaders)
					);
				}

				return panels.every(
					(panel) => panel.loader === undefined || validLoaderSeed(panel.loader, state.loaders)
				);
			}
			case "account":
			case "security":
				return !config.extensions.account.some((panel) => panel.position === "replace");
			case "login":
				return !config.extensions.login.some((panel) => panel.position === "replace");
			default:
				return true;
		}
	}

	#relativePath(pathname: string) {
		if (this.#basePath === "/") return pathname;
		if (pathname === this.#basePath) return "/";
		return pathname.startsWith(`${this.#basePath}/`)
			? pathname.slice(this.#basePath.length)
			: pathname;
	}
}

function validLoaderSeed(
	loader: AdminLoader<never, unknown>,
	results: AdminPreparedRouteStateV1["loaders"]
) {
	const result = results?.[loader.key];
	if (result === undefined) return false;
	// A structured read failure is complete data too: the view renders its error state.
	if (result.error !== undefined) return true;

	try {
		loader.decode(result.value);
		return true;
	} catch {
		return false;
	}
}

function routeStateKey(state: AdminPreparedRouteStateV1) {
	return `${state.pathname}${state.search}:${state.fingerprint}`;
}

// Create access depends on values, including a recovered local draft. Object
// key order is not data identity, and authored fields named "signal" are data.
export function samePreparedCreateValues(left: unknown, right: unknown): boolean {
	if (left === right) return true;
	if (Array.isArray(left) && Array.isArray(right))
		return (
			left.length === right.length &&
			left.every((value, index) => samePreparedCreateValues(value, right[index]))
		);
	if (!isRecord(left) || !isRecord(right)) return false;
	const keys = Object.keys(left);
	return (
		keys.length === Object.keys(right).length &&
		keys.every(
			(key) => Object.hasOwn(right, key) && samePreparedCreateValues(left[key], right[key])
		)
	);
}

function normalizeAdminBasePath(basePath: string) {
	return `/${basePath.split("/").filter(Boolean).join("/")}`;
}

function neverSettles(): Promise<never> {
	// A hard navigation transfers ownership to a new document, so the initiating handler must not
	// resume and mutate the outgoing application while that document is loading.
	return new Promise(() => {});
}

const [getAdminBootstrapCoordinator, setAdminBootstrapCoordinator] =
	createContext<AdminBootstrapCoordinator>();

export { getAdminBootstrapCoordinator, setAdminBootstrapCoordinator };
