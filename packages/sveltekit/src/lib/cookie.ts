/** Frontend-domain cookie that holds a Ridu session token. */
export const DEFAULT_COOKIE_NAME = "ridu_token";

/** Read one cookie value from a `document.cookie` or `Cookie` header string. */
export function readCookie(header: string, name: string): string | null {
	for (const part of header.split(";")) {
		const separator = part.indexOf("=");
		if (separator < 0 || part.slice(0, separator).trim() !== name) continue;
		const value = part.slice(separator + 1).trim();
		if (value === "") return null;
		try {
			return decodeURIComponent(value);
		} catch {
			return null;
		}
	}
	return null;
}

export interface TokenCookieOptions {
	/** Absolute session expiry; the browser drops the cookie when the session ends. */
	expires: Date;
	/** Send only over HTTPS. Plain-HTTP development cannot keep a Secure cookie. */
	secure: boolean;
}

/**
 * Serialize the token cookie for `document.cookie`. It is deliberately readable by scripts, so the
 * browser client can send it directly to Ridu; SameSite=Lax keeps it off cross-site subrequests.
 */
export function tokenCookie(name: string, token: string, options: TokenCookieOptions): string {
	const attributes = [
		`${name}=${encodeURIComponent(token)}`,
		"Path=/",
		`Expires=${options.expires.toUTCString()}`,
		"SameSite=Lax",
	];
	if (options.secure) attributes.push("Secure");
	return attributes.join("; ");
}

/** Serialize an expired token cookie that removes the stored token. */
export function expiredTokenCookie(name: string, secure: boolean): string {
	const attributes = [`${name}=`, "Path=/", "Max-Age=0", "SameSite=Lax"];
	if (secure) attributes.push("Secure");
	return attributes.join("; ");
}
