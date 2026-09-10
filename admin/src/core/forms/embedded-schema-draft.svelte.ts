import { resolveBlockTypes } from "@riducms/protocol";
import type {
	AdminI18n,
	EmbeddedSchemaDraft,
	EmbeddedSchemaFormScope,
	EmbeddedSchemaVariantScope,
} from "@riducms/plugin";
import type { SchemaField } from "@riducms/protocol";
import { FormController } from "@admin/core/forms/form-controller.svelte";
import { embeddedOccurrences } from "@admin/core/forms/embedded-fields";
import { cloneFormValue, initialFormValues } from "@admin/core/forms/form-schema";
import { validateFormValues } from "@admin/core/forms/form-validation";
import { cloneFieldForPaste } from "@admin/fields/field-clipboard";
import { fieldAccessPath, scopeRepeatedRowField } from "@admin/fields/nested/scoped-field";
import { indexFieldValues, canonicalIssueTarget } from "@admin/core/forms/form-issue-correlation";

/** Rendering state stays private; the plugin receives only the public detached edit. */
export interface HostedSchemaDraft {
	draft: EmbeddedSchemaDraft;
	form: FormController;
	fields: SchemaField[];
}

export function embeddedVariant(field: SchemaField, scope: EmbeddedSchemaVariantScope) {
	const tree = field.plugin?.embeddedTrees?.find((candidate) => candidate.key === scope.treeKey);
	const branch = tree?.cases.find((candidate) => candidate.tagValue === scope.caseTag);
	const block = resolveBlockTypes(branch).find((candidate) => candidate.slug === scope.variantSlug);
	if (tree === undefined || branch === undefined || block === undefined)
		throw new Error(
			"The embedded schema is no longer available. Reload its configuration before editing."
		);
	return { tree, branch, block };
}

export function copyEmbeddedSchemaPayload(
	field: SchemaField,
	scope: EmbeddedSchemaVariantScope,
	payload: Readonly<Record<string, unknown>>
) {
	const { branch, block } = embeddedVariant(field, scope);
	if (payload[branch.discriminator] !== block.slug)
		throw new Error("The copied payload does not match the configured embedded schema.");
	const copy = cloneFormValue(payload) as Record<string, unknown>;
	for (const child of block.fields) {
		if (Object.hasOwn(copy, child.name))
			copy[child.name] = cloneFieldForPaste(child, copy[child.name]);
	}
	copy[branch.identity] = crypto.randomUUID();
	return copy;
}

