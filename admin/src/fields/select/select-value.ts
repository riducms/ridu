import type { SchemaSelectChoice } from "@riducms/protocol";

export function selectManyValues(value: unknown): string[] {
	if (!Array.isArray(value)) return [];
	return value.filter((candidate): candidate is string => typeof candidate === "string");
}

export function selectChoiceLabel(choices: readonly SchemaSelectChoice[], value: string) {
	return choices.find((choice) => choice.value === value)?.label ?? value;
}

export function removeSelectValue(values: readonly string[], value: string) {
	return values.filter((candidate) => candidate !== value);
}
