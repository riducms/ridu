import { isRecord } from "@riducms/protocol";
export type RichTextFeature =
	"links" | "lists" | "code" | "horizontal-rule" | "uploads" | "relationships" | "blocks";

/** Presentation options from Go's `richtext.Admin`. Every option defaults to false. */
export interface RichTextAdminConfig {
	fixedToolbar: boolean;
	hideGutter: boolean;
	hideDraggableBlockElement: boolean;
	hideAddBlockButton: boolean;
	hideInsertParagraphAtEnd: boolean;
}

export interface RichTextConfig {
	features: readonly RichTextFeature[];
	uploadCollections: readonly string[];
	relationshipCollections: readonly string[];
	admin: Readonly<RichTextAdminConfig>;
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

const adminOptions = [
	"fixedToolbar",
	"hideGutter",
	"hideDraggableBlockElement",
	"hideAddBlockButton",
	"hideInsertParagraphAtEnd",
] as const satisfies readonly (keyof RichTextAdminConfig)[];

export function decodeRichTextConfig(value: unknown): RichTextConfig {
	if (!isRecord(value)) throw new Error("Rich-text config must be an object.");
	for (const key of Object.keys(value))
		if (!["features", "uploadCollections", "relationshipCollections", "admin"].includes(key))
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
		admin: decodeAdminConfig(value.admin),
	};
}

function decodeAdminConfig(value: unknown): RichTextAdminConfig {
	const admin: RichTextAdminConfig = {
		fixedToolbar: false,
		hideGutter: false,
		hideDraggableBlockElement: false,
		hideAddBlockButton: false,
		hideInsertParagraphAtEnd: false,
	};
	if (value === undefined) return admin;
	if (!isRecord(value)) throw new Error("Rich-text admin config must be an object.");
	for (const [key, option] of Object.entries(value)) {
		if (!isAdminOption(key)) throw new Error(`Unsupported rich-text admin option ${key}.`);
		if (typeof option !== "boolean")
			throw new Error(`Rich-text admin option ${key} must be a boolean.`);
		admin[key] = option;
	}
	return admin;
}

function isAdminOption(key: string): key is keyof RichTextAdminConfig {
	return (adminOptions as readonly string[]).includes(key);
}

export function hasRichTextFeature(config: RichTextConfig, feature: RichTextFeature): boolean {
	return config.features.includes(feature);
}

function isRichTextFeature(value: unknown): value is RichTextFeature {
	return typeof value === "string" && features.has(value as RichTextFeature);
}
