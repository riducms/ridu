import { defineAdmin } from "../src/admin";
import type { Component } from "svelte";
import { defineFieldEditor, type FieldEditorProps, type FieldEditorType } from "../src/editor";

declare const Text: Component<FieldEditorProps<"text">>;
declare const TextList: Component<FieldEditorProps<"text-list">>;
declare const NumberList: Component<FieldEditorProps<"number-list">>;
declare const Number: Component<FieldEditorProps<"number">>;
declare const Palette: Component<FieldEditorProps<"text", { palette: string[] }>>;
declare const WrongProps: Component<{ value: number }>;
declare const widenedType: FieldEditorType;
declare const unknownConfig: unknown;
declare const unionConfig: { palette: string[] } | { other: boolean };

// Kept in a function so this compiler contract is not a runtime test.
function registrations() {
	defineFieldEditor({ type: "text-list", component: TextList });
	defineFieldEditor({ type: "number-list", component: NumberList });
	// @ts-expect-error a scalar text editor cannot consume a string list
	defineFieldEditor({ type: "text-list", component: Text });
	// @ts-expect-error text lists and number lists have different element types
	defineFieldEditor({ type: "number-list", component: TextList });
	const registeredText = defineFieldEditor({ type: "text", component: Text });
	const registeredPalette = defineFieldEditor({
		type: "text",
		component: Palette,
		decodeConfig: () => ({ palette: ["red"] }),
	});
	// @ts-expect-error an erased registration must not retype its original text component
	defineFieldEditor({ type: "number", component: registeredText.component });
	// @ts-expect-error erasure must not strip a component's required config decoder
	defineFieldEditor({ type: "text", component: registeredPalette.component });
	defineAdmin({ fields: { "app:text": defineFieldEditor({ type: "text", component: Text }) } });
	defineFieldEditor({
		type: "text",
		component: Palette,
		decodeConfig: (_value: unknown) => ({ palette: ["red"] }),
	});
	defineFieldEditor({
		type: "number",
		component: (_internals, props) => {
			const number: number | null | undefined = props.field.value;
			props.field.set(number ?? 0);
			// @ts-expect-error contextual props come from type, not a broad value union
			props.field.set("wrong");
			return {};
		},
	});
	// @ts-expect-error component props must agree with the selected underlying field type
	defineFieldEditor({ type: "text", component: Number });
	defineFieldEditor({
		type: "text",
		// @ts-expect-error configuration decoder must produce the component's required config
		component: Palette,
		decodeConfig: (_value: unknown) => ({ invalid: true }),
	});
	// @ts-expect-error a config-bearing component requires a decoder
	defineFieldEditor({ type: "text", component: Palette });
	// @ts-expect-error naming the config generic cannot replace a decoder
	defineFieldEditor<"text", { palette: string[] }>({ type: "text", component: Palette });
	// @ts-expect-error never cannot suppress the decoder and then receive undefined at runtime
	defineFieldEditor<"text", never>({ type: "text", component: Palette });
	// @ts-expect-error explicit any cannot suppress the decoder requirement
	defineFieldEditor<"text", any>({ type: "text", component: Palette });
	// @ts-expect-error unrelated component props cannot be supplied by the host
	defineFieldEditor({ type: "text", component: WrongProps });
	// @ts-expect-error a component for text cannot consume a widened field type
	defineFieldEditor({ type: widenedType, component: Text });
	// @ts-expect-error unknown decoder output has not established the required config
	defineFieldEditor({ type: "text", component: Palette, decodeConfig: () => unknownConfig });
	// @ts-expect-error every possible decoder result must satisfy the component
	defineFieldEditor({ type: "text", component: Palette, decodeConfig: () => unionConfig });
	// @ts-expect-error registry entries must have been checked by the helper
	defineAdmin({ fields: { "app:text": { type: "text", component: Text } } });
	// @ts-expect-error field editors do not support relationship value semantics
	defineFieldEditor({ type: "relationship", component: Text });
	// @ts-expect-error malformed namespace is rejected by the registry contextual type
	defineAdmin({ fields: { "other:text": defineFieldEditor({ type: "text", component: Text }) } });
	// @ts-expect-error an editor cannot be registered without a component
	defineFieldEditor({ type: "text" });
	defineAdmin({
		fields: {
			"app:text": defineFieldEditor({ type: "text", component: Text }),
			// @ts-expect-error duplicate object references are diagnosed before runtime
			"app:text": defineFieldEditor({ type: "text", component: Text }),
		},
	});
}
void registrations;

function localEditorCapabilities({ field, form, authoring }: FieldEditorProps<"text">) {
	field.set("Updated");
	form.get("title");
	form.issuesFor("title");
	form.snapshot?.();
	const locale: string | undefined = authoring.locale;
	void locale;
	// @ts-expect-error local writes use the typed occurrence binding
	form.set("title", "Updated");
	// @ts-expect-error the host owns occurrence registration
	form.register("title");
	// @ts-expect-error a local field editor has no owning plugin endpoint
	authoring.requestPlugin("generate", {});
	// @ts-expect-error embedded schema authoring belongs to paired plugin fields
	authoring.beginSchemaDraft({ treeKey: "blocks", identity: "one" });
}
void localEditorCapabilities;

function noConfigProps(base: Omit<FieldEditorProps<"text">, "config">) {
	const props: FieldEditorProps<"text"> = base;
	// @ts-expect-error an editor without settings does not accept a configuration object
	const extra: FieldEditorProps<"text"> = { ...base, config: {} };
	const configured: FieldEditorProps<"text", { palette: string[] }> = {
		...base,
		config: { palette: [] },
	};
	// @ts-expect-error a configured component requires validated configuration props
	const missing: FieldEditorProps<"text", { palette: string[] }> = base;
	void [props, configured, missing, extra];
}
void noConfigProps;

function listBindings(
	text: FieldEditorProps<"text-list">,
	numbers: FieldEditorProps<"number-list">
) {
	const labels: string[] | null | undefined = text.field.value;
	const sizes: number[] | null | undefined = numbers.field.value;
	text.field.set(labels ?? ["Solid oak", "Solid oak"]);
	numbers.field.set(sizes ?? [0, 8, 8]);
	// @ts-expect-error primitive list values are not scalar values
	text.field.set("scalar");
	// @ts-expect-error numeric editor input is not an unvalidated string array
	numbers.field.set(["-"]);
	// @ts-expect-error primitive lists cannot contain null elements
	text.field.set([null]);
}
void listBindings;
