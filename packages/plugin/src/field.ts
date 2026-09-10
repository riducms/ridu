import type { FieldType, SchemaField, ValidationIssue } from "@riducms/protocol";
import type { Component } from "svelte";
import type { FieldAuthoringHost } from "./authoring";
import type { FieldDocumentForm, FieldLiveValidation } from "./form";
import type { AdminI18n } from "./i18n";
import { decodeComponentConfig } from "./component-config";

/**
 * Read or update one field in the current, unsaved document form.
 *
 * A binding is a connection to a particular field. For a field inside an array or
 * block, it follows that row's stable `_key` when the row moves. It does not switch
 * to whichever row later occupies the same numbered position.
 *
 * Ridu creates and cleans up bindings. Removing the field, unmounting its editor,
 * or replacing the document, locale, schema, or saved/reset form values makes a
 * binding stale. Reads and writes then throw; `stale` and `readOnly` remain safe
 * to check. Obtain a new binding from the newly mounted editor.
 */
export interface PluginFieldBinding<Value, Type extends FieldType = FieldType, Input = Value> {
	/**
	 * A copy of the field's resolved settings, including its current path, label and rules.
	 * `schema.admin.readOnly` reflects field configuration and access, not a pending document save.
	 */
	readonly schema: SchemaField & { type: Type };
	/**
	 * The latest value in the form, including unsaved edits. Objects and arrays are
	 * copied: modifying this result does not update the form. Call `set` to change it.
	 *
	 * `null` and `undefined` mean the field is empty or absent. Other values pass
	 * through `decodeValue`, or through `decodeInput` if the output decoder rejects
	 * a pending edit. Reading throws if neither decoder accepts the value.
	 */
	readonly value: Value | Input | null | undefined;
	/**
	 * A copy of the current value before decoding. Use this to display or recover
	 * older data your decoder rejects; validate it before treating it as `Value`.
	 */
	readonly rawValue: unknown;
	/** Current form and server validation issues for this field and its children. */
	readonly issues: readonly ValidationIssue[];
	/** Opted-in server feedback, managed by the host; save validation stays independent. */
	readonly liveValidation: FieldLiveValidation;
	/**
	 * Whether changes are currently blocked, for example by access rules, a save in
	 * progress, or a stale binding. Bindings from `form.bind` also respect the source
	 * editor's read-only state. `set` checks this again when called.
	 */
	readonly readOnly: boolean;
	/** Whether this connection has expired. Once true, it cannot become usable again. */
	readonly stale: boolean;
	/**
	 * Replace the value in the unsaved form. Use `null` to clear it; `undefined` is
	 * rejected. Ridu copies and decodes the supplied value, checks editability, and
	 * updates its normal form state. This does not send a save request to the server.
	 *
	 * Throws if the binding is stale, the field is read-only, the decoder rejects the
	 * value, or a structured edit changes a protected child field. Server validation
	 * still runs when the document is saved.
	 */
	set: (value: Input | null) => void;
}

/** Read the containing document form, or connect to another field to update it. */
export interface PluginForm extends FieldDocumentForm {
	/**
	 * Connect to another field in this document's unsaved form.
	 *
	 * Paths start at the document root: `"title"`, `"seo.description"`, or
	 * `"reviews.0.note"`. The last example selects the first review **now**. Keep
	 * the returned binding: it follows that same row if the user reorders reviews.
	 * Calling `bind("reviews.0.note")` later would select the row at index 0 then.
	 *
	 * Create the binding before starting a request, timer or other delayed work.
	 * Ridu disables it if the target disappears or the source editor closes, and
	 * also when the document, locale, schema or saved/reset form values change.
	 * Later reads and writes throw instead of editing a different field or document.
	 * The target must be editable, and the source editor must be editable too.
	 *
	 * The returned value type is `unknown`: a string path does not prove a field's
	 * TypeScript type. Check read values and supply the target field's expected shape.
	 * `bind` itself throws if the path has no identifiable field. Repeated rows
	 * currently require nonempty, unique `_key` values.
	 *
	 * @example
	 * ```ts
	 * // In component setup: remember the Title field for this editor.
	 * const title = form.bind("title");
	 * // In a click handler: change Title in the form. The user still needs to save.
	 * function useNoteAsTitle() {
	 *   title.set(field.value?.text ?? null);
	 * }
	 * ```
	 */
	bind(path: string): PluginFieldBinding<unknown>;
}

