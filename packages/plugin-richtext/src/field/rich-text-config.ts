export type RichTextFeature =
	"links" | "lists" | "code" | "horizontal-rule" | "uploads" | "relationships" | "blocks";

export interface RichTextConfig {
	features: readonly RichTextFeature[];
	uploadCollections: readonly string[];
	relationshipCollections: readonly string[];
}

const features = new Set<RichTextFeature>([
	"links",
	"lists",
	"code",
	"horizontal-rule",
	"uploads",
	"relationships",
	"blocks",
]);

export function parseRichTextConfig(value: unknown): RichTextConfig {
	if (!isRecord(value)) return { features: [], uploadCollections: [], relationshipCollections: [] };
	return {
		features: Array.isArray(value.features) ? value.features.filter(isRichTextFeature) : [],
		uploadCollections: Array.isArray(value.uploadCollections)
			? value.uploadCollections.filter(
					(collection): collection is string => typeof collection === "string"
				)
			: [],
		relationshipCollections: Array.isArray(value.relationshipCollections)
			? value.relationshipCollections.filter(
					(collection): collection is string => typeof collection === "string"
				)
			: [],
	};
}

export function hasRichTextFeature(config: RichTextConfig, feature: RichTextFeature): boolean {
	return config.features.includes(feature);
}

function isRichTextFeature(value: unknown): value is RichTextFeature {
	return typeof value === "string" && features.has(value as RichTextFeature);
}

function isRecord(value: unknown): value is Record<string, unknown> {
	return typeof value === "object" && value !== null && !Array.isArray(value);
}
