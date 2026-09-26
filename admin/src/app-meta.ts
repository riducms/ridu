import type { SchemaManifest } from "@riducms/protocol";
import type { AdminI18n } from "@riducms/translations";

export const ADMIN_NAME = "Ridu";
export const ADMIN_TITLE = `${ADMIN_NAME} Admin`;

export function adminApplicationName(
	manifest: SchemaManifest | undefined,
	i18n: Pick<AdminI18n, "text">,
	fallback: string
): string {
	return manifest === undefined
		? fallback
		: i18n.text(manifest.application.name, manifest.application.nameTranslations);
}