/**
 * Props Ridu supplies to a plugin's Svelte field editor.
 *
 * `Value` describes saved data, `Config` describes decoded editor settings, and
 * `Type` is the schema field type (normally `"plugin"`). Set `Input` only when
 * edits sent to the server have a different shape from saved data; it defaults
 * to `Value`. Register the component with `definePluginField` or `defineFieldComponent`.
 *
 * `field` reads and changes this input's unsaved value. `form` reads the document
 * and can bind another editable field. `i18n` translates messages, and `authoring`
 * supplies document pickers, plugin requests, and forms for structured values.
 * `config` is present only when the registration supplies a settings decoder.
 * These props are created by Ridu; the application does not assemble them itself.
 */
export type PluginFieldProps<
	Value,
	Config = undefined,
	Type extends FieldType = "plugin",
	Input = Value,
> = {
	/** This editor's field. Read `field.value` and change it with `field.set(...)`. */
	field: PluginFieldBinding<Value, Type, Input>;
	/** The same document form used by the surrounding fields, including unsaved edits. */
	form: PluginForm;
	/** Translate interface text and format dates/numbers using the admin's preferences. */
	i18n: AdminI18n;
	/** Ridu tools for related documents, plugin requests and forms inside structured values. */
	authoring: FieldAuthoringHost;
} & ([Config] extends [undefined] ? { config?: never } : { config: Config });

type FieldDefinition<Value, Config, Type extends FieldType, Input = Value> = {
	/** A Svelte component accepting `PluginFieldProps` for the decoder's value/config types. */
	component: Component<
		PluginFieldProps<NoInfer<Value>, NoInfer<Config>, NoInfer<Type>, NoInfer<Input>>
	>;
	/**
	 * Check saved data and return the shape your component can read; throw a useful
	 * error for unsupported data. Runs synchronously and can run on every value read.
	 * Accept JSON data, not class instances, functions or cyclic objects. Ridu handles
	 * empty null/undefined values separately. Go validation still decides what saves.
	 */
	decodeValue: (value: unknown) => Value;
};

const registration = Symbol("ridu-plugin-field");
const valueContract = Symbol("ridu-plugin-field-value");
const registrations = new WeakSet<RegisteredPluginField>();

/**
 * Registration stored by Ridu after a helper has checked the component and decoders.
 * Plugin authors should use `definePluginField` or `defineFieldComponent` and let
 * TypeScript infer the result. Do not construct this object or call its component
 * directly: Ridu supplies the field's form connection when rendering it.
 */
export interface RegisteredPluginField {
	readonly [registration]: true;
	readonly type: FieldType;
	readonly fieldType?: string;
	readonly component: Component<never>;
	decodeConfig(field: SchemaField): unknown;
	decodeValue(value: unknown): unknown;
	decodeInput(value: unknown): unknown;
	decodeFormValue(value: unknown): unknown;
}

/**
 * Helper return type that preserves saved-value and write-value types for generated
 * Go/admin compatibility checks. Let TypeScript infer it from your registration;
 * annotating everything as `RegisteredPluginField` would lose this information.
 */
export interface PluginFieldRegistration<
	Value,
	Input = Value,
	Type extends FieldType = FieldType,
> extends RegisteredPluginField {
	readonly type: Type;
	readonly [valueContract]: { value: (value: Value) => Value; input: (value: Input) => Input };
}

