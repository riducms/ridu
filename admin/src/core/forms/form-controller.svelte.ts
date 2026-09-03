import type { AccessCapabilitiesEnvelope, ValidationIssue } from "@riducms/protocol";
import type { SchemaField } from "@riducms/protocol";
import type { AdminI18n } from "@riducms/plugin";
import { RiduError } from "@riducms/sdk";
import { createAdminI18n } from "@riducms/translations";

import {
	cloneFormValues,
	localizationSource,
	reconcileFormSchema,
	recoverFormDraft,
	shouldSubmitLocalizedPath,
	type DetachedDraftValue,
	type FormValues,
	submissionFormValues,
} from "@admin/core/forms/form-schema";
import { validateFormValues } from "@admin/core/forms/form-validation";

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
	values = $state<FormValues>({});
	original = $state<FormValues>({});
	issues = $state<ValidationIssue[]>([]);
	submitting = $state(false);
	revision = $state(0);
	access = $state.raw<AccessCapabilitiesEnvelope>();
	accessMode = $state<"create" | "update">("update");
	writeBlocked = $state(false);
	resource = $state.raw<FormResource>();
	contentLocale = $state<string>();
	localizationSources = $state.raw<Readonly<Record<string, string>>>({});
	#registered = new Set<string>();
	#pathObservers = new Map<string, Set<(value: unknown) => void>>();
	#derivedTextBindings = new Map<string, DerivedTextBindingState>();
	#revision = 0;
	#i18n: AdminI18n;

	constructor(values: FormValues = {}, i18n: AdminI18n = createAdminI18n()) {
		this.#i18n = i18n;
		this.reset(values);
	}

	get dirty() {
		return JSON.stringify(this.values) !== JSON.stringify(this.original);
	}

	register(path: string) {
		this.#registered.add(path);
		return () => this.#registered.delete(path);
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

	get(path: string) {
		return readPath(this.values, path);
	}

	snapshot() {
		return cloneFormValues(this.values);
	}

	set(path: string, value: unknown) {
		writePath(this.values, path, value);
		this.issues = this.issues.filter(
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
	}

	issuesFor(path: string) {
		return this.issues.filter((issue) => issue.path === path || issue.path.startsWith(`${path}.`));
	}

	setAccess(access: AccessCapabilitiesEnvelope | undefined, mode: "create" | "update") {
		this.access = access;
		this.accessMode = mode;
	}

	setResource(resource: FormResource | undefined) {
		this.resource = resource;
	}

	setLocalization(locale: string | undefined, sources: Readonly<Record<string, string>> = {}) {
		this.contentLocale = locale;
		this.localizationSources = sources;
	}

	localizationSource(path: string) {
		return localizationSource(path, this.values, this.original, this.localizationSources);
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

	invalidateIndexedFieldCapabilities(path: string) {
		const access = this.access;
		if (access === undefined) return;
		const fields = Object.fromEntries(
			Object.entries(access.fields).filter(([candidate]) => !isIndexedChildPath(path, candidate))
		);
		if (Object.keys(fields).length === Object.keys(access.fields).length) return;
		this.access = { ...access, fields };
	}

	reset(values: FormValues) {
		this.#clearDerivedTextBindings();
		this.values = cloneFormValues(values);
		this.original = cloneFormValues(values);
		this.localizationSources = {};
		this.issues = [];
		this.submitting = false;
		this.revision = ++this.#revision;
	}

	reconcile(
		previousFields: readonly SchemaField[],
		nextFields: readonly SchemaField[],
		initializeDefaults = false
	) {
		this.#clearDerivedTextBindings();
		const result = reconcileFormSchema(this, previousFields, nextFields, { initializeDefaults });
		this.values = result.values;
		this.original = result.original;
		this.issues = [];
		this.revision = ++this.#revision;
		return { detached: result.detached, restoredFields: 0 };
	}

	recover(
		draft: { values: FormValues; original: FormValues },
		previousFields: readonly SchemaField[],
		nextFields: readonly SchemaField[]
	) {
		this.#clearDerivedTextBindings();
		const result = recoverFormDraft(this, draft, previousFields, nextFields);
		this.values = result.values;
		this.original = result.original;
		this.issues = [];
		this.revision = ++this.#revision;
		return { detached: result.detached, restoredFields: result.restoredFields };
	}

	async submit<T>(
		fields: readonly SchemaField[],
		requireMissing: boolean,
		operation: (values: FormValues) => Promise<T>
	) {
		const controllerRevision = this.#revision;
		const include = (path: string, canonicalPath: string) =>
			this.canRead(path, canonicalPath) &&
			this.canWrite(path, canonicalPath) &&
			shouldSubmitLocalizedPath(
				path,
				this.values,
				this.original,
				this.contentLocale,
				this.localizationSources
			);
		const clientIssues = validateFormValues(fields, this.values, {
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
		try {
			const result = await operation(submissionFormValues(fields, this.values, include));
			if (this.#revision === controllerRevision) {
				this.original = cloneFormValues(this.values);
				this.revision = ++this.#revision;
			}
			return result;
		} catch (error) {
			if (this.#revision === controllerRevision && error instanceof RiduError) {
				this.issues = [...error.issues];
			}
			throw error;
		} finally {
			if (this.#revision === controllerRevision) this.submitting = false;
		}
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
