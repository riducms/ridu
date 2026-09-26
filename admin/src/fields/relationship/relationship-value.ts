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

	const references = ids.map((id) => ({ relationTo: target, id }));
	if (!hasMany) return references[0] ?? null;

	let index = 0;
	const previous = Array.isArray(current) ? current.filter(isPolymorphicReference) : [];
	const result = previous.flatMap((reference) => {
		if (reference.relationTo !== target) return [reference];
		const replacement = references[index++];
		return replacement ? [replacement] : [];
	});
	return [...result, ...references.slice(index)];
}

export function isPolymorphicReference(value: unknown): value is PolymorphicReference {
	return (
		typeof value === "object" &&
		value !== null &&
		"relationTo" in value &&
		typeof value.relationTo === "string" &&
		"id" in value &&
		typeof value.id === "string"
	);
}

/** Ordered identities for every target, including polymorphic many-value fields. */
export function relationshipReferences(
	value: unknown,
	hasMany: boolean,
	polymorphic: boolean,
	target: string
): PolymorphicReference[] {
	const values = hasMany ? (Array.isArray(value) ? value : []) : [value];
	return values.flatMap((item) => {
		if (polymorphic) return isPolymorphicReference(item) ? [item] : [];
		return typeof item === "string" && item !== "" ? [{ relationTo: target, id: item }] : [];
	});
}

export function relationshipKey(reference: PolymorphicReference) {
	return JSON.stringify([reference.relationTo, reference.id]);
}

export function parseRelationshipKey(value: string): PolymorphicReference | undefined {
	try {
		const parsed: unknown = JSON.parse(value);
		if (
			!Array.isArray(parsed) ||
			parsed.length !== 2 ||
			typeof parsed[0] !== "string" ||
			typeof parsed[1] !== "string"
		) {
			return undefined;
		}
		return { relationTo: parsed[0], id: parsed[1] };
	} catch {
		return undefined;
	}
}
