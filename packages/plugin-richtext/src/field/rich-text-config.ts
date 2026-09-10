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

export function decodeRichTextConfig(value: unknown): RichTextConfig {
	if (!isRecord(value)) throw new Error("Rich-text config must be an object.");
	for (const key of Object.keys(value))
		if (!["features", "uploadCollections", "relationshipCollections"].includes(key))
			throw new Error(`Unsupported rich-text config key ${key}.`);
	const strings = (key: string): string[] => {
		const items = value[key] ?? [];
		if (!Array.isArray(items) || items.some((item) => typeof item !== "string" || !item))
			throw new Error(`${key} must be an array of nonempty strings.`);
		return [...items];
	};
	const selected = strings("features");
	if (!selected.every(isRichTextFeature)) throw new Error("Unsupported rich-text feature.");
	return {
		features: selected,
		uploadCollections: strings("uploadCollections"),
		relationshipCollections: strings("relationshipCollections"),
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
