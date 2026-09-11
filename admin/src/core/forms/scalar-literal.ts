import type { SchemaScalarLiteral } from "@riducms/protocol";

/** Decode manifest operands without coercing malformed booleans or empty/nonfinite numbers. */
export function scalarLiteralValue(
	literal: SchemaScalarLiteral
): string | number | boolean | undefined {
	switch (literal.type) {
		case "string":
			return literal.value;
		case "number": {
			if (literal.value.trim() === "") return undefined;
			const value = Number(literal.value);
			return Number.isFinite(value) ? value : undefined;
		}
		case "boolean":
			return literal.value === "true" ? true : literal.value === "false" ? false : undefined;
	}
}
