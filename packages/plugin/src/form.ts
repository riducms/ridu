import type { ValidationIssue } from "@riducms/protocol";

export interface FieldFormResource {
	collection: string;
	id?: string;
	global?: boolean;
}

export interface FieldForm {
	readonly contentLocale?: string;
	readonly resource?: FieldFormResource;
	get(path: string): unknown;
	issuesFor(path: string): readonly ValidationIssue[];
	register(path: string): () => void;
	set(path: string, value: unknown): void;
	snapshot?(): Record<string, unknown>;
}