/**
 * Register the default Svelte editor for a new field type provided by your Go plugin.
 *
 * Put the result in `defineAdminPlugin({ fields: { color: ... } })`. The map key
 * matches the Go descriptor's `PluginFieldType.Key`. Applications then use the Go
 * field helper, such as `color.Field("accent")`, without selecting an editor.
 * For a different input on an existing application field, use `defineFieldEditor`.
 *
 * Required options are `component` and `decodeValue`. The component receives
 * `PluginFieldProps`. `decodeValue` checks unknown saved data and returns the
 * component's value type. TypeScript infers that type from the function's return.
 *
 * Add `decodeInput` only when edits sent to the server differ from saved values.
 * Without it, `decodeValue` checks reads and writes. Add `decodeConfig` only when
 * the component needs Go field settings as its `config` prop. Without a settings
 * decoder, the plugin's field config must be empty.
 *
 * Decoders must return synchronously and throw useful errors for invalid data.
 * Keep them free of network requests or other side effects: they may run on every
 * value read. Ridu handles empty `null`/`undefined` values separately. These checks
 * help the browser interpret values; Go validation still decides what can be saved.
 *
 * @param definition The component, required saved-value decoder, and optional settings/input decoders.
 * @returns A frozen registration retaining inferred saved and input types. Put
 * it directly in the plugin's `fields` map; it does not add the Go field type.
 * @throws If a component or decoder is missing or invalid. Data/config decoding
 * can throw later when the admin reads a value or renders the selected field.
 * @example
 * ```ts
 * import { defineAdminPlugin, definePluginField } from '@riducms/plugin/authoring/v1';
 * import ColorField from './color-field.svelte';
 * import { decodeColor } from './value';
 *
 * export const colorAdminPlugin = defineAdminPlugin({
 *   key: 'color',
 *   pairingVersion: 1,
 *   fields: {
 *     color: definePluginField({ component: ColorField, decodeValue: decodeColor })
 *   }
 * });
 * ```
 */
export function definePluginField<Value, Config, Input>(
	definition: FieldDefinition<Value, Config, "plugin", Input> & {
		/** Check serialized Go field settings and return the component's config; throw if invalid. */
		decodeConfig: (value: unknown) => Config;
		/** Check values passed to `field.set`; required when writes differ from saved values. */
		decodeInput: (value: unknown) => Input;
	}
): PluginFieldRegistration<Value, Input, "plugin"> & { readonly fieldType?: never };
/**
 * Register a plugin field editor with checked settings. Put the result in your
 * `defineAdminPlugin` fields map; the map key must match the Go field-type key.
 * `decodeValue` checks reads and writes of the same shape. `decodeConfig` checks
 * the Go settings before rendering. Both must be synchronous and throw on invalid
 * data. Ridu owns the unsaved form; Go still validates the document on save.
 */
export function definePluginField<Value, Config>(
	definition: FieldDefinition<Value, Config, "plugin"> & {
		/** Check serialized Go field settings and return the component's config; throw if invalid. */
		decodeConfig: (value: unknown) => Config;
		decodeInput?: never;
	}
): PluginFieldRegistration<Value, Value, "plugin"> & { readonly fieldType?: never };
/**
 * Register a plugin field editor without settings. Put the result in your plugin's
 * `fields` map under the Go field-type key. `decodeValue` checks saved data;
 * `decodeInput` checks the different shape your component writes. Both must run
 * synchronously and throw on invalid data. The component receives no config prop;
 * the descriptor-owned field configuration must be empty.
 * Go still validates the document on save.
 */
export function definePluginField<Value, Input>(
	definition: FieldDefinition<Value, undefined, "plugin", Input> & {
		/** Check values passed to `field.set`; required when writes differ from saved values. */
		decodeInput: (value: unknown) => Input;
		decodeConfig?: never;
	}
): PluginFieldRegistration<Value, Input, "plugin"> & { readonly fieldType?: never };
/**
 * Register a plugin field editor without settings. Put the result in your plugin's
 * `fields` map under the Go field-type key. `decodeValue` checks reads and writes;
 * it must run synchronously and throw on invalid data. The component receives no
 * config prop; the descriptor-owned field configuration must be empty.
 * Ridu owns the unsaved form,
 * and Go still validates the document on save.
 */
export function definePluginField<Value>(
	definition: FieldDefinition<Value, undefined, "plugin"> & {
		decodeConfig?: never;
		decodeInput?: never;
	}
): PluginFieldRegistration<Value, Value, "plugin"> & { readonly fieldType?: never };
export function definePluginField(definition: RuntimeDefinition): RegisteredPluginField {
	return createRegistration("plugin", definition);
}

type SingleType<Type, Whole = Type> = 0 extends 1 & Type
	? never
	: Type extends unknown
		? [Whole] extends [Type]
			? Type
			: never
		: never;

