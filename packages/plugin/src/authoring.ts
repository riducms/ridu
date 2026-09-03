import type { SchemaCollection, SchemaField } from "@riducms/protocol";
import type { Component } from "svelte";

export type FieldDocument = Record<string, unknown> & {
	id: string;
	createdAt?: string;
	updatedAt?: string;
	deletedAt?: string;
	_status?: "draft" | "published";
	_revision?: number;
	_localization?: {
		sources: Record<string, string>;
	};
};

export interface FieldReferenceFilter {
	field: string;
	operator:
		| "equals"
		| "notEquals"
		| "like"
		| "contains"
		| "greaterThan"
		| "greaterThanEqual"
		| "lessThan"
		| "lessThanEqual";
	value: string | number | boolean;
}

export interface FieldReferenceBrowserProps {
	open?: boolean;
	field: SchemaField;
	collection: SchemaCollection;
	hasMany: boolean;
	selectedIDs: readonly string[];
	readOnly?: boolean;
	initialDocument?: FieldDocument;
	initialDocumentID?: string;
	optionFilter?: FieldReferenceFilter | readonly FieldReferenceFilter[];
	defaultValues?: Readonly<Record<string, unknown>>;
	allowCreate?: boolean;
	locale?: string;
	onCommit: (ids: string[]) => void | boolean | Promise<void | boolean>;
	onClose: () => void;
}

export interface FieldAuthoringHost {
	readonly collections: readonly SchemaCollection[];
	readonly documentRevision: number;
	readonly locale?: string;
	referenceBrowser: Component<FieldReferenceBrowserProps>;
	findDocument(collection: string, id: string, signal?: AbortSignal): Promise<FieldDocument>;
	requestPlugin?<Result>(path: string, body: unknown, signal?: AbortSignal): Promise<Result>;
}
