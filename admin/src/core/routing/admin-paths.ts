export const adminRoutePatterns = {
	home: "/",
	createFirstUser: "/create-first-user",
	login: "/login",
	forgotPassword: "/forgot-password",
	resetPassword: "/reset-password",
	requestVerification: "/request-verification",
	verifyEmail: "/verify-email",
	account: "/account",
	accountSecurity: "/account/security",
	collection: "/collections/:collection",
	collectionTrash: "/collections/:collection/trash",
	collectionUpload: "/collections/:collection/upload",
	createDocument: "/collections/:collection/create",
	createDocumentAPI: "/collections/:collection/create/api",
	document: "/collections/:collection/:document",
	documentAPI: "/collections/:collection/:document/api",
	documentVersions: "/collections/:collection/:document/versions",
	documentVersion: "/collections/:collection/:document/versions/:revision",
	global: "/globals/:global",
	globalAPI: "/globals/:global/api",
	globalVersions: "/globals/:global/versions",
	globalVersion: "/globals/:global/versions/:revision",
} as const;

const adminAuthPaths = new Set<string>([
	adminRoutePatterns.login,
	adminRoutePatterns.createFirstUser,
	adminRoutePatterns.forgotPassword,
	adminRoutePatterns.resetPassword,
	adminRoutePatterns.requestVerification,
	adminRoutePatterns.verifyEmail,
]);

export function adminLoginPath(requestedPath: string): string {
	const redirect = safeAdminRedirectPath(requestedPath);
	return redirect === adminRoutePatterns.home
		? adminRoutePatterns.login
		: `${adminRoutePatterns.login}?redirect=${encodeURIComponent(redirect)}`;
}

export function adminCreateFirstUserPath(): string {
	return adminRoutePatterns.createFirstUser;
}

export function adminRedirectFromSearch(search: string): string {
	return safeAdminRedirectPath(new URLSearchParams(search).get("redirect"));
}

function safeAdminRedirectPath(requestedPath: string | null | undefined): string {
	if (
		requestedPath === null ||
		requestedPath === undefined ||
		!requestedPath.startsWith("/") ||
		requestedPath.startsWith("//")
	) {
		return adminRoutePatterns.home;
	}
	try {
		const origin = "https://ridu.invalid";
		const target = new URL(requestedPath, origin);
		if (target.origin !== origin || adminAuthPaths.has(target.pathname)) {
			return adminRoutePatterns.home;
		}
		return `${target.pathname}${target.search}${target.hash}`;
	} catch {
		return adminRoutePatterns.home;
	}
}

export function collectionPath(collection: string): string {
	return `/collections/${encodeURIComponent(collection)}`;
}

export function globalPath(global: string): string {
	return `/globals/${encodeURIComponent(global)}`;
}

export function createDocumentPath(collection: string): string {
	return `${collectionPath(collection)}/create`;
}

export function createDocumentAPIPath(collection: string): string {
	return `${createDocumentPath(collection)}/api`;
}

export function collectionTrashPath(collection: string): string {
	return `${collectionPath(collection)}/trash`;
}

export function collectionUploadPath(collection: string): string {
	return `${collectionPath(collection)}/upload`;
}

export function documentPath(collection: string, document: string): string {
	return `${collectionPath(collection)}/${encodeURIComponent(document)}`;
}

export function documentIDFromAdminPath(pathname: string): string | undefined {
	const segments = pathname.split("/");
	if (segments[1] !== "collections" || segments[3] === undefined || segments[3] === "") {
		return undefined;
	}
	try {
		return decodeURIComponent(segments[3]);
	} catch {
		return undefined;
	}
}

export function documentAPIPath(collection: string, document: string): string {
	return `${documentPath(collection, document)}/api`;
}

export function documentVersionsPath(collection: string, document: string): string {
	return `${documentPath(collection, document)}/versions`;
}

export function documentVersionPath(
	collection: string,
	document: string,
	revision: number
): string {
	return `${documentVersionsPath(collection, document)}/${revision}`;
}

export function globalVersionsPath(global: string): string {
	return `${globalPath(global)}/versions`;
}

export function globalAPIPath(global: string): string {
	return `${globalPath(global)}/api`;
}

export function globalVersionPath(global: string, revision: number): string {
	return `${globalVersionsPath(global)}/${revision}`;
}
