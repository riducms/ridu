import type { AdminDataType, SchemaAdminLoader } from "@riducms/protocol";

/** Generated reference to a compiled Go read function. Never author its schema by hand. */
export interface AdminLoader<Input, Data> {
	readonly key: string;
	readonly contract: SchemaAdminLoader;
	query(input: Input): string;
	decode(value: unknown): Data;
}

/**
 * Bind authored runtime behavior to a Go-derived contract. Only generated code
 * should call this; application code consumes the resulting typed reference.
 */
export function createAdminLoader<Input, Data>(
	contract: SchemaAdminLoader
): AdminLoader<Input, Data> {
	return {
		key: contract.key,
		contract,
		query(input) {
			if (typeof input !== "object" || input === null)
				throw new TypeError("Expected loader input object");
			const query = new URLSearchParams();
			for (const [key, value] of Object.entries(input)) {
				if (!Object.hasOwn(contract.input.fields ?? {}, key))
					throw new TypeError(`Unknown loader input ${key}`);
				if (value !== undefined && value !== null) query.set(key, String(value));
			}
			return query.toString();
		},
		decode(value) {
			if (!matchesData(contract.output, value))
				throw new TypeError(`Invalid data for admin loader ${contract.key}`);
			return value as Data;
		},
	};
}

function matchesData(type: AdminDataType, value: unknown): boolean {
	if (value === null) return type.nullable === true;
	switch (type.kind) {
		case "string":
			return typeof value === "string";
		case "number":
			return typeof value === "number" && Number.isFinite(value);
		case "boolean":
			return typeof value === "boolean";
		case "array":
			return (
				Array.isArray(value) &&
				type.element !== undefined &&
				value.every((item) => matchesData(type.element!, item))
			);
		case "object":
			// Output objects are structurally extensible: validate every declared member
			// without rejecting additional fields outside this loader's projection.
			if (typeof value !== "object" || Array.isArray(value)) return false;
			return Object.entries(type.fields ?? {}).every(([key, field]) =>
				Object.hasOwn(value, key)
					? matchesData(field, (value as Record<string, unknown>)[key])
					: field.optional === true
			);
	}
}
