export interface PolymorphicReference {
	relationTo: string;
	id: string;
}

export function selectedRelationshipIDs(
	value: unknown,
	hasMany: boolean,
	polymorphic: boolean,
	target: string
): string[] {
	const values = hasMany ? (Array.isArray(value) ? value : []) : [value];
	return values.flatMap((item) => {
		if (!polymorphic && typeof item === "string" && item !== "") return [item];
		if (!isPolymorphicReference(item) || item.relationTo !== target) return [];
		return [item.id];
	});
}

export function updateRelationshipValue(
	current: unknown,
	ids: readonly string[],
	hasMany: boolean,
	polymorphic: boolean,
	target: string
): unknown {
	if (!polymorphic) return hasMany ? [...ids] : (ids[0] ?? "");

	const previous = hasMany
		? Array.isArray(current)
			? current.filter((item) => isPolymorphicReference(item) && item.relationTo !== target)
			: []
		: [];
	const references = ids.map((id) => ({ relationTo: target, id }));
	return hasMany ? [...previous, ...references] : (references[0] ?? null);
}

function isPolymorphicReference(value: unknown): value is PolymorphicReference {
	return (
		typeof value === "object" &&
		value !== null &&
		"relationTo" in value &&
		typeof value.relationTo === "string" &&
		"id" in value &&
		typeof value.id === "string"
	);
}
