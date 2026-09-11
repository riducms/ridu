import {
	LiveValidationController,
	type LiveValidationTransport,
} from "@admin/core/forms/live-validation.svelte";
import type { LiveValidationRequest } from "@riducms/protocol";
import { embeddedOccurrences, type EmbeddedOccurrence } from "@admin/core/forms/embedded-fields";
import {
	correlateEmbeddedIssues,
	correlateFormIssues,
	indexFieldValues,
	canonicalIssueTarget,
} from "@admin/core/forms/form-issue-correlation";
import type { AccessCapabilitiesEnvelope, ValidationIssue } from "@riducms/protocol";
import type { SchemaField } from "@riducms/protocol";
import type { AdminI18n } from "@riducms/plugin";
import { RiduError } from "@riducms/sdk";
import { createAdminI18n } from "@riducms/translations";

import {
	cloneFormValues,
	cloneFormValue,
	initialFormValues,
	localizationSource,
	reconcileFormSchema,
	recoverFormDraft,
	shouldSubmitLocalizedPath,
	type DetachedDraftValue,
	type FormValues,
	submissionFormValues,
} from "@admin/core/forms/form-schema";
import { validateFormValues, unknownBlockIssues } from "@admin/core/forms/form-validation";

export type { DetachedDraftValue, FormValues } from "@admin/core/forms/form-schema";

export interface FormReconciliationResult {
	detached: DetachedDraftValue[];
	restoredFields: number;
}

export interface FormResource {
	collection: string;
	id?: string;
	global?: boolean;
}

export interface DerivedTextBinding {
	follow(): void;
	setManual(value: string): void;
}

interface DerivedTextBindingState {
	sourcePath: string;
	following: boolean;
	derive: (source: unknown) => string;
	stop: () => void;
}

export class FormValidationError extends Error {
	constructor(
		readonly issues: readonly ValidationIssue[],
		i18n: AdminI18n = createAdminI18n()
	) {
		super(i18n.t("errors:correctInvalidFields"));
		this.name = "FormValidationError";
	}
}

export class FormController {
	readonly liveValidation = new LiveValidationController(this);
	#changeObservers = new Set<() => void>();