type BuiltinValue<Type extends FieldType> = Type extends "text-list"
	? string[]
	: Type extends "number-list"
		? number[]
		: Type extends "number"
			? number
			: Type extends "checkbox"
				? boolean
				: Type extends "text" | "textarea" | "email" | "date" | "code"
					? string
					: Type extends "ui"
						? undefined
						: unknown;

type NamedFieldTarget<Type extends FieldType, Key extends string> = Type extends "plugin"
	? { readonly fieldType: Key }
	: { readonly fieldType?: never };
/**
 * Register a plugin component that can replace an existing field's editor.
 *
 * Put the result in `defineAdminPlugin({ components: { colorSwatch: ... } })`.
 * Go selects that name through `field.PluginComponent(pluginKey, "colorSwatch")`
 * in the field's `Admin.Editor` setting. This changes the input while keeping
 * the original Go field's storage, validation, permissions, and generated types.
 * For an input used only in your application, use `defineFieldEditor` instead.
 *
 * Required options are `type`, `component`, and `decodeValue`. Use the exact Go
 * field type for `type`, such as `"text"`. When it is `"plugin"`, also supply the
 * exact `fieldType` key, such as `"richtext"`. Omit `fieldType` for built-in fields.
 *
 * `decodeValue` checks saved data. Add `decodeInput` if write values have another
 * shape; otherwise `decodeValue` checks both. The Svelte component receives the
 * matching `PluginFieldProps<Value, Config, Type, Input>`.
 *
 * Add `decodeConfig` only when `field.PluginComponent` supplies settings. That
 * decoder requires a Go settings object, even when empty. Without one, omit the
 * settings argument. All decoders must return synchronously and throw useful
 * errors for invalid input. Go still validates the document when it is saved.
 *
 * @param definition The existing field type, component, and value/settings decoders.
 * @returns A frozen registration for the plugin's `components` map. It preserves
 * the selected field type and inferred value/input types; it does not create a new field type.
 * @throws If the field type, component, decoders, or plugin field-type selection are invalid.
 * @example
 * ```ts
 * import { defineAdminPlugin, defineFieldComponent } from '@riducms/plugin/authoring/v1';
 * import ColorSwatch from './color-swatch.svelte';
 *
 * export const editorialAdminPlugin = defineAdminPlugin({
 *   key: 'editorial-tools',
 *   pairingVersion: 1,
 *   components: {
 *     colorSwatch: defineFieldComponent({
 *       type: 'text',
 *       component: ColorSwatch,
 *       decodeValue(value): string {
 *         if (typeof value !== 'string') throw new Error('Expected text.');
 *         return value;
 *       }
 *     })
 *   }
 * });
 * ```
 */
export function defineFieldComponent<
	const Type extends FieldType,
	Value extends BuiltinValue<Type>,
	Config,
	Input extends BuiltinValue<Type>,
	const Key extends string = string,
>(
	definition: { type: Type & SingleType<NoInfer<Type>> } & NamedFieldTarget<Type, Key> &
		FieldDefinition<Value, Config, Type, Input> & {
			/** Check the settings supplied by Go `field.Admin.Editor`; throw if invalid. */
			decodeConfig: (value: unknown) => Config;
			/** Check values passed to `field.set` when writes differ from saved values. */
			decodeInput: (value: unknown) => Input;
		}
): PluginFieldRegistration<Value, Input, Type> & NamedFieldTarget<Type, Key>;
/**
 * Name an alternative editor in your paired plugin's `components` map. Go selects
 * it with `.Admin(field.Admin{Editor: field.PluginComponent(pluginKey, componentName, config)})`. Set `type` to
 * the actual field type; also supply `fieldType` for plugin values. `decodeValue`
 * checks reads/writes and `decodeConfig` checks settings, synchronously. Changing
 * the editor does not change the field's storage or server validation.
 */
export function defineFieldComponent<
	const Type extends FieldType,
	Value extends BuiltinValue<Type>,
	Config,
	const Key extends string = string,
