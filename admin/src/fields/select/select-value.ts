import type { SchemaSelectOption } from "@riducms/protocol";

export function selectManyValues(value: unknown): string[] {
	if (!Array.isArray(value)) return [];
	return value.filter((candidate): candidate is string => typeof candidate === "string");
}

export function selectOptionLabel(options: readonly SchemaSelectOption[], value: string) {
	return options.find((option) => option.value === value)?.label ?? value;
}

export function removeSelectValue(values: readonly string[], value: string) {
	return values.filter((candidate) => candidate !== value);
}
