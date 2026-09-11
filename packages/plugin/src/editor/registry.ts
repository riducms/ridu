import type { Component } from "svelte";
import type { SchemaField } from "@riducms/protocol";

import type { FieldEditorProps, FieldEditorType } from "./types";
import { decodeComponentConfig } from "../component-config";

export const localEditorReference = /^app:[A-Za-z][A-Za-z0-9_]*$/;
const editorTypes = new Set<string>([
	"text",
	"textarea",
	"email",
	"date",
	"code",
	"number",
	"checkbox",
	"text-list",
	"number-list",
]);

/**
 * A custom field input and its optional settings decoder. `type` must match
 * the Go field's type. When Config is not undefined, `decodeConfig` is required;
 * naming a config type alone does not check serialized settings.
 */
export type FieldEditorDefinition<Type extends FieldEditorType, Config = undefined> = {
	/** The existing field type to display differently, for example `"text"` or `"number"`. */
	type: Type;
	/** Svelte component accepting the matching `FieldEditorProps<Type, Config>`. */
	component: Component<FieldEditorProps<NoInfer<Type>, NoInfer<Config>>>;
} & (0 extends 1 & Config
	? { decodeConfig: (value: unknown) => Config }
	: [Config] extends [never]
		? { decodeConfig: (value: unknown) => Config }
		: [Config] extends [undefined]
			? { decodeConfig?: (value: unknown) => Config }
			: { decodeConfig: (value: unknown) => Config });

const registration = Symbol("ridu-field-editor");
const registeredEditors = new WeakSet<RegisteredFieldEditor>();
/** Registration returned by `defineFieldEditor`; store it in `defineAdmin({ fields: ... })`. */
export interface RegisteredFieldEditor {
	readonly [registration]: true;
	readonly type: FieldEditorType;
	// Erasure must not advertise that a specific component accepts every value/config.
	// Only the validated host render boundary may recover its props.
	readonly component: Component<never>;
	decode(field: SchemaField): unknown;
}

/**
 * Register a Svelte component to replace a field input in your application.
 * Supports text, textarea, email, date, code, number, checkbox, text-list and number-list fields.
 *
 * Put the result under an `app:name` key in `defineAdmin({ fields: ... })` and
 * select that same key in Go with `field.Admin{Editor: field.Component("app:name")}`. The Go field keeps
 * its original storage, validation and filtering behaviour.
 *
 * `type` and `component` are required. The component receives `FieldEditorProps`
 * for that type: read `field.value`, update the unsaved form with `field.set`, and
 * render labels/errors with the `Field` component from `@riducms/plugin/editor/field`.
 * Ridu provides the value type, so this helper needs no `decodeValue` option.
 *
 * Add `decodeConfig` only when Go supplies settings through `field.Component`.
 * It checks unknown JSON and returns the component's `config` prop synchronously;
 * throw a useful error for invalid settings. With a decoder, Go must supply an
 * object even for empty settings. Without one, omit the settings argument in Go.
 * TypeScript infers the `Config` type from the decoder's return value.
 *
 * @param definition The existing field type, Svelte component, and optional settings decoder.
 * @returns A frozen registration for `defineAdmin`'s `fields` map. This does not
 * render the component, select it on a Go field, or change the stored value type.
 * @throws If the type is unsupported or the component is invalid. `ridu check`
 * also checks the selected Go field type and its settings before deployment.
 * @example
 * ```ts
 * import { defineAdmin } from '@riducms/plugin/admin';
 * import { defineFieldEditor } from '@riducms/plugin/editor';
 * import { generatedAdminPlugins } from './ridu.plugins.generated';
 * import TitleField from './components/title-field.svelte';
 *
 * export default defineAdmin({
 *   plugins: generatedAdminPlugins,
 *   fields: {
 *     'app:titleCounter': defineFieldEditor({ type: 'text', component: TitleField })
 *   }
 * });
 * ```
 */
export function defineFieldEditor<const Type extends FieldEditorType, Config = undefined>(
	definition: FieldEditorDefinition<Type, Config>
): RegisteredFieldEditor {
	if (!editorTypes.has(definition.type) || typeof definition.component !== "function")
		throw new Error("A field editor requires a supported field type and a Svelte component.");
	const { type, component, decodeConfig } = definition;
	const editor = Object.freeze({
		[registration]: true as const,
		type,
		component,
		decode(field: SchemaField): unknown {
			const reference = field.admin.editor?.reference;
			if (field.type !== type)
				throw new Error(
					`Editor ${reference} expects ${type}, but ${field.path} is ${field.type}. Change the Go editor selection or registration type.`
				);
			try {
				return decodeComponentConfig(field.admin.editor, decodeConfig);
			} catch (error) {
				throw new Error(
					`Editor ${reference} config for ${field.path} is invalid: ${error instanceof Error ? error.message : String(error)}. Fix the field component config in Go or the decoder in admin/src/admin.config.ts.`
				);
			}
		},
	});
	registeredEditors.add(editor);
	return editor;
}

export interface FieldEditorConfig {
	/** Custom field inputs selected by Go `field.Admin{Editor: field.Component("app:name")}`. Use `defineFieldEditor` for each entry. */
	fields?: Readonly<Record<`app:${string}`, RegisteredFieldEditor>>;
}

/** The application owns one static configuration; packaged plugins retain their pairing contracts. */
export function validateFieldEditorRegistrations(config: FieldEditorConfig): void {
	for (const [reference, editor] of Object.entries(config.fields ?? {})) {
		if (!localEditorReference.test(reference))
			throw new Error(
				`Invalid editor reference ${reference}; expected app:name in admin/src/admin.config.ts.`
			);
		if (!registeredEditors.has(editor))
			throw new Error(`Editor ${reference} must use defineFieldEditor.`);
	}
}

/** Check one local selection using the same decoder as the mounted editor. */
export function validateFieldEditorSelection(
	editors: NonNullable<FieldEditorConfig["fields"]>,
	field: SchemaField
): void {
	const reference = field.admin.editor?.reference;
	if (reference === undefined) return;
	if (!localEditorReference.test(reference))
		throw new Error(`Malformed editor reference ${reference}; expected app:name.`);
	const editor = editors[reference as `app:${string}`];
	if (editor === undefined)
		throw new Error(
			`Editor ${reference} is not registered. Register it in admin/src/admin.config.ts or change the field component in Go.`
		);
	editor.decode(field);
}
