import { scalarLiteralValue } from "@admin/core/forms/scalar-literal";
import type {
	SchemaFieldCondition,
	SchemaFieldConditionPredicate,
	SchemaScalarLiteral,
} from "@riducms/protocol";

export type FieldConditionValueReader = (path: string) => unknown;

/** Evaluates manifest-owned presentation metadata without mutating form values. */
export function evaluateFieldCondition(
	condition: SchemaFieldCondition,
	fieldPath: string,
	read: FieldConditionValueReader
): boolean {
	switch (condition.kind) {
		case "all":
			return condition.conditions.every((child) => evaluateFieldCondition(child, fieldPath, read));
		case "any":
			return condition.conditions.some((child) => evaluateFieldCondition(child, fieldPath, read));
		case "not":
			return !evaluateFieldCondition(condition.conditions[0], fieldPath, read);
		case "predicate":
			return evaluatePredicate(condition.predicate, fieldPath, read);
	}
}

export function fieldConditionValuePath(
	scope: SchemaFieldConditionPredicate["scope"],
	path: string,
	fieldPath: string
): string {
	if (scope === "document") return path;
	const separator = fieldPath.lastIndexOf(".");
	return separator === -1 ? path : `${fieldPath.slice(0, separator)}.${path}`;
}

function evaluatePredicate(
	predicate: SchemaFieldConditionPredicate,
	fieldPath: string,
	read: FieldConditionValueReader
) {
	const actual = read(fieldConditionValuePath(predicate.scope, predicate.path, fieldPath));
	const matches = (expected: SchemaScalarLiteral) => scalarMatches(actual, expected);
	switch (predicate.operator) {
		case "equals":
			return predicate.values[0] !== undefined && matches(predicate.values[0]);
		case "notEquals":
			return predicate.values[0] !== undefined && !matches(predicate.values[0]);
		case "oneOf":
			return predicate.values.some(matches);
	}
}

function scalarMatches(actual: unknown, expected: SchemaScalarLiteral) {
	const value = scalarLiteralValue(expected);
	return value !== undefined && actual === value;
}
