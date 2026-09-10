export interface LengthConfig {
	generate: boolean;
	minLength: number;
	maxLength: number;
}

export interface OverviewConfig {
	titlePath: string;
	descriptionPath: string;
	imagePath: string;
	titleMin: number;
	titleMax: number;
	descriptionMin: number;
	descriptionMax: number;
}

export interface PreviewConfig {
	generate: boolean;
	titlePath: string;
	descriptionPath: string;
}

export function decodeLengthConfig(raw: unknown, type: "text" | "textarea"): LengthConfig {
	const value = componentConfig(raw, ["generate", "minLength", "maxLength"]);
	return {
		generate: value.generate === true,
		minLength: positiveInteger(value.minLength, type === "textarea" ? 100 : 50),
		maxLength: positiveInteger(value.maxLength, type === "textarea" ? 150 : 60),
	};
}

export function decodeOverviewConfig(raw: unknown): OverviewConfig {
	const value = componentConfig(raw, [
		"titlePath",
		"descriptionPath",
		"imagePath",
		"titleMin",
		"titleMax",
		"descriptionMin",
		"descriptionMax",
	]);
	return {
		titlePath: stringValue(value.titlePath, "meta.title"),
		descriptionPath: stringValue(value.descriptionPath, "meta.description"),
		imagePath: stringValue(value.imagePath, "meta.image"),
		titleMin: positiveInteger(value.titleMin, 50),
		titleMax: positiveInteger(value.titleMax, 60),
		descriptionMin: positiveInteger(value.descriptionMin, 100),
		descriptionMax: positiveInteger(value.descriptionMax, 150),
	};
}

export function decodePreviewConfig(raw: unknown): PreviewConfig {
	const value = componentConfig(raw, ["generate", "titlePath", "descriptionPath"]);
	return {
		generate: value.generate === true,
		titlePath: stringValue(value.titlePath, "meta.title"),
		descriptionPath: stringValue(value.descriptionPath, "meta.description"),
	};
}

export function decodeImageConfig(raw: unknown): { generate: boolean } {
	return { generate: componentConfig(raw, ["generate"]).generate === true };
}
function componentConfig(raw: unknown, allowed: readonly string[]): Record<string, unknown> {
	if (typeof raw !== "object" || raw === null || Array.isArray(raw))
		throw new Error("SEO config must be an object.");
	const value = raw as Record<string, unknown>;
	for (const key of Object.keys(value))
		if (!allowed.includes(key)) throw new Error(`Unsupported SEO config key ${key}.`);
	if (value.generate !== undefined && typeof value.generate !== "boolean")
		throw new Error("generate must be a boolean.");
	return value;
}
export function decodeString(value: unknown): string {
	if (typeof value !== "string") throw new Error("Expected a string value.");
	return value;
}
export function decodeUI(value: unknown): undefined {
	if (value !== undefined) throw new Error("UI fields have no stored value.");
	return undefined;
}

function stringValue(value: unknown, fallback: string) {
	if (value === undefined) return fallback;
	if (typeof value !== "string" || !value.length)
		throw new Error("Expected a nonempty string config value.");
	return value;
}

function positiveInteger(value: unknown, fallback: number) {
	if (value === undefined) return fallback;
	if (typeof value !== "number" || !Number.isInteger(value) || value <= 0)
		throw new Error("Expected a positive integer config value.");
	return value;
}
