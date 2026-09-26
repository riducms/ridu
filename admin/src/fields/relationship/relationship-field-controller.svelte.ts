import { documentLabel } from "@admin/features/documents/document-title";
import type { SchemaField } from "@riducms/protocol";

import type { AdminDocument } from "@admin/core/api/admin-client";
import type { FormController } from "@admin/core/forms/form-controller.svelte";
import type { AdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
import { localizeSchemaCollection } from "@admin/core/i18n/localized-schema";

import {
	selectedRelationshipIDs,
	relationshipReferences,
	relationshipKey,
	type PolymorphicReference,
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
	initialCreate = $state(false);
	initialFile = $state.raw<File>();
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
			const references = this.references;
			const locale = this.options.form.contentLocale;
			const revision = this.options.runtime.documentRevision;
			if (references.length === 0) {
				this.documents = {};
				this.hydrationError = undefined;
				return;
			}
			const request = new AbortController();
			this.#hydrateSelected(references, locale, revision, request.signal);
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

	get references() {
		return relationshipReferences(
			this.options.form.get(this.options.field.path),
			this.hasMany,
			this.polymorphic,
			this.targets[0]?.collectionSlug ?? ""
		);
	}

	get selections() {
		return this.references.map((reference) => ({
			...reference,
			key: relationshipKey(reference),
			label: this.label(reference.id, reference.relationTo),
			document: this.documents[relationshipKey(reference)],
		}));
	}

	setReferences = (references: readonly PolymorphicReference[]) => {
		if (this.options.field.admin.readOnly || this.options.form.editingBlocked) return;
		const values = references.map((reference) => (this.polymorphic ? reference : reference.id));
		this.options.form.set(
			this.options.field.path,
			this.hasMany ? values : (values[0] ?? (this.polymorphic ? null : ""))
		);
	};

	removeReference = (reference: PolymorphicReference) => {
		this.setReferences(
			this.references.filter(
				(candidate) => relationshipKey(candidate) !== relationshipKey(reference)
			)
		);
	};

	move = (from: number, to: number) => {
		const references = [...this.references];
		const [moved] = references.splice(from, 1);
		if (moved) references.splice(to, 0, moved);
		this.setReferences(references);
	};

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

	label = (id: string, target = this.selectedTarget) => {
		const document = this.documents[relationshipKey({ relationTo: target, id })];
		if (document === undefined) return id;
		const collection = this.options.runtime.manifest?.collections.find(
			(collection) => collection.slug === target
		);
		if (collection?.capabilities.upload) return String(document.filename ?? id);
		return documentLabel(collection, document);
	};

	openBrowser = (
		documentID?: string,
		create = false,
		target = this.selectedTarget,
		file?: File
	) => {
		this.selectedTarget = target;
		this.initialFile = file;
		this.initialCreate = create;
		this.initialDocumentID = documentID;
		this.initialDocument =
			documentID === undefined
				? undefined
				: this.documents[relationshipKey({ relationTo: target, id: documentID })];
		this.browserOpen = true;
	};

	setBrowserOpen = (open: boolean) => {
		this.browserOpen = open;
	};

	commit = (ids: string[]) => {
		const field = this.options.field;
		const form = this.options.form;
		if (field.admin.readOnly || form.editingBlocked) return;
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
		references: readonly PolymorphicReference[],
		locale: string | undefined,
		revision: number,
		signal: AbortSignal
	) {
		const results = await Promise.allSettled(
			references.map((reference) =>
				this.options.runtime.client.find(reference.relationTo, reference.id, {
					signal,
					locale,
				})
			)
		);
		if (signal.aborted || revision !== this.options.runtime.documentRevision) return;
		const documents: Record<string, AdminDocument> = {};
		let failed = false;
		for (let index = 0; index < results.length; index += 1) {
			const result = results[index];
			const reference = references[index];
			if (result?.status === "fulfilled" && reference !== undefined)
				documents[relationshipKey(reference)] = result.value;
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
