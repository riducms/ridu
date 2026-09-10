import type { Component } from "svelte";
import type { PluginFieldProps, PluginFieldRegistration } from "@riducms/plugin/authoring/v1";
import {
	definePluginField,
	defineFieldComponent,
	defineAdminPlugin,
} from "@riducms/plugin/authoring/v1";
import Configured from "./ConfiguredPluginField.svelte";
import Color from "./ColorField.svelte";
const decode = (value: unknown): { items: string[] } => {
	if (
		typeof value !== "object" ||
		value === null ||
		!("items" in value) ||
		!Array.isArray(value.items) ||
		!value.items.every((item: unknown) => typeof item === "string")
	)
		throw new Error("Invalid items");
	return { items: value.items };
};
const config = (_value: unknown) => ({ limit: 2 });
const valid = definePluginField({
	component: Configured,
	decodeValue: decode,
	decodeConfig: config,
});
const exact: PluginFieldRegistration<{ items: string[] }> = valid;
definePluginField({
	component: Configured,
	decodeValue: decode,
	decodeConfig: (_raw: unknown) => ({ limit: 2 }),
});
defineAdminPlugin({
	key: "shapes",
	pairingVersion: 1,
	fields: {
		outline: valid,
		color: definePluginField({
			component: Color,
			decodeValue: (raw: unknown): string => String(raw),
		}),
	},
});
// @ts-expect-error config decoder required
// prettier-ignore
definePluginField({ component: Configured, decodeValue: decode });
// @ts-expect-error explicit generics do not bypass decoding
// prettier-ignore
definePluginField<{ items: string[] }, { limit: number }>({ component: Configured, decodeValue: decode, });
// @ts-expect-error any config still needs a decoder
// prettier-ignore
definePluginField<{ items: string[] }, any>({ component: Configured, decodeValue: decode });
// @ts-expect-error unknown config still needs a decoder
// prettier-ignore
definePluginField<{ items: string[] }, unknown>({ component: Configured, decodeValue: decode });
// @ts-expect-error unions do not bypass decoding
// prettier-ignore
definePluginField<{ items: string[] }, { limit: number } | undefined>({ component: Configured, decodeValue: decode, });
// @ts-expect-error wrong decoded config
// prettier-ignore
definePluginField({ component: Configured, decodeValue: decode, decodeConfig: () => ({ limit: "two" }), });
// @ts-expect-error wrong value contract
// prettier-ignore
definePluginField({ component: Configured, decodeValue: () => ({ items: [1] }), decodeConfig: config, });
// @ts-expect-error unknown decoder cannot promise a structured value
// prettier-ignore
definePluginField({ component: Configured, decodeValue: (value: unknown) => value, decodeConfig: config, });
// @ts-expect-error union decoder is incompatible with component writes
// prettier-ignore
definePluginField({ component: Configured, decodeValue: (_value: unknown): { items: string[] } | number => 1, decodeConfig: config, });
declare const wrong: Component<{ required: boolean }>;
// @ts-expect-error wrong component props
// prettier-ignore
definePluginField({ component: wrong, decodeValue: decode });
// @ts-expect-error generated descriptor output mismatch
// prettier-ignore
const incorrect: PluginFieldRegistration<{ items: number[] }> = valid;
declare const text: Component<PluginFieldProps<string, undefined, "text">>;
// @ts-expect-error text component cannot register as number
// prettier-ignore
defineFieldComponent({ type: "number", component: text, decodeValue: (): number => 1 });
let type: "text" | "number" = Math.random() > 0.5 ? "text" : "number";
// @ts-expect-error widened type cannot erase schema correlation
// prettier-ignore
defineFieldComponent({ type, component: text, decodeValue: (): string => "hello" });
// @ts-expect-error version belongs to the authoring import
// prettier-ignore
defineAdminPlugin({ apiVersion: 1, key: "invalid", pairingVersion: 1 });
// @ts-expect-error structurally fabricated entries lack registration evidence
// prettier-ignore
defineAdminPlugin({ key: "invalid", pairingVersion: 1, fields: { outline: { type: "plugin", component: Configured } }, });
declare const widenedComponent: Component<PluginFieldProps<string, undefined, "text" | "number">>;
// @ts-expect-error a union type cannot claim only the text half of the value contract
// prettier-ignore
defineFieldComponent({ type, component: widenedComponent, decodeValue: (): string => "wrong for number", });
declare const distinct: Component<
	PluginFieldProps<{ id: string; label: string }, undefined, "plugin", { id: string }>
>;
const separate = definePluginField({
	component: distinct,
	decodeValue: (_raw: unknown) => ({ id: "one", label: "One" }),
	decodeInput: (_raw: unknown) => ({ id: "one" }),
});
const both: PluginFieldRegistration<{ id: string; label: string }, { id: string }> = separate;
// @ts-expect-error missing decoder for explicitly distinct write shape
// prettier-ignore
definePluginField<{ id: string; label: string }, undefined, { id: string }>({ component: distinct, decodeValue: (_raw: unknown) => ({ id: "one", label: "One" }), decodeConfig: () => undefined, });
// @ts-expect-error incompatible decoder for the component's write shape
// prettier-ignore
definePluginField({ component: distinct, decodeValue: (_raw: unknown) => ({ id: "one", label: "One" }), decodeInput: () => 3, });
// @ts-expect-error a builtin component registration is not a plugin-owned field type
// prettier-ignore
defineAdminPlugin({ key: "wrong-kind", pairingVersion: 1, fields: { text: defineFieldComponent({ type: "text", component: text, decodeValue: (_raw: unknown): string => "x", }), }, });
// @ts-expect-error plugin-owned field types cannot be named builtin replacements
// prettier-ignore
defineAdminPlugin({ key: "wrong-kind", pairingVersion: 1, components: { outline: valid } });

declare const distinctProps: PluginFieldProps<
	{ id: string; label: string },
	undefined,
	"plugin",
	{ id: string }
>;
// @ts-expect-error pending input does not contain the server-produced label
// prettier-ignore
const inventedLabel: string = distinctProps.field.value!.label;
const pendingOrSaved: { id: string } | null | undefined = distinctProps.field.value;

const named = defineFieldComponent({
	type: "plugin",
	fieldType: "outline",
	component: Configured,
	decodeValue: decode,
	decodeConfig: config,
});
const target: "outline" = named.fieldType;
defineAdminPlugin({ key: "outline-tools", pairingVersion: 1, components: { Outline: named } });
// @ts-expect-error named plugin renderers require an exact field type
// prettier-ignore
defineFieldComponent({type:"plugin",component:Configured,decodeValue:decode,decodeConfig:config});
// @ts-expect-error named renderer cannot be registered as a default field type
// prettier-ignore
defineAdminPlugin({key:"outline-tools",pairingVersion:1,fields:{outline:named}});
