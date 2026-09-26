import {
	isRecord,
	ADMIN_PREPARED_ROUTE_STATE_VERSION,
	isAdminPreparedRouteData,
	isAdminLoaderResults,
	type AdminPreparedRouteStateV1,
	type AdminPreparedRouteDataV1,
} from "@riducms/protocol";

const templateID = "ridu-admin-initial-state";

// Cold HTML and navigation JSON cross the same wire boundary. Validate them
// here, before the coordinator exposes anything to the runtime or controllers.
export function readEmbeddedAdminState(): AdminPreparedRouteStateV1 | undefined {
	const template = document.getElementById(templateID);
	if (!(template instanceof HTMLTemplateElement)) return undefined;

	try {
		return validateAdminPreparedState(
			JSON.parse(template.content.textContent ?? ""),
			new URL(location.href)
		);
	} catch {
		// Embedded state is optional; malformed or stale data falls back to the SDK bootstrap.
		return undefined;
	} finally {
		template.remove();
	}
}

export function validateAdminPreparedState(
	value: unknown,
	url: URL,
	contextKey?: string
): AdminPreparedRouteStateV1 {
	if (!isRecord(value)) throw new Error("Admin prepared state must be an object.");
	if (value.version !== ADMIN_PREPARED_ROUTE_STATE_VERSION)
		throw new Error("Admin prepared state version is incompatible.");

	if (typeof value.pathname !== "string" || typeof value.search !== "string")
		throw new Error("Admin prepared route identity is invalid.");
	if (`${value.pathname}${value.search}` !== normalizedURLIdentity(url))
		throw new Error("Admin prepared route identity is stale.");

	if (
		typeof value.contextKey !== "string" ||
		(value.contextKey !== "" && !/^[0-9a-f]{32}$/.test(value.contextKey))
	)
		throw new Error("Admin prepared context key is invalid.");
	if (
		typeof value.fingerprint !== "string" ||
		(value.fingerprint !== "" && !/^[0-9a-f]{64}$/.test(value.fingerprint))
	)
		throw new Error("Admin prepared fingerprint is invalid.");
	if (
		typeof value.buildId !== "string" ||
		(value.buildId !== "" && !/^[0-9a-f]{24}$/.test(value.buildId))
	)
		throw new Error("Admin prepared build ID is invalid.");
	if (
		!Array.isArray(value.moduleGroups) ||
		!value.moduleGroups.every((group) => typeof group === "string")
	)
		throw new Error("Admin prepared module groups are invalid.");

	const expectedBuildID = document
		.querySelector<HTMLMetaElement>('meta[name="ridu-admin-build-id"]')
		?.getAttribute("content");
	if (
		value.outcome !== "reload" &&
		// An empty navigation context requires a fresh document, including its build.
		contextKey !== "" &&
		expectedBuildID != null &&
		value.buildId !== expectedBuildID
	)
		throw new Error("Admin prepared state build is stale.");

	if (
		value.loaders !== undefined &&
		(value.outcome !== "prepared" || !isAdminLoaderResults(value.loaders))
	)
		throw new Error("Admin loader results are invalid.");

	// Each outcome has a different payload. Keep those requirements separate so
	// adding a field to one outcome cannot silently relax another's validation.
	switch (value.outcome) {
		case "prepared":
			if (
				!isAdminPreparedRouteData(value.route) ||
				(value.runtime === undefined && value.navigation === undefined) ||
				!validPreparedModuleGroups(value.moduleGroups, value.route)
			)
				throw new Error("Admin prepared route data is invalid.");
			break;
		case "redirect":
			if (typeof value.location !== "string")
				throw new Error("Admin prepared redirect location is invalid.");
			break;
		case "reload":
			if (
				value.runtime !== undefined ||
				value.navigation !== undefined ||
				value.route !== undefined
			)
				throw new Error("Admin reload must not carry runtime or route data.");
			break;
		case "fallback":
			if (value.location !== undefined || !validDiagnostic(value.diagnostic))
				throw new Error("Admin fallback requires a diagnostic and no redirect.");
			break;
		default:
			throw new Error("Admin prepared state outcome is invalid.");
	}

	if (value.runtime !== undefined && !validPreparedRuntime(value.runtime))
		throw new Error("Admin prepared runtime is invalid.");

	// Compact navigation can reuse a runtime, but can never establish one.
	if (
		value.navigation !== undefined &&
		(value.runtime !== undefined ||
			!contextKey ||
			value.contextKey !== contextKey ||
			!validPreparedNavigation(value.navigation))
	)
		throw new Error("Admin prepared navigation requires the matching runtime context.");

	return value as unknown as AdminPreparedRouteStateV1;
}

function validPreparedModuleGroups(groups: string[], route: AdminPreparedRouteDataV1) {
	const allowed = new Set([
		"entry",
		"document",
		"date",
		"upload-preview",
		"bulk-upload",
		"document-api",
		"versions",
	]);
	if (
		groups.some((group) => !allowed.has(group)) ||
		new Set(groups).size !== groups.length ||
		!groups.includes("entry")
	)
		return false;

	switch (route.kind) {
		case "collection-create":
		case "collection-document":
		case "global-document":
			return groups.includes("document");
		case "collection-api":
		case "global-api":
			return groups.includes("document") && groups.includes("document-api");
		case "collection-versions":
		case "global-versions":
			return groups.includes("versions");
		case "upload":
			return groups.includes("bulk-upload");
		case "security":
			return groups.includes("date");
		default:
			return true;
	}
}

function validPreparedNavigation(value: unknown) {
	return (
		isRecord(value) &&
		isRecord(value.collectionOperations) &&
		isRecord(value.globalOperations) &&
		(value.contentLocale === undefined || typeof value.contentLocale === "string")
	);
}

function validPreparedRuntime(value: unknown) {
	if (!isRecord(value) || !isRecord(value.manifest) || !isRecord(value.manifest.application))
		return false;

	if (
		!Array.isArray(value.manifest.collections) ||
		(value.manifest.globals !== undefined && !Array.isArray(value.manifest.globals)) ||
		!Array.isArray(value.manifest.plugins) ||
		typeof value.authBootstrapAvailable !== "boolean" ||
		!validPreparedNavigation(value) ||
		!isRecord(value.preferences) ||
		!["system", "light", "dark"].includes(String(value.theme))
	)
		return false;

	if (value.session === undefined) return true;
	return (
		isRecord(value.session) &&
		typeof value.session.id === "string" &&
		typeof value.session.collection === "string" &&
		isRecord(value.session.user)
	);
}

function validDiagnostic(value: unknown) {
	return isRecord(value) && typeof value.code === "string" && typeof value.message === "string";
}

export function normalizedURLIdentity(url: Pick<URL, "pathname" | "search">) {
	const parameters = new URLSearchParams(url.search);
	// Stable key sorting matches Go's identity and preserves repeated-value order.
	parameters.sort();

	const search = parameters.size === 0 ? "" : `?${parameters}`;
	const pathname = url.pathname.length > 1 ? url.pathname.replace(/\/$/, "") : url.pathname;
	return pathname + search;
}