>(
	definition: { type: Type & SingleType<NoInfer<Type>> } & NamedFieldTarget<Type, Key> &
		FieldDefinition<Value, Config, Type> & {
			/** Check the settings supplied by Go `field.Admin.Editor`; throw if invalid. */
			decodeConfig: (value: unknown) => Config;
			decodeInput?: never;
		}
): PluginFieldRegistration<Value, Value, Type> & NamedFieldTarget<Type, Key>;
/**
 * Name an alternative editor without settings in your plugin's `components` map.
 * Go selects it with `field.Admin.Editor`. Set the actual schema `type`, plus
 * `fieldType` for plugin values. Supply synchronous `decodeValue` and `decodeInput`
 * for different saved/write shapes. Omit the Go settings argument; no config prop is
 * passed. Any supplied component settings require a decoder.
 * The field's storage and server validation stay the same.
 */
export function defineFieldComponent<
	const Type extends FieldType,
	Value extends BuiltinValue<Type>,
	Input extends BuiltinValue<Type>,
	const Key extends string = string,
>(
	definition: { type: Type & SingleType<NoInfer<Type>> } & NamedFieldTarget<Type, Key> &
		FieldDefinition<Value, undefined, Type, Input> & {
			/** Check values passed to `field.set` when writes differ from saved values. */
			decodeInput: (value: unknown) => Input;
			decodeConfig?: never;
		}
): PluginFieldRegistration<Value, Input, Type> & NamedFieldTarget<Type, Key>;
/**
 * Name an alternative editor without settings in your plugin's `components` map.
 * Go selects it with `field.Admin.Editor`. Set the actual schema `type`, plus
 * `fieldType` for plugin values. `decodeValue` synchronously checks reads/writes.
 * Omit the Go settings argument; no config prop is passed. Any supplied component
 * settings require a decoder. The field's storage and server
 * validation stay the same. For local field editors, use `defineFieldEditor`.
 */
export function defineFieldComponent<
	const Type extends FieldType,
	Value extends BuiltinValue<Type>,
	const Key extends string = string,
>(
	definition: { type: Type & SingleType<NoInfer<Type>> } & NamedFieldTarget<Type, Key> &
		FieldDefinition<Value, undefined, Type> & { decodeConfig?: never; decodeInput?: never }
): PluginFieldRegistration<Value, Value, Type> & NamedFieldTarget<Type, Key>;
export function defineFieldComponent(
	definition: RuntimeDefinition & { type: FieldType; fieldType?: string }
): RegisteredPluginField {
	if (
		definition.type === "plugin" &&
		!/^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$/.test(definition.fieldType ?? "")
	)
		throw new Error("A named plugin renderer requires an exact fieldType.");
	if (definition.type !== "plugin" && definition.fieldType !== undefined)
		throw new Error("Only plugin renderers select fieldType.");
	return createRegistration(definition.type, definition, definition.fieldType);
}