	configureLiveValidation(transport: LiveValidationTransport) {
		this.liveValidation.configure(transport);
	}
	requestLiveValidation(input: LiveValidationRequest, signal: AbortSignal) {
		return this.liveValidation.request(input, signal);
	}
	observeChanges(observer: () => void) {
		this.#changeObservers.add(observer);
		return () => {
			this.#changeObservers.delete(observer);
		};
	}
	/** A detached child also depends on unsaved values in each enclosing document scope. */
	refreshLiveValidation() {
		this.liveValidation.changed();
		for (const observer of [...this.#changeObservers]) observer();
	}
	get enclosingLiveInputAvailable(): boolean {
		return this.#readContext instanceof FormController
			? this.#readContext.liveValidation.inputAvailable
			: true;
	}
	setLiveInputUnavailable(path: string) {
		const release = this.liveValidation.inputUnavailable(path);
		for (const observer of [...this.#changeObservers]) observer();
		return () => {
			if (release()) this.refreshLiveValidation();
		};
	}
	/** Submit exact-locale edits with the same omission rules as Save, without client validation. */
	liveValidationData() {
		return submissionFormValues(this.#fields, this.values, (path, canonicalPath) =>
			this.includesLiveValidation(path, canonicalPath)
		);
	}
	includesLiveValidation(path: string, canonicalPath: string) {
		return (
			this.canRead(path, canonicalPath) &&
			this.canWrite(path, canonicalPath) &&
			shouldSubmitLocalizedPath(
				path,
				this.values,
				this.original,
				this.contentLocale,
				this.localizationSources,
				this.#fields
			)
		);
	}

	editorEpoch = $state(0);
	#editorScope: (() => boolean) | undefined;
	/** Detached scopes consult their owner synchronously when a capability is used. */
	setEditorScope(isActive: () => boolean) {
		this.#editorScope = isActive;
	}
	get editorScopeActive() {
		return this.#editorScope?.() ?? true;
	}
	#editorLifetimes = new Set<() => void>();
	registerEditorLifetime(invalidate: () => void) {
		this.#editorLifetimes.add(invalidate);
		return () => {
			this.#editorLifetimes.delete(invalidate);
		};
	}
	#invalidateEditors() {
		this.#invalidateValueIndexes();
		this.liveValidation.reset();
		this.#listEdits.clear();
		this.editorEpoch += 1;
		for (const invalidate of [...this.#editorLifetimes]) invalidate();
	}

	values = $state<FormValues>({});
	original = $state<FormValues>({});
	issues = $state<ValidationIssue[]>([]);
	#submittingIssues = $state.raw<readonly ValidationIssue[]>([]);
	submitting = $state(false);
	revision = $state(0);
	access = $state.raw<AccessCapabilitiesEnvelope>();
	accessMode = $state<"create" | "update">("update");
	#writeBlocked = $state(false);
	get writeBlocked() {
		return this.#writeBlocked;
	}
	set writeBlocked(value: boolean) {
		if (this.#writeBlocked === value) return;
		this.#writeBlocked = value;
		this.refreshLiveValidation();
	}
	resource = $state.raw<FormResource>();
	contentLocale = $state<string>();
	localizationSources = $state.raw<Readonly<Record<string, string>>>({});
	#registered = new Map<string, Set<symbol>>();
	#rowMountKeys = new WeakMap<object, symbol>();
	#valueEpoch = $state(0);
	#rowIndexes = new Map<string, Map<string, { index: number; variant: unknown }>>();
	#embeddedIndexes = new Map<
		string,
		{
			result: ReturnType<typeof embeddedOccurrences>;
			identities: Map<string, EmbeddedOccurrence | undefined>;
		}
	>();
	#pathObservers = new Map<string, Set<(value: unknown) => void>>();
	#derivedTextBindings = new Map<string, DerivedTextBindingState>();
	#revision = 0;
	// One edit marker per primitive list, never per item. Even equal duplicate reorders
	// invalidate positional feedback from an earlier submitted list.
	#listEdits = new Map<string, number>();
	#nextListEdit = 0;
	#fields: readonly SchemaField[] = [];
	#i18n: AdminI18n;
	#readContext: Pick<FormController, "get" | "snapshot"> | undefined;
	#pendingEdits = $state.raw<ReadonlyMap<symbol, () => readonly ValidationIssue[]>>(new Map());

	constructor(
		values: FormValues = {},
		i18n: AdminI18n = createAdminI18n(),
		readContext?: Pick<FormController, "get" | "snapshot">
	) {
		this.#i18n = i18n;
		this.#readContext = readContext;
		this.reset(values);
	}

	/** Private admin host traversal; never passed to public plugin components. */
	get schemaFields() {
		return this.#fields;
	}
	disposeBindings() {
		this.liveValidation.dispose();
		// Logical revocation may be discovered by a derived read. This only runs lease cleanup.
		for (const invalidate of [...this.#editorLifetimes]) invalidate();
	}

	get dirty() {
		return (
			this.pendingEditIssues().length > 0 ||
			JSON.stringify(this.values) !== JSON.stringify(this.original)
		);
	}

	/** Transient host-owned editing lock; unlike schema read-only, this is not presentation metadata. */
	get editingBlocked() {
		return this.submitting;
	}

	/** Parent-owned detached edits participate in saving and navigation protection. */
	registerPendingEdit(issues: () => readonly ValidationIssue[]) {
		const token = Symbol("scoped-edit");
		this.#pendingEdits = new Map(this.#pendingEdits).set(token, issues);
		return () => {
			const remaining = new Map(this.#pendingEdits);
			remaining.delete(token);
			this.#pendingEdits = remaining;
		};
	}

	pendingEditIssues() {
		return [...this.#pendingEdits.values()].flatMap((issues) => issues());
	}

	register(path: string) {
		const token = Symbol(path);
		const registrations = this.#registered.get(path) ?? new Set<symbol>();
		registrations.add(token);
		this.#registered.set(path, registrations);
		return () => {
			registrations.delete(token);
			if (registrations.size === 0 && this.#registered.get(path) === registrations)
				this.#registered.delete(path);
		};
	}

	isRegistered(path: string) {
		return this.#registered.has(path);
	}

	observe(path: string, observer: (value: unknown) => void) {
		const observers = this.#pathObservers.get(path) ?? new Set<(value: unknown) => void>();
		observers.add(observer);
		this.#pathObservers.set(path, observers);
		return () => {
			observers.delete(observer);
			if (observers.size === 0) this.#pathObservers.delete(path);
		};
	}

	bindDerivedText(
		path: string,
		sourcePath: string,
		derive: (source: unknown) => string,
		follows: (current: unknown, source: unknown) => boolean
	): DerivedTextBinding {
		let binding = this.#derivedTextBindings.get(path);
		if (binding !== undefined && binding.sourcePath !== sourcePath) {
			binding.stop();
			this.#derivedTextBindings.delete(path);
			binding = undefined;
		}
		if (binding === undefined) {
			const source = this.get(sourcePath);
			const state: DerivedTextBindingState = {
				sourcePath,
				following: follows(this.get(path), source),
				derive,
				stop: () => undefined,
			};
			state.stop = this.observe(sourcePath, (nextSource) => {
				if (state.following) this.#setDerivedText(path, state.derive(nextSource));
			});
			this.#derivedTextBindings.set(path, state);
			binding = state;
			if (state.following) this.#setDerivedText(path, state.derive(source));
		}
		const active = binding;
		return {
			follow: () => {
				active.following = true;
				this.#setDerivedText(path, active.derive(this.get(active.sourcePath)));
			},
			setManual: (value) => {
				active.following = false;
				this.set(path, value);
			},
		};
	}

	get(path: string): unknown {
		if (this.#readContext !== undefined && !Object.hasOwn(this.values, path.split(".")[0] ?? ""))
			return this.#readContext.get(path);
		return readPath(this.values, path);
	}

	/** Shared identity lookups: a mounted header never scans every sibling on its own. */
	rowPath(path: string, identity: string, variant: unknown) {
		this.#valueEpoch;
		let index = this.#rowIndexes.get(path);
		if (index === undefined) {
			index = new Map();
			const rows = this.get(path);
			if (Array.isArray(rows))
				for (const [position, row] of rows.entries()) {
					if (!isRecord(row) || typeof row._key !== "string") continue;
					index.set(row._key, {
						index: index.has(row._key) ? -1 : position,
						variant: row.blockType,
					});
				}
			this.#rowIndexes.set(path, index);
		}
		const match = index.get(identity);
		return match !== undefined && match.index >= 0 && match.variant === variant
			? `${path}.${match.index}`
			: undefined;
	}

	/** Cache only structural traversal, never access decisions or detached field values. */
	embeddedFields(field: SchemaField) {
		this.#valueEpoch;
		const key = field.path;
		let entry = this.#embeddedIndexes.get(key);
		if (entry === undefined) {
			const result = embeddedOccurrences(field, this.get(field.path), field.path);
			const identities = new Map<string, EmbeddedOccurrence | undefined>();
			for (const occurrence of result.occurrences) {
				const identity = JSON.stringify([occurrence.tree.key, occurrence.identity]);
				identities.set(identity, identities.has(identity) ? undefined : occurrence);
			}
			entry = { result, identities };
			this.#embeddedIndexes.set(key, entry);
		}
		return entry.result;
	}

	embeddedOccurrence(field: SchemaField, treeKey: string, identity: string) {
		this.embeddedFields(field);
		return this.#embeddedIndexes
			.get(field.path)
			?.identities.get(JSON.stringify([treeKey, identity]));
	}

	#invalidateValueIndexes() {
		this.#rowIndexes.clear();
		this.#embeddedIndexes.clear();
		this.#valueEpoch++;
	}

	/** DOM identity survives retained-row replacements, but not removal and key reuse. */
	rowMountKey(row: Record<string, unknown>) {
		const key = this.#rowMountKeys.get(row) ?? Symbol();
		this.#rowMountKeys.set(row, key);
		return key;
	}

	snapshot(): FormValues {
		return { ...this.#readContext?.snapshot(), ...cloneFormValues(this.values) };
	}

	set(path: string, value: unknown) {
		if (this.#readContext !== undefined && !Object.hasOwn(this.values, path.split(".")[0] ?? ""))
			throw new Error("Only fields in this scoped draft can be edited.");
		const previous = this.get(path);
		const [name] = path.split(".");
		const root =
			typeof previous === "object" ||
			typeof value === "object" ||
			/\.(?:_key|blockType)$/.test(path)
				? this.#fields.find((field) => field.name === name)
				: undefined;
		// Use the same schema identities as issue/access correlation, including embedded payloads.
		const listValues = new Map(
			(root ? indexFieldValues([root], this.values) : []).flatMap(({ token, schema, value }) =>
				schema?.type === "text-list" || schema?.type === "number-list"
					? [[token, JSON.stringify(value)] as const]
					: []
			)
		);
		const mounts = new Map<string, { row: Record<string, unknown>; key: symbol }>();
		if (root)
			for (const { token, value: row } of indexFieldValues([root], this.values)) {
				if (!isRecord(row)) continue;
				const key = this.#rowMountKeys.get(row);
				if (key) mounts.set(token, { row, key });
			}
		writePath(this.values, path, value);
		this.#invalidateValueIndexes();
		if (root) {
			const retained = new Set<string>();
			for (const { token, schema, path: fieldPath, value: current } of indexFieldValues(
				[root],
				this.values
			)) {
				if (schema?.type !== "text-list" && schema?.type !== "number-list") continue;
				retained.add(token);
				if (fieldPath === path || listValues.get(token) !== JSON.stringify(current))
					this.#listEdits.set(token, ++this.#nextListEdit);
			}
			for (const token of listValues.keys())
				if (!retained.has(token)) this.#listEdits.delete(token);
		}
		if (root && mounts.size) {
			for (const { row } of mounts.values()) this.#rowMountKeys.delete(row);
			for (const { token, value: row } of indexFieldValues([root], this.values)) {
				const mount = mounts.get(token);
				if (mount && isRecord(row)) this.#rowMountKeys.set(row, mount.key);
			}
		}
		this.issues = this.issues.filter(
			(issue) => issue.path !== path && !issue.path.startsWith(`${path}.`)
		);
		this.#submittingIssues = this.#submittingIssues.filter(
			(issue) => issue.path !== path && !issue.path.startsWith(`${path}.`)
		);
		for (const [observedPath, observers] of this.#pathObservers) {
			if (
				observedPath !== path &&
				!observedPath.startsWith(`${path}.`) &&
				!path.startsWith(`${observedPath}.`)
			) {
				continue;
			}
			const observedValue = this.get(observedPath);
			for (const observer of [...observers]) observer(observedValue);
		}
		this.liveValidation.changed(path);
		for (const observer of [...this.#changeObservers]) observer();
	}

	issuesFor(path: string) {
		const issues = new Map<string, ValidationIssue>();
		for (const issue of [
			...this.issues,
			...this.#submittingIssues,
			...this.liveValidation.issues,
		]) {
			if (issue.path !== path && !issue.path.startsWith(`${path}.`)) continue;
			const key = JSON.stringify([issue.path, issue.code, issue.message]);
			if (!issues.has(key)) issues.set(key, issue);
		}
		return [...issues.values()];
	}

	setAccess(access: AccessCapabilitiesEnvelope | undefined, mode: "create" | "update") {
		this.access = access;
		this.accessMode = mode;
		this.refreshLiveValidation();
	}

	setResource(resource: FormResource | undefined) {
		if (JSON.stringify(this.resource) !== JSON.stringify(resource)) this.#invalidateEditors();
		this.resource = resource;
	}

	setLocalization(locale: string | undefined, sources: Readonly<Record<string, string>> = {}) {
		if (this.contentLocale !== locale) this.#invalidateEditors();
		this.contentLocale = locale;
		this.localizationSources = sources;
	}

	localizationSource(path: string) {
		return localizationSource(
			path,
			this.values,
			this.original,
			this.localizationSources,
			this.#fields
		);
	}

	isInherited(path: string) {
		const source = this.localizationSource(path);
		return source !== undefined && source !== this.contentLocale;
	}

	fieldCapabilities(path: string, canonicalPath = authoredPath(path)) {
		const access = this.access;
		if (access === undefined) return undefined;
		return access.fields[path] ?? access.fields[canonicalPath] ?? access.fields[authoredPath(path)];
	}

	canRead(path: string, canonicalPath?: string) {
		return this.fieldCapabilities(path, canonicalPath)?.read ?? true;
	}

	canWrite(path: string, canonicalPath?: string) {
		if (this.writeBlocked) return false;
		const access = this.access;
		if (access === undefined) return true;
		const field = this.fieldCapabilities(path, canonicalPath);
		return this.accessMode === "create"
			? (field?.create ?? access.operations.create)
			: (field?.update ?? access.operations.update);
	}

	/** Keep occurrence capabilities attached to retained rows during structural edits. */
	setRows(path: string, rows: Record<string, unknown>[]) {
		const issues = this.issues.filter(
			(issue) => issue.path === path || issue.path.startsWith(`${path}.`)
		);
		const submitted = issues.length === 0 ? undefined : this.snapshot();
		const access = this.access;
		if (access !== undefined) {
			const previous = this.get(path);
			const before = Array.isArray(previous) ? previous : [];
			const oldIndexes = uniqueRowIndexes(before);
			const newIndexes = uniqueRowIndexes(rows);
			const fields: AccessCapabilitiesEnvelope["fields"] = {};
			const prefix = `${path}.`;
			for (const [candidate, capability] of Object.entries(access.fields)) {
				if (!isIndexedChildPath(path, candidate)) {
					fields[candidate] = capability;
					continue;
				}
				const index = candidate.slice(prefix.length).split(".", 1)[0]!;
				const oldRow = before[Number(index)];
				if (!isRecord(oldRow) || typeof oldRow._key !== "string") continue;
				const nextIndex = newIndexes.get(oldRow._key);
				if (
					oldIndexes.get(oldRow._key) !== Number(index) ||
					nextIndex === undefined ||
					nextIndex < 0
				)
					continue;
				// Replacing a variant is a new occurrence even if a caller reuses its key.
				if (rows[nextIndex]?.blockType !== oldRow.blockType) continue;
				fields[`${prefix}${nextIndex}${candidate.slice(prefix.length + index.length)}`] =
					capability;
			}
			this.access = { ...access, fields };
		}
		this.set(path, rows);
		if (submitted !== undefined)
			this.issues = [
				...this.issues,
				...correlateFormIssues(this.#fields, submitted, this.values, issues),
			];
	}

	/** Initialize new payloads and rebase access when a plugin replaces its envelope. */
	setEmbedded(field: SchemaField, value: unknown, rebaseAccess = true) {
		const retainedIssues = correlateEmbeddedIssues(field, this.get(field.path), value, this.issues);
		const before = embeddedOccurrences(field, this.get(field.path));
		value = cloneFormValue(value);
		const after = embeddedOccurrences(field, value);
		if (before.issues.length === 0 && after.issues.length === 0) {
			const retained = new Set(before.occurrences.map(embeddedOccurrenceKey));
			for (const occurrence of after.occurrences) {
				if (occurrence.identity !== undefined && retained.has(embeddedOccurrenceKey(occurrence)))
					continue;
				Object.assign(
					occurrence.payload,
					initialFormValues(occurrence.block.fields, occurrence.payload)
				);
			}
		}
		if (
			rebaseAccess &&
			this.access !== undefined &&
			before.issues.length === 0 &&
			after.issues.length === 0
		) {
			const previousPaths = new Map(
				before.occurrences.map((occurrence) => [occurrence.path, occurrence])
			);
			const nextIdentities = new Map(
				after.occurrences
					.filter((occurrence) => occurrence.identity !== undefined)
					.map((occurrence) => [embeddedOccurrenceKey(occurrence), occurrence])
			);
			const fields: AccessCapabilitiesEnvelope["fields"] = {};
			for (const [path, capability] of Object.entries(this.access.fields)) {
				let parentPath = path;
				while (!previousPaths.has(parentPath) && parentPath.includes("."))
					parentPath = parentPath.slice(0, parentPath.lastIndexOf("."));
				const occurrence = previousPaths.get(parentPath);
				if (occurrence === undefined) {
					fields[path] = capability;
					continue;
				}
				const next = nextIdentities.get(embeddedOccurrenceKey(occurrence));
				if (next !== undefined) fields[next.path + path.slice(occurrence.path.length)] = capability;
			}
			this.access = { ...this.access, fields };
		}
		this.set(field.path, value);
		this.issues = [...this.issues, ...retainedIssues];
	}

	reset(values: FormValues, fields: readonly SchemaField[] = this.#fields) {
		this.#fields = fields;
		this.#invalidateEditors();
		this.#clearDerivedTextBindings();
		this.values = cloneFormValues(values);
		this.#invalidateValueIndexes();
		this.original = cloneFormValues(values);
		this.localizationSources = {};
		this.issues = [];
		this.#submittingIssues = [];
		this.submitting = false;
		this.revision = ++this.#revision;
	}

	reconcile(
		previousFields: readonly SchemaField[],
		nextFields: readonly SchemaField[],
		initializeDefaults = false
	) {
		this.#invalidateEditors();
		this.#clearDerivedTextBindings();
		this.#fields = nextFields;
		const result = reconcileFormSchema(this, previousFields, nextFields, { initializeDefaults });
		this.values = result.values;
		this.#invalidateValueIndexes();
		this.original = result.original;
		this.issues = [];
		this.#submittingIssues = [];
		this.revision = ++this.#revision;
		return { detached: result.detached, restoredFields: 0 };
	}

	recover(
		draft: { values: FormValues; original: FormValues },
		previousFields: readonly SchemaField[],
		nextFields: readonly SchemaField[]
	) {
		this.#invalidateEditors();
		this.#clearDerivedTextBindings();
		this.#fields = nextFields;
		const result = recoverFormDraft(this, draft, previousFields, nextFields);
		this.values = result.values;
		this.#invalidateValueIndexes();
		this.original = result.original;
		this.issues = [];
		this.#submittingIssues = [];
		this.revision = ++this.#revision;
		return { detached: result.detached, restoredFields: result.restoredFields };
	}

	async submit<T>(
		fields: readonly SchemaField[],
		requireMissing: boolean,
		operation: (values: FormValues) => Promise<T>
	) {
		// Save owns the next validation result, but cancelling advisory work must not
		// make its current feedback disappear while the request is in flight.
		const retainedIssues = uniqueValidationIssues([
			...this.issues,
			...this.#submittingIssues,
			...this.liveValidation.issues,
		]);
		this.liveValidation.suspend();
		const pendingIssues = this.pendingEditIssues();
		if (pendingIssues.length > 0) {
			this.issues = [...pendingIssues];
			throw new FormValidationError(pendingIssues, this.#i18n);
		}
		this.#fields = fields;
		const controllerRevision = this.#revision;
		const include = (path: string, canonicalPath: string) =>
			this.canRead(path, canonicalPath) &&
			this.canWrite(path, canonicalPath) &&
			shouldSubmitLocalizedPath(
				path,
				this.values,
				this.original,
				this.contentLocale,
				this.localizationSources,
				fields
			);
		const unknownIssues = [
			...unknownBlockIssues(fields, this.original, this.#i18n),
			...unknownBlockIssues(fields, this.values, this.#i18n),
		];
		const clientIssues =
			unknownIssues.length > 0
				? unknownIssues
				: validateFormValues(fields, this.values, {
						requireMissing,
						include,
						i18n: this.#i18n,
					});
		if (clientIssues.length > 0) {
			this.issues = clientIssues;
			throw new FormValidationError(clientIssues, this.#i18n);
		}
		this.submitting = true;
		this.issues = [];
		this.#submittingIssues = retainedIssues;
		const submittedValues = cloneFormValues(this.values);
		const retainIssueOccurrences = this.#captureIssueLifetime(fields);
		try {
			const result = await operation(submissionFormValues(fields, this.values, include));
			if (this.#revision === controllerRevision) {
				this.#invalidateEditors();
				this.original = cloneFormValues(submittedValues);
				this.issues = [];
				this.#submittingIssues = [];
				this.submitting = false;
				this.revision = ++this.#revision;
			}
			return result;
		} catch (error) {
			if (this.#revision === controllerRevision) {
				this.issues =
					error instanceof RiduError && error.issues.length > 0
						? correlateFormIssues(
								fields,
								submittedValues,
								this.values,
								retainIssueOccurrences(error.issues)
							)
						: [...this.#submittingIssues];
				this.#submittingIssues = [];
			}
			throw error;
		} finally {
			if (this.#revision === controllerRevision) this.submitting = false;
		}
	}

	#captureIssueLifetime(fields: readonly SchemaField[]) {
		const epoch = this.editorEpoch;
		const locations = indexFieldValues(fields, this.values);
		const listEdits = new Map(
			locations.flatMap(({ token, schema }) =>
				schema?.type === "text-list" || schema?.type === "number-list"
					? [[token, this.#listEdits.get(token)] as const]
					: []
			)
		);
		const paths = new Map(locations.map((location) => [location.path, location.token]));
		// Reuse the controller's existing mount identities: removing and reinserting
		// the same serialized key cannot resurrect a pending authoritative issue.
		const mounts = new Map(
			locations.flatMap(({ token, value }) =>
				isRecord(value) ? [[token, this.rowMountKey(value)] as const] : []
			)
		);
		return (issues: readonly ValidationIssue[]) => {
			if (this.editorEpoch !== epoch) return [];
			const current = new Map(
				indexFieldValues(fields, this.values).flatMap(({ token, value }) =>
					isRecord(value) ? [[token, this.rowMountKey(value)] as const] : []
				)
			);
			const expired = [...mounts].flatMap(([token, mount]) =>
				current.get(token) === mount ? [] : [token.slice(0, -1)]
			);
			return issues.filter((issue) => {
				let path = issue.path;
				while (!paths.has(path) && path.includes(".")) path = path.slice(0, path.lastIndexOf("."));
				const token =
					issue.target === undefined ? paths.get(path) : canonicalIssueTarget(issue.target);
				if (issue.target !== undefined && token === undefined) return false;
				if (
					token !== undefined &&
					listEdits.has(token) &&
					listEdits.get(token) !== this.#listEdits.get(token)
				)
					return false;
				return (
					token === undefined ||
					!expired.some((prefix) => token === `${prefix}]` || token.startsWith(`${prefix},`))
				);
			});
		};
	}

	#setDerivedText(path: string, value: string) {
		if (String(this.get(path) ?? "") !== value) this.set(path, value);
	}

	#clearDerivedTextBindings() {
		for (const binding of this.#derivedTextBindings.values()) binding.stop();
		this.#derivedTextBindings.clear();
	}
}

function authoredPath(path: string) {
	return path
		.split(".")
		.filter((segment) => !/^\d+$/.test(segment))
		.join(".");
}

function isIndexedChildPath(parent: string, candidate: string) {
	const prefix = parent === "" ? "" : `${parent}.`;
	if (!candidate.startsWith(prefix)) return false;
	return /^\d+$/.test(candidate.slice(prefix.length).split(".", 1)[0] ?? "");
}

function uniqueRowIndexes(rows: readonly unknown[]) {
	const indexes = new Map<string, number>();
	rows.forEach((row, index) => {
		if (isRecord(row) && typeof row._key === "string" && row._key.trim() !== "")
			indexes.set(row._key, indexes.has(row._key) ? -1 : index);
	});
	return indexes;
}

function uniqueValidationIssues(issues: readonly ValidationIssue[]) {
	const unique = new Map<string, ValidationIssue>();
	for (const issue of issues) {
		const key = JSON.stringify([
			issue.path,
			issue.code,
			issue.message,
			issue.target,
			issue.fieldId,
			issue.collectionId,
			issue.globalId,
			issue.locale,
		]);
		if (!unique.has(key)) unique.set(key, issue);
	}
	return [...unique.values()];
}

function readPath(values: FormValues, path: string) {
	let current: unknown = values;
	for (const segment of path.split(".")) {
		if (Array.isArray(current)) {
			current = current[Number(segment)];
		} else if (isRecord(current)) {
			current = current[segment];
		} else return undefined;
	}
	return current;
}

function writePath(values: FormValues, path: string, value: unknown) {
	const segments = path.split(".");
	let current: FormValues | unknown[] = values;
	for (let position = 0; position < segments.length - 1; position += 1) {
		const segment = segments[position];
		if (segment === undefined) continue;
		let next = getChild(current, segment);
		if (!isRecord(next) && !Array.isArray(next)) {
			const following = segments[position + 1];
			next = /^\d+$/.test(following ?? "") ? [] : {};
			setChild(current, segment, next);
		}
		current = next as FormValues | unknown[];
	}
	const last = segments.at(-1);
	if (last !== undefined) setChild(current, last, value);
}

function getChild(container: FormValues | unknown[], segment: string) {
	return Array.isArray(container) ? container[Number(segment)] : container[segment];
}

function setChild(container: FormValues | unknown[], segment: string, value: unknown) {
	if (Array.isArray(container)) container[Number(segment)] = value;
	else container[segment] = value;
}

function isRecord(value: unknown): value is FormValues {
	return typeof value === "object" && value !== null && !Array.isArray(value);
}

function embeddedOccurrenceKey(occurrence: EmbeddedOccurrence) {
	return JSON.stringify([
		occurrence.tree.key,
		occurrence.case.tagValue,
		occurrence.block.slug,
		occurrence.identity,
	]);
}
