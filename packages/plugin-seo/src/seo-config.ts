import type { SchemaField } from "@riducms/protocol";

export interface LengthConfig {
	generate: boolean;
	minLength: number;
	maxLength: number;
}

export interface OverviewConfig {
	titlePath: string;
	descriptionPath: string;
	imagePath: string;
	titleMin: number;
	titleMax: number;
	descriptionMin: number;
	descriptionMax: number;
}

export interface PreviewConfig {
	generate: boolean;
	titlePath: string;
	descriptionPath: string;
}

export function lengthConfig(field: SchemaField): LengthConfig {
	const value = componentConfig(field);
	return {
		generate: value.generate === true,
		minLength: positiveInteger(value.minLength, field.type === "textarea" ? 100 : 50),
		maxLength: positiveInteger(value.maxLength, field.type === "textarea" ? 150 : 60),
	};
}

export function overviewConfig(field: SchemaField): OverviewConfig {
	const value = componentConfig(field);
	return {
		titlePath: stringValue(value.titlePath, "meta.title"),
		descriptionPath: stringValue(value.descriptionPath, "meta.description"),
		imagePath: stringValue(value.imagePath, "meta.image"),
		titleMin: positiveInteger(value.titleMin, 50),
		titleMax: positiveInteger(value.titleMax, 60),
		descriptionMin: positiveInteger(value.descriptionMin, 100),
		descriptionMax: positiveInteger(value.descriptionMax, 150),
	};
}

export function previewConfig(field: SchemaField): PreviewConfig {
	const value = componentConfig(field);
	return {
		generate: value.generate === true,
		titlePath: stringValue(value.titlePath, "meta.title"),
		descriptionPath: stringValue(value.descriptionPath, "meta.description"),
	};
}

export function imageGenerationEnabled(field: SchemaField) {
	return componentConfig(field).generate === true;
}

function componentConfig(field: SchemaField): Record<string, unknown> {
	const value = field.admin.component?.config;
	return typeof value === "object" && value !== null && !Array.isArray(value)
		? (value as Record<string, unknown>)
		: {};
}

function stringValue(value: unknown, fallback: string) {
	return typeof value === "string" && value.length > 0 ? value : fallback;
}

function positiveInteger(value: unknown, fallback: number) {
	return typeof value === "number" && Number.isInteger(value) && value > 0 ? value : fallback;
}