interface RuntimeDefinition {
	component: Component<never>;
	decodeValue: (value: unknown) => unknown;
	decodeConfig?: (value: unknown) => unknown;
	decodeInput?: (value: unknown) => unknown;
}
function createRegistration(
	type: FieldType,
	definition: RuntimeDefinition,
	fieldType?: string
): RegisteredPluginField {
	const supported: Record<FieldType, true> = {
		"text-list": true,
		"number-list": true,
		text: true,
		textarea: true,
		email: true,
		date: true,
		code: true,
		number: true,
		checkbox: true,
		json: true,
		select: true,
		radio: true,
		point: true,
		ui: true,
		join: true,
		virtual: true,
		relationship: true,
		upload: true,
		group: true,
		array: true,
		blocks: true,
		plugin: true,
	};
	if (!Object.hasOwn(supported, type)) throw new Error(`Unsupported field renderer type ${type}.`);
	if ("canRender" in definition || "key" in definition || "componentKey" in definition)
		throw new Error(
			"Renderer matching comes from its keyed registration; key/canRender/componentKey are not supported."
		);
	if (
		typeof definition.component !== "function" ||
		typeof definition.decodeValue !== "function" ||
		(definition.decodeConfig !== undefined && typeof definition.decodeConfig !== "function") ||
		(definition.decodeInput !== undefined && typeof definition.decodeInput !== "function")
	)
		throw new Error(
			"A plugin field requires a component, decodeValue and a synchronous config decoder when configured."
		);
	const { component, decodeValue, decodeConfig, decodeInput = decodeValue } = definition;
	const hasInputDecoder = definition.decodeInput !== undefined;
	const result = Object.freeze({
		[registration]: true as const,
		[valueContract]: { value: (value: unknown) => value, input: (value: unknown) => value },
		type,
		...(fieldType === undefined ? {} : { fieldType }),
		component,
		decodeValue(value: unknown) {
			return checkedValue(type, synchronous(decodeValue(detach(value))));
		},
		decodeInput(value: unknown) {
			return checkedValue(type, synchronous(decodeInput(detach(value))));
		},
		decodeFormValue(value: unknown) {
			try {
				return checkedValue(type, synchronous(decodeValue(detach(value))));
			} catch (error) {
				if (!hasInputDecoder) throw error;
				return checkedValue(type, synchronous(decodeInput(detach(value))));
			}
		},
		decodeConfig(field: SchemaField) {
			if (field.type !== type)
				throw new Error(`Renderer expects ${type}, but ${field.path} is ${field.type}.`);
			if (fieldType !== undefined && field.plugin?.key !== fieldType)
				throw new Error(
					`Renderer expects plugin field type ${fieldType}, but ${field.path} uses ${field.plugin?.key}.`
				);
			if (field.admin.component !== undefined) {
				try {
					return decodeComponentConfig(field.admin.component, decodeConfig);
				} catch (error) {
					throw new Error(
						`Component ${field.admin.component.plugin}:${field.admin.component.component} config for ${field.path} is invalid: ${error instanceof Error ? error.message : String(error)}`
					);
				}
			}
			const raw = field.plugin?.config ?? {};
			if (decodeConfig === undefined) {
				if (
					typeof raw !== "object" ||
					raw === null ||
					Array.isArray(raw) ||
					Object.keys(raw).length !== 0
				)
					throw new Error(
						`Field ${field.path} has configuration but its renderer has no decodeConfig.`
					);
				return undefined;
			}
			try {
				return synchronous(decodeConfig(detach(raw)));
			} catch (error) {
				throw new Error(
					`Invalid renderer config for ${field.path}: ${error instanceof Error ? error.message : String(error)}`
				);
			}
		},
	});
	registrations.add(result);
	return result;
}

export function assertPluginFieldRegistration(value: RegisteredPluginField): void {
	if (!registrations.has(value))
		throw new Error(
			"Plugin field registrations must use definePluginField or defineFieldComponent."
		);
}
function synchronous<Value>(value: Value): Value {
	if (
		typeof value === "object" &&
		value !== null &&
		"then" in value &&
		typeof value.then === "function"
	)
		throw new Error("Field decoders must be synchronous.");
	return value;
}
function detach(value: unknown): unknown {
	const seen = new Set<object>();
	const copy = (item: unknown, depth: number): unknown => {
		if (depth > 100) throw new Error("Field value exceeds the supported JSON depth.");
		if (
			item === null ||
			item === undefined ||
			typeof item === "string" ||
			typeof item === "boolean"
		)
			return item;
		if (typeof item === "number" && Number.isFinite(item)) return item;
		if (
			typeof item !== "object" ||
			(!Array.isArray(item) &&
				Object.getPrototypeOf(item) !== Object.prototype &&
				Object.getPrototypeOf(item) !== null) ||
			seen.has(item)
		)
			throw new Error("Field values and config must contain finite, acyclic JSON data.");
		seen.add(item);
		const result = Array.isArray(item)
			? item.map((child) => copy(child, depth + 1))
			: Object.fromEntries(
					Object.entries(item).map(([key, child]) => [key, copy(child, depth + 1)])
				);
		seen.delete(item);
		return result;
	};
	return copy(value, 0);
}

function checkedValue(type: FieldType, result: unknown): unknown {
	if (
		(["text", "textarea", "email", "date", "code"].includes(type) && typeof result !== "string") ||
		(type === "number" && typeof result !== "number") ||
		(type === "text-list" &&
			(!Array.isArray(result) || !Array.from(result).every((item) => typeof item === "string"))) ||
		(type === "number-list" &&
			(!Array.isArray(result) ||
				!Array.from(result).every((item) => typeof item === "number" && Number.isFinite(item)))) ||
		(type === "checkbox" && typeof result !== "boolean") ||
		(type === "ui" && result !== undefined)
	)
		throw new Error(`Invalid decoded ${type} value.`);
	return detach(result);
}
