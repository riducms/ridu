import type {
	EmbeddedSchemaDraft,
	FieldAuthoringHost,
	FieldReferenceBrowserProps,
} from "@riducms/plugin";
import type { PluginFieldBinding } from "@admin/core/forms/plugin-field-binding";
import { cloneFormValue } from "@admin/core/forms/form-schema";

/** Every retained callable checks its lease, including after asynchronous host operations. */
export function guardPluginAuthoring(
	host: FieldAuthoringHost,
	binding: PluginFieldBinding,
	registerDraft?: (draft: EmbeddedSchemaDraft, raw: EmbeddedSchemaDraft) => void
): FieldAuthoringHost {
	return {
		get collections() {
			binding.assertActive();
			return cloneFormValue(host.collections) as typeof host.collections;
		},
		get documentRevision() {
			binding.assertActive();
			return host.documentRevision;
		},
		get locale() {
			binding.assertActive();
			return host.locale;
		},
		referenceBrowser: (anchor, props) => {
			binding.assertActive();
			const overrides = {
				get readOnly() {
					return binding.readOnly || props.readOnly;
				},
				onCommit: async (ids: string[]) => {
					binding.assertEditable();
					const result = await props.onCommit([...ids]);
					binding.assertEditable();
					return result;
				},
				onClose: () => {
					binding.assertActive();
					props.onClose();
				},
			};
			// Delegate every read, including spread-props proxies whose backing object is replaced.
			const guarded = new Proxy({} as FieldReferenceBrowserProps, {
				get: (_target, key) =>
					Reflect.has(overrides, key) ? Reflect.get(overrides, key) : Reflect.get(props, key),
				set: (_target, key, value) => Reflect.set(props, key, value),
				has: (_target, key) => Reflect.has(overrides, key) || Reflect.has(props, key),
				ownKeys: () => [...new Set([...Reflect.ownKeys(props), ...Reflect.ownKeys(overrides)])],
				getOwnPropertyDescriptor: (_target, key) => {
					if (!Reflect.has(overrides, key) && !Reflect.has(props, key)) return undefined;
					const setter = Reflect.has(overrides, key)
						? undefined
						: Reflect.getOwnPropertyDescriptor(props, key)?.set;
					return {
						configurable: true,
						enumerable: true,
						get: () => Reflect.get(guarded, key),
						...(setter === undefined
							? {}
							: {
									set: (value: unknown) => {
										Reflect.set(props, key, value);
									},
								}),
					};
				},
			});
			return host.referenceBrowser(anchor, guarded);
		},
		async findDocument(...args) {
			binding.assertActive();
			const result = await host.findDocument(...args);
			binding.assertActive();
			return cloneFormValue(result) as typeof result;
		},
		...(host.requestPlugin === undefined
			? {}
			: {
					async requestPlugin<Result>(
						path: string,
						body: unknown,
						signal?: AbortSignal
					): Promise<Result> {
						binding.assertEditable();
						const detached = cloneFormValue(body);
						binding.assertEditable();
						const result = await host.requestPlugin!<Result>(path, detached, signal);
						binding.assertEditable();
						return cloneFormValue(result) as Result;
					},
				}),
		...(host.schemaForm === undefined ? {} : { schemaForm: host.schemaForm }),
		...(host.schemaHeader === undefined ? {} : { schemaHeader: host.schemaHeader }),
		...(host.schemaIssues === undefined
			? {}
			: {
					schemaIssues: (scope) => {
						binding.assertActive();
						return host.schemaIssues!(scope).map((issue) => ({ ...issue }));
					},
				}),
		...(host.copySchemaPayload === undefined
			? {}
			: {
					copySchemaPayload: (scope, payload) => {
						binding.assertActive();
						const result = host.copySchemaPayload!(scope, payload);
						binding.assertActive();
						return result;
					},
				}),
		...(host.beginSchemaDraft === undefined
			? {}
			: {
					beginSchemaDraft: (scope) => {
						binding.assertEditable();
						const raw = host.beginSchemaDraft!({ ...scope });
						const discard = raw.discard;
						const stop = binding.onDestroy(() => raw.discard());
						// Drawers dispose the raw session; every disposal must release owner cleanup.
						raw.discard = () => {
							stop();
							discard();
						};
						const assertDraft = () => {
							binding.assertEditable();
							if (raw.stale) throw new Error("This embedded schema draft is stale.");
						};
						const draft: EmbeddedSchemaDraft = {
							id: raw.id,
							identity: raw.identity,
							get stale() {
								return binding.stale || raw.stale;
							},
							get dirty() {
								return !binding.stale && raw.dirty;
							},
							get issues() {
								binding.assertActive();
								return raw.issues.map((issue) => ({ ...issue }));
							},
							payload: () => {
								assertDraft();
								return cloneFormValue(raw.payload()) as Record<string, unknown>;
							},
							validate: () => {
								assertDraft();
								return raw.validate();
							},
							discard: raw.discard,
						};
						registerDraft?.(draft, raw);
						return draft;
					},
				}),
		...(host.schemaDraftEditor === undefined ? {} : { schemaDraftEditor: host.schemaDraftEditor }),
	};
}