export function createEmbeddedSchemaDraft(
	parent: FormController,
	getField: () => SchemaField,
	scope: EmbeddedSchemaFormScope | EmbeddedSchemaVariantScope,
	i18n: AdminI18n,
	isActive: () => boolean = () => true
): HostedSchemaDraft {
	const field = getField();
	// Rendered fields carry DOM-specific IDs. Detached validation keeps manifest field identity.
	const authoredField =
		indexFieldValues(parent.schemaFields, parent.values).find(
			(location) => location.path === field.path
		)?.schema ?? field;
	if (
		("readOnly" in scope && scope.readOnly) ||
		parent.submitting ||
		field.admin.readOnly ||
		!parent.canWrite(field.path, fieldAccessPath(field))
	)
		throw new Error("This field is read-only.");
	const revision = parent.revision;
	const locale = parent.contentLocale;
	const signature = JSON.stringify(field.plugin?.embeddedTrees);
	const existing = "identity" in scope;
	const findOccurrence = () =>
		embeddedOccurrences(getField(), parent.get(getField().path)).occurrences.find(
			(occurrence) =>
				occurrence.tree.key === scope.treeKey && existing && occurrence.identity === scope.identity
		);
	const occurrence = existing ? findOccurrence() : undefined;
	if (existing && occurrence === undefined)
		throw new Error("The embedded occurrence no longer exists.");
	const variant =
		occurrence === undefined
			? embeddedVariant(authoredField, scope as EmbeddedSchemaVariantScope)
			: embeddedVariant(authoredField, {
					treeKey: occurrence.tree.key,
					caseTag: occurrence.case.tagValue,
					variantSlug: occurrence.block.slug,
				});
	const { branch, block } = variant;
	const accessState = () => {
		const currentPath = existing ? findOccurrence()?.path : undefined;
		const canonical = block.fields.map(fieldAccessPath);
		const entries = Object.entries(parent.access?.fields ?? {})
			.filter(
				([path]) =>
					path === getField().path ||
					path === fieldAccessPath(getField()) ||
					(currentPath !== undefined &&
						(path === currentPath || path.startsWith(`${currentPath}.`))) ||
					canonical.some((prefix) => path === prefix || path.startsWith(`${prefix}.`))
			)
			.map(([path, capability]) => [
				currentPath !== undefined && path.startsWith(currentPath)
					? "$payload" + path.slice(currentPath.length)
					: path,
				capability,
			])
			.sort((left, right) => String(left[0]).localeCompare(String(right[0])));
		return JSON.stringify({
			operations: parent.access?.operations,
			mode: parent.accessMode,
			entries,
		});
	};
	const initialAccess = accessState();
	const id = `draft_${crypto.randomUUID().replaceAll("-", "")}`;
	const identity = occurrence?.identity ?? crypto.randomUUID();
	const originalPayload = occurrence === undefined ? undefined : JSON.stringify(occurrence.payload);
	const payload =
		occurrence === undefined
			? initialFormValues(block.fields, {
					[branch.identity]: identity,
					[branch.discriminator]: block.slug,
				})
			: (cloneFormValue(occurrence.payload) as Record<string, unknown>);
	const form = new FormController({ [id]: payload }, i18n, parent);
	const fields = block.fields.map((child) => scopeRepeatedRowField(child, id, id));
	const container: SchemaField = {
		id,
		name: id,
		path: id,
		type: "group",
		category: "nested",
		required: true,
		unique: false,
		admin: { label: block.labels.singular },
		nested: { fields: block.fields },
	};
	form.reset({ [id]: payload }, [container]);
	form.setResource(parent.resource);
	const rebase = (path: string) =>
		occurrence !== undefined && (path === occurrence.path || path.startsWith(`${occurrence.path}.`))
			? id + path.slice(occurrence.path.length)
			: path;
	form.setAccess(
		parent.access === undefined
			? undefined
			: {
					...parent.access,
					fields: Object.fromEntries(
						Object.entries(parent.access.fields).map(([path, value]) => [rebase(path), value])
					),
				},
		parent.accessMode
	);
	form.setLocalization(
		locale,
		Object.fromEntries(
			Object.entries(parent.localizationSources).map(([path, value]) => [rebase(path), value])
		)
	);
	const draftTargets = new Map(
		indexFieldValues([container], form.snapshot()).map((location) => [
			location.path,
			location.token,
		])
	);
	form.issues =
		occurrence === undefined
			? []
			: parent.issues
					.filter(
						(issue) =>
							issue.path === occurrence.path || issue.path.startsWith(`${occurrence.path}.`)
					)
					.map((issue) => {
						const path = rebase(issue.path);
						return {
							...issue,
							path,
							...(issue.target === undefined ? {} : { target: draftTargets.get(path) }),
						};
					});
	let discarded = false;
	// Revocation is synchronous; UI notification waits until any derived read has finished.
	let publishedDiscard = $state(false);
	let invalidated = false;
	const isStale = () =>
		discarded ||
		invalidated ||
		!isActive() ||
		parent.submitting ||
		parent.revision !== revision ||
		parent.contentLocale !== locale ||
		accessState() !== initialAccess ||
		JSON.stringify(getField().plugin?.embeddedTrees) !== signature ||
		getField().admin.readOnly === true ||
		!parent.canWrite(getField().path, fieldAccessPath(getField())) ||
		(existing && JSON.stringify(findOccurrence()?.payload) !== originalPayload);
	const stale = () => {
		// A getter can run inside a Svelte derived expression. Record revocation without
		// mutating reactive form state; retained child capabilities consult this scope.
		if (isStale()) invalidated = true;
		return invalidated;
	};
	form.setEditorScope(() => !stale());
	if (parent.liveValidation.available) {
		const relative = (path: string) => {
			if (!path.startsWith(`${id}.`)) throw new Error("The embedded field is outside this draft.");
			return path.slice(id.length + 1);
		};
		form.configureLiveValidation(async (input, signal) => {
			if (stale()) throw new Error("This embedded schema draft is stale.");
			const nested = input.embedded ?? [];
			const data = input.data[id];
			if (typeof data !== "object" || data === null || Array.isArray(data))
				throw new Error("The embedded payload is unavailable.");
			const result = await parent.requestLiveValidation(
				{
					...(parent.resource?.id === undefined ? {} : { id: parent.resource.id }),
					data: parent.liveValidationData(),
					fields: nested.length ? input.fields : input.fields.map(relative),
					embedded: [
						{
							field: getField().path,
							treeKey: variant.tree.key,
							caseTag: branch.tagValue,
							variantSlug: block.slug,
							identity,
							data: { ...data, [branch.identity]: identity, [branch.discriminator]: block.slug },
						},
						...nested.map((scope, index) =>
							index === 0 ? { ...scope, field: relative(scope.field) } : scope
						),
					],
				},
				signal
			);
			// Only the innermost host rebases payload-relative feedback into its private form.
			if (nested.length) return result;
			const target = (value: string | undefined) => {
				if (value === undefined) return undefined;
				const canonical = canonicalIssueTarget(value);
				return canonical === undefined ? undefined : JSON.stringify([id, ...JSON.parse(canonical)]);
			};
			return {
				evaluations: result.evaluations.map((evaluation) => ({
					...evaluation,
					path: `${id}.${evaluation.path}`,
					target: target(evaluation.target),
					issues: evaluation.issues.map((issue) => ({
						...issue,
						path: `${id}.${issue.path}`,
						target: target(issue.target),
					})),
				})),
			};
		});
	}
	const stop = parent.registerPendingEdit(() =>
		discarded || !isActive() || (!form.dirty && existing)
			? []
			: [
					{
						code: "pending_embedded_edit",
						path: getField().path,
						message: i18n.t("errors:pendingEmbeddedEdit"),
					},
				]
	);
	const stopObserve = parent.observe(field.path.split(".")[0]!, () => {
		if (stale()) form.disposeBindings();
	});
	const stopChanges = parent.observeChanges(() => {
		if (stale()) form.disposeBindings();
		else form.refreshLiveValidation();
	});
	const stopLifetime = parent.registerEditorLifetime(() => draft.discard());
	const draft: EmbeddedSchemaDraft = {
		id,
		identity,
		get dirty() {
			return !publishedDiscard && !discarded && (form.dirty || !existing);
		},
		get stale() {
			return publishedDiscard || stale();
		},
		get issues() {
			return form.issues.map((issue) => ({ ...issue }));
		},
		payload: () => {
			if (stale()) throw new Error("This embedded schema draft is stale.");
			return cloneFormValue(form.get(id)) as Record<string, unknown>;
		},
		validate: () => {
			form.liveValidation.suspend();
			if (stale()) {
				form.issues = [
					{
						code: "stale_embedded_edit",
						path: id,
						message: i18n.t("errors:staleEmbeddedEdit"),
					},
				];
				return false;
			}
			form.issues = [
				...form.pendingEditIssues(),
				...validateFormValues([container], form.values, {
					requireMissing: !existing,
					include: (path, canonicalPath) =>
						form.canRead(path, canonicalPath) && form.canWrite(path, canonicalPath),
					i18n,
				}),
			];
			return form.issues.length === 0;
		},
		discard: () => {
			if (discarded) return;
			discarded = true;
			queueMicrotask(() => {
				publishedDiscard = true;
				stop();
			});
			stopObserve();
			stopChanges();
			stopLifetime();
			form.disposeBindings();
		},
	};
	return { draft, form, fields };
}
