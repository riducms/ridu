import type { FieldType, SchemaField } from "@riducms/protocol";
import type { Component } from "svelte";

import type { FieldAuthoringHost } from "./authoring";
import type { FieldForm } from "./form";
import type { AdminI18n } from "./i18n";

export interface FieldComponentProps {
	field: SchemaField;
	form: FieldForm;
	i18n: AdminI18n;
	authoring?: FieldAuthoringHost;
}

export interface FieldPlugin {
	type: FieldType;
	key?: string;
	/** Named renderer selected by field.admin.component for built-in field semantics. */
	componentKey?: string;
	component?: Component<FieldComponentProps>;
	canRender(field: SchemaField): boolean;
}

export function defineFieldPlugin<const Plugin extends FieldPlugin>(plugin: Plugin): Plugin {
	return plugin;
}
