import type { SchemaField } from "@riducms/protocol";

import type { AdminDocument } from "@admin/core/api/admin-client";
import type { FormController } from "@admin/core/forms/form-controller.svelte";
import type { AdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
import { localizeSchemaCollection } from "@admin/core/i18n/localized-schema";

import {
	selectedRelationshipIDs,
	updateRelationshipValue,
} from "@admin/fields/relationship/relationship-value";

interface RelationshipFieldControllerOptions {
	runtime: AdminRuntime;
	get field(): SchemaField;
	get form(): FormController;
}

export class RelationshipFieldController {
	selectedTarget = $state("");
	documents = $state.raw<Record<string, AdminDocument>>({});
	hydrationError = $state<string>();
	browserOpen = $state(false);
	initialDocument = $state.raw<AdminDocument>();
	initialDocumentID = $state<string>();
	#formRevision = 0;

	constructor(readonly options: RelationshipFieldControllerOptions) {
		this.selectedTarget =
			selectedRelationshipTarget(options.form.get(options.field.path)) ??
			relationshipTargets(options.field)[0]?.collectionSlug ??
			"";

		$effect(() => this.options.form.register(this.options.field.path));

		$effect(() => {
			const revision = this.options.form.revision;
			if (revision === this.#formRevision) return;
			this.#formRevision = revision;
			const target = selectedRelationshipTarget(this.options.form.get(this.options.field.path));
			if (
				target !== undefined &&
				this.targets.some((candidate) => candidate.collectionSlug === target)
			) {
				this.selectedTarget = target;
			}
		});

		$effect(() => {
			if (this.targets.some((target) => target.collectionSlug === this.selectedTarget)) return;
			this.selectedTarget = this.targets[0]?.collectionSlug ?? "";
		});

		$effect(() => {
			const slug = this.currentTarget?.collectionSlug;
			const ids = [...this.selectedIDs];
			const revision = this.options.runtime.documentRevision;
			if (slug === undefined || ids.length === 0) {
				this.documents = {};
				this.hydrationError = undefined;
				return;
			}
			const request = new AbortController();
			this.#hydrateSelected(slug, ids, revision, request.signal);
			return () => request.abort();
		});
	}

	get targets() {
		return relationshipTargets(this.options.field);
	}

	get hasMany() {
		const field = this.options.field;
		return field.relationship?.hasMany ?? field.upload?.hasMany ?? false;
	}

	get polymorphic() {
		return this.options.field.relationship?.polymorphic === true;
	}

	get currentTarget() {
		return (
			this.targets.find((target) => target.collectionSlug === this.selectedTarget) ??
			this.targets[0]
		);
	}

	get targetCollection() {
		const collection = this.options.runtime.manifest?.collections.find(
			(collection) => collection.slug === this.currentTarget?.collectionSlug
		);
		return collection === undefined
			? undefined
			: localizeSchemaCollection(collection, this.options.runtime.i18n);
	}

	get selectedIDs() {
		return selectedRelationshipIDs(
			this.options.form.get(this.options.field.path),
			this.hasMany,
			this.polymorphic,
			this.selectedTarget
		);
	}

	get selectedID() {
		return this.selectedIDs[0];
	}

	get issues() {
		return this.options.form.issuesFor(this.options.field.path);
	}

	selectTarget = (target: string) => {
		this.selectedTarget = target;
	};

	label = (id: string) => {
		const document = this.documents[id];
		if (document === undefined) return id;
		if (this.targetCollection?.capabilities.upload) return String(document.filename ?? id);
		const titleField = findDisplayField(this.targetCollection?.fields ?? []);
		return String((titleField === undefined ? undefined : document[titleField.name]) ?? id);
	};

	initials = (id: string) =>
		this.label(id)
			.trim()
			.split(/\s+/)
			.filter(Boolean)
			.slice(0, 2)
			.map((part) => part[0]?.toLocaleUpperCase(this.options.runtime.i18n.language) ?? "")
			.join("");

	mediaURL = (document: AdminDocument | undefined) =>
		typeof document?.url === "string" ? document.url : undefined;

	isImage = (document: AdminDocument | undefined) =>
		typeof document?.mimeType === "string" && document.mimeType.startsWith("image/");

	openBrowser = (documentID?: string) => {
		this.initialDocumentID = documentID;
		this.initialDocument = documentID === undefined ? undefined : this.documents[documentID];
		this.browserOpen = true;
	};

	setBrowserOpen = (open: boolean) => {
		this.browserOpen = open;
	};

	commit = (ids: string[]) => {
		const field = this.options.field;
		const form = this.options.form;
		form.set(
			field.path,
			updateRelationshipValue(
				form.get(field.path),
				ids,
				this.hasMany,
				this.polymorphic,
				this.selectedTarget
			)
		);
	};

	remove = (id: string) => {
		this.commit(this.selectedIDs.filter((candidate) => candidate !== id));
	};

	async #hydrateSelected(
		slug: string,
		ids: readonly string[],
		revision: number,
		signal: AbortSignal
	) {
		const results = await Promise.allSettled(
			ids.map((id) =>
				this.options.runtime.client.find(slug, id, {
					signal,
					locale: this.options.form.contentLocale,
				})
			)
		);
		if (signal.aborted || revision !== this.options.runtime.documentRevision) return;
		const documents: Record<string, AdminDocument> = {};
		let failed = false;
		for (let index = 0; index < results.length; index += 1) {
			const result = results[index];
			const id = ids[index];
			if (result?.status === "fulfilled" && id !== undefined) documents[id] = result.value;
			else failed = true;
		}
		this.documents = documents;
		this.hydrationError = failed
			? this.options.runtime.i18n.t("errors:selectedDocumentsLoad")
			: undefined;
	}
}

function selectedRelationshipTarget(value: unknown): string | undefined {
	if (typeof value === "object" && value !== null && !Array.isArray(value)) {
		const target = (value as Record<string, unknown>).relationTo;
		return typeof target === "string" ? target : undefined;
	}
	if (Array.isArray(value)) {
		for (const candidate of value) {
			const target: string | undefined = selectedRelationshipTarget(candidate);
			if (target !== undefined) return target;
		}
	}
	return undefined;
}

function relationshipTargets(field: SchemaField) {
	if (field.upload !== undefined) {
		return [
			{
				collectionId: field.upload.collectionId,
				collectionSlug: field.upload.collectionSlug,
			},
		];
	}
	if ((field.relationship?.targets?.length ?? 0) > 0) return field.relationship?.targets ?? [];
	if (
		field.relationship?.collectionId !== undefined &&
		field.relationship.collectionSlug !== undefined
	) {
		return [
			{
				collectionId: field.relationship.collectionId,
				collectionSlug: field.relationship.collectionSlug,
			},
		];
	}
	return [];
}

function findDisplayField(fields: readonly SchemaField[]) {
	return (
		fields.find((field) => field.name === "title" || field.name === "name") ??
		fields.find((field) => field.type === "text" && field.name !== "email") ??
		fields.find((field) => field.type === "text" || field.type === "email")
	);
}
