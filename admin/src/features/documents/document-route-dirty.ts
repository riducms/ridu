export interface DocumentRouteDirtyInput {
	documentValuesDirty: boolean;
	creatingAuthUser: boolean;
	password: string;
	passwordConfirmation: string;
}

export interface AuthCreateCredentials {
	password: string;
	passwordConfirmation: string;
}

export function documentRouteIsDirty({
	documentValuesDirty,
	creatingAuthUser,
	password,
	passwordConfirmation,
}: DocumentRouteDirtyInput) {
	return (
		documentValuesDirty ||
		(creatingAuthUser && (password.length > 0 || passwordConfirmation.length > 0))
	);
}

export async function saveAuthCreateWithClearedCredentials(
	credentials: AuthCreateCredentials,
	replaceCredentials: (next: AuthCreateCredentials) => void,
	save: (password: string) => Promise<boolean>
) {
	// The document controller performs the successful create redirect before its
	// save promise resolves. Clear route-owned credentials first so the route's
	// unsaved-change blocker permits that redirect, then restore them verbatim if
	// the save does not complete.
	replaceCredentials({ password: "", passwordConfirmation: "" });
	try {
		const saved = await save(credentials.password);
		if (!saved) replaceCredentials(credentials);
		return saved;
	} catch (cause) {
		replaceCredentials(credentials);
		throw cause;
	}
}
