import { isRecord } from "@riducms/protocol";

/** Read a document/group path without treating arrays as nested records. */
export function readDocumentPath(value: unknown, path: string) {
	let current = value;
	for (const segment of path.split(".")) {
		if (!isRecord(current)) return undefined;
		current = current[segment];
	}
	return current;
}
