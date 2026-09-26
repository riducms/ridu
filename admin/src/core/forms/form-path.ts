import { isRecord } from "@riducms/protocol";

// Form paths address concrete row indexes as well as object fields. Document
// display paths deliberately stop at arrays; keep those semantics separate.
export function readFormPath(values: unknown, path: string): unknown {
	let current = values;
	for (const segment of path.split(".")) {
		if (Array.isArray(current)) current = current[Number(segment)];
		else if (isRecord(current)) current = current[segment];
		else return undefined;
	}
	return current;
}

export function joinFormPath(parent: string, child: string) {
	return parent === "" ? child : `${parent}.${child}`;
}
