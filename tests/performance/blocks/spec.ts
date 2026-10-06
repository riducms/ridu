// Framework-neutral schema and document generator for the block-heavy Ridu/Payload benchmark.
// Both fixtures consume the same JSON spec: the Ridu fixture builds `ridu.Config` from it and the
// Payload fixture builds its `buildConfig` input from it, so the compared schemas are equivalent.

export type FieldSpec =
	| { type: "text" | "textarea" | "number" | "checkbox" | "date"; name: string; required?: boolean }
	| { type: "select"; name: string; options: string[] }
	| { type: "relationship"; name: string; relationTo: string }
	| { type: "group"; name: string; fields: FieldSpec[] }
	| { type: "array"; name: string; fields: FieldSpec[] }
	| { type: "blocks"; name: string; blocks: string[] };

export type BlockSpec = { slug: string; fields: FieldSpec[] };
export type ResourceSpec = { slug: string; drafts: boolean; fields: FieldSpec[] };

export type Spec = {
	version: 1;
	scenario: string;
	description: string;
	/** true: `blocks` fields reference the config-level registry by slug; false: inline copies. */
	references: boolean;
	/**
	 * Ridu only: the first text field of every leaf block (a block without nested blocks) carries a
	 * beforeChange hook, a validator, field access rules, a dynamic default and a visibility
	 * condition. None of them changes the workload's content.
	 */
	hooks: boolean;
	blocks: BlockSpec[];
	collections: ResourceSpec[];
	globals: ResourceSpec[];
	workload: {
		collection: string;
		layoutField: string;
		topLevelBlocks: number;
		maxDepth: number;
		targetInstances: number;
	};
};

export type ScenarioName = "A" | "A-full" | "B" | "B-dense" | "C" | "C-over";
export const scenarioNames: ScenarioName[] = ["A", "A-full", "B", "B-dense", "C", "C-over"];

const select = (name: string, options: string[]): FieldSpec => ({ type: "select", name, options });
const text = (name: string, required = false): FieldSpec => ({ type: "text", name, required });
const textarea = (name: string, required = false): FieldSpec => ({
	type: "textarea",
	name,
	required,
});
const number = (name: string): FieldSpec => ({ type: "number", name });
const checkbox = (name: string): FieldSpec => ({ type: "checkbox", name });
const relationship = (name: string): FieldSpec => ({
	type: "relationship",
	name,
	relationTo: "media",
});
const group = (name: string, fields: FieldSpec[]): FieldSpec => ({ type: "group", name, fields });
const array = (name: string, fields: FieldSpec[]): FieldSpec => ({ type: "array", name, fields });
const blocks = (name: string, slugs: string[]): FieldSpec => ({
	type: "blocks",
	name,
	blocks: slugs,
});

// ---------------------------------------------------------------------------------------------
// Scenario A: a faithful port of the payloadcms/payload#17214 reproduction
// (evelynhathaway/payload@evelyn/block-schema-memory-repro, test/_community/blocks/index.ts and
// config.ts). Deviations: slugs are kebab-case because Ridu requires them, the `richText` block's
// Lexical field is a textarea on both sides, and `media` is an ordinary (non-upload) collection.

function issue17214Blocks(): BlockSpec[] {
	const sizes = ["none", "small", "medium", "large"];
	const spacing = group("spacing", [
		select("paddingTop", sizes),
		select("paddingBottom", sizes),
		select("marginTop", sizes),
		select("marginBottom", sizes),
	]);
	const color = group("color", [text("background"), text("foreground"), text("accent")]);
	const alignment = group("alignment", [
		select("horizontal", ["left", "center", "right"]),
		select("vertical", ["top", "middle", "bottom"]),
	]);
	const styling = [spacing, color, alignment];
	const children = (slugs: string[]) => blocks("children", slugs);
	const containerChildren = [
		"heading",
		"tagline",
		"rich-text",
		"quote",
		"image",
		"embed",
		"video",
		"button",
		"card-grid",
		"feature-grid",
		"card",
		"dual-card",
		"grid",
		"split-text",
		"accordion",
		"disclosure-list",
		"metric-list",
		"spec-list",
		"comparison",
		"spacing-container",
		"panel",
		"divider",
	];
	const panelChildren = containerChildren.filter(
		(slug) => slug !== "spacing-container" && slug !== "panel"
	);
	return [
		{ slug: "container", fields: [...styling, children(containerChildren)] },
		{
			slug: "spacing-container",
			fields: [
				...styling,
				children(containerChildren.filter((slug) => slug !== "spacing-container")),
			],
		},
		{ slug: "panel", fields: [...styling, children(panelChildren)] },
		{
			slug: "hero",
			fields: [
				...styling,
				children([
					"heading",
					"tagline",
					"rich-text",
					"quote",
					"image",
					"embed",
					"video",
					"button",
					"card-grid",
					"feature-grid",
					"card",
					"grid",
					"split-text",
					"accordion",
					"disclosure-list",
					"metric-list",
					"spec-list",
					"spacing-container",
					"divider",
				]),
			],
		},
		{
			slug: "banner",
			fields: [
				...styling,
				children([
					"heading",
					"tagline",
					"rich-text",
					"quote",
					"image",
					"embed",
					"video",
					"button",
					"card-grid",
					"feature-grid",
					"card",
					"grid",
					"split-text",
					"spacing-container",
					"divider",
				]),
			],
		},
		{
			slug: "timeline",
			fields: [
				...styling,
				children([
					"heading",
					"rich-text",
					"button",
					"image",
					"embed",
					"video",
					"timeline-item",
					"container",
					"hero",
					"banner",
					"carousel",
				]),
			],
		},
		{
			slug: "timeline-item",
			fields: [children(["heading", "rich-text", "image", "button", "badge"])],
		},
		{
			slug: "tabs",
			fields: [
				...styling,
				children(["container", "hero", "banner", "timeline", "carousel", "heading", "tab-item"]),
			],
		},
		{
			slug: "tab-item",
			fields: [
				children(["container", "card", "rich-text", "heading", "image", "button", "accordion"]),
			],
		},
		{ slug: "carousel", fields: [...styling, children(["slide"])] },
		{ slug: "slide", fields: [children(["heading", "rich-text", "image", "button", "badge"])] },
		{ slug: "card-grid", fields: [...styling, children(["card", "dual-card"])] },
		{
			slug: "card",
			fields: [
				...styling,
				children(["heading", "image", "embed", "video", "rich-text", "date", "button"]),
			],
		},
		{ slug: "dual-card", fields: [children(["heading", "image", "embed", "video", "button"])] },
		{ slug: "grid", fields: [...styling, children(["image", "embed", "video", "card"])] },
		{ slug: "feature-grid", fields: [...styling, children(["feature"])] },
		{ slug: "feature", fields: [children(["heading", "icon", "button"])] },
		{
			slug: "split-text",
			fields: [
				...styling,
				children(["heading", "tagline", "rich-text", "quote", "button", "divider"]),
			],
		},
		{ slug: "accordion", fields: [children(["heading", "rich-text", "accordion-item"])] },
		{ slug: "accordion-item", fields: [children(["rich-text", "heading", "image", "button"])] },
		{ slug: "disclosure-list", fields: [children(["definition-list"])] },
		{ slug: "definition-list", fields: [children(["rich-text", "heading"])] },
		{ slug: "metric-list", fields: [children(["metric-group"])] },
		{ slug: "metric-group", fields: [children(["metric"])] },
		{ slug: "spec-list", fields: [children(["spec"])] },
		{ slug: "spec", fields: [children(["display-text"])] },
		{ slug: "comparison", fields: [children(["comparison-column"])] },
		{ slug: "comparison-column", fields: [children(["comparison-cell"])] },
		{
			slug: "heading",
			fields: [
				select("level", ["h1", "h2", "h3", "h4"]),
				children(["display-text", "kicker", "image"]),
			],
		},
		{ slug: "tagline", fields: [children(["display-text", "kicker"])] },
		{ slug: "display-text", fields: [text("text"), select("size", ["sm", "md", "lg", "xl"])] },
		{ slug: "kicker", fields: [text("text")] },
		{ slug: "rich-text", fields: [textarea("body")] },
		{ slug: "quote", fields: [textarea("text"), text("attribution")] },
		{ slug: "image", fields: [text("src"), text("alt"), alignment] },
		{ slug: "embed", fields: [text("url")] },
		{ slug: "video", fields: [text("url"), text("poster")] },
		{
			slug: "button",
			fields: [text("label"), text("href"), select("variant", ["primary", "secondary", "ghost"])],
		},
		{ slug: "divider", fields: [select("style", ["solid", "dashed", "dotted"])] },
		{ slug: "badge", fields: [text("text"), color] },
		{ slug: "icon", fields: [text("name")] },
		{ slug: "date", fields: [{ type: "date", name: "value" }] },
		{ slug: "metric", fields: [text("value"), text("label")] },
		{ slug: "comparison-cell", fields: [text("value")] },
	];
}

// `full` keeps the reproduction's six layout collections (pages has two layout fields). Without it,
// the scenario uses the "~35 blocks and 3 collections" shape the reproduction describes.
function issue17214(full: boolean): Omit<Spec, "references" | "hooks"> {
	const roots = ["container", "hero", "banner", "timeline", "tabs", "carousel"];
	const layoutCollection = (slug: string, layoutFields: number): ResourceSpec => ({
		slug,
		drafts: true,
		fields: [
			text("title", true),
			...Array.from({ length: layoutFields }, (_, index) =>
				blocks(index === 0 ? "layout" : `layout${index + 1}`, roots)
			),
		],
	});
	return {
		version: 1,
		scenario: full ? "A-full" : "A",
		description: full
			? "payloadcms/payload#17214 reproduction config: 44 registry blocks, 6 drafts-enabled layout collections (pages has two layout fields)"
			: "payloadcms/payload#17214 reproduction graph: 44 registry blocks, 3 drafts-enabled layout collections",
		blocks: issue17214Blocks(),
		collections: [
			layoutCollection("pages", full ? 2 : 1),
			layoutCollection("sections", 1),
			layoutCollection("landing-pages", 1),
			...(full
				? [
						layoutCollection("marketing-pages", 1),
						layoutCollection("campaigns", 1),
						layoutCollection("guides", 1),
					]
				: []),
			{ slug: "posts", drafts: false, fields: [text("title", true), textarea("content")] },
			mediaCollection(),
		],
		globals: [{ slug: "menu", drafts: false, fields: [text("globalText")] }],
		workload: {
			collection: "pages",
			layoutField: "layout",
			topLevelBlocks: 50,
			maxDepth: 4,
			targetInstances: 400,
		},
	};
}

function mediaCollection(): ResourceSpec {
	return { slug: "media", drafts: false, fields: [text("title", true), text("alt")] };
}

// ---------------------------------------------------------------------------------------------
// Generic layered layout builder used by scenarios B and C: realistic leaf blocks (text,
// textarea, select, checkbox, number, relationship, nested arrays) and container blocks whose
// `content` field references every leaf plus every container in a strictly lower layer.

const leafTemplates: BlockSpec[] = [
	{ slug: "rich-text", fields: [textarea("body", true), checkbox("dropCap")] },
	{
		slug: "heading",
		fields: [text("text", true), select("level", ["h1", "h2", "h3", "h4"]), text("anchor")],
	},
	{
		slug: "image",
		fields: [
			relationship("image"),
			text("caption"),
			text("alt"),
			select("width", ["narrow", "wide", "full"]),
			checkbox("rounded"),
		],
	},
	{
		slug: "video",
		fields: [
			text("url", true),
			text("caption"),
			checkbox("autoplay"),
			select("aspect", ["16-9", "4-3", "1-1"]),
		],
	},
	{
		slug: "quote",
		fields: [
			textarea("quote", true),
			text("attribution"),
			text("role"),
			select("style", ["plain", "large", "pull"]),
		],
	},
	{
		slug: "cta",
		fields: [
			text("heading"),
			textarea("body"),
			array("buttons", [
				text("label", true),
				text("url"),
				select("variant", ["primary", "secondary", "ghost"]),
				checkbox("newTab"),
			]),
		],
	},
	{
		slug: "stats",
		fields: [text("heading"), array("items", [number("value"), text("label"), text("suffix")])],
	},
	{
		slug: "testimonial",
		fields: [
			textarea("quote", true),
			text("author"),
			text("role"),
			relationship("avatar"),
			number("rating"),
		],
	},
	{
		slug: "pricing-table",
		fields: [
			text("heading"),
			array("plans", [
				text("name", true),
				number("price"),
				select("period", ["month", "year"]),
				checkbox("featured"),
			]),
		],
	},
	{
		slug: "faq",
		fields: [text("heading"), array("items", [text("question", true), textarea("answer")])],
	},
	{
		slug: "logo-cloud",
		fields: [text("heading"), array("logos", [relationship("logo"), text("label"), text("url")])],
	},
	{
		slug: "code",
		fields: [
			select("language", ["go", "ts", "sql", "shell"]),
			textarea("code", true),
			text("caption"),
		],
	},
	{ slug: "embed", fields: [text("url", true), number("height"), text("title")] },
	{
		slug: "form-embed",
		fields: [text("formId", true), text("heading"), text("submitLabel"), text("redirectUrl")],
	},
	{ slug: "map", fields: [number("lat"), number("lng"), number("zoom"), text("label")] },
	{
		slug: "spacer",
		fields: [select("size", ["small", "medium", "large"]), checkbox("showDivider")],
	},
	{
		slug: "button",
		fields: [
			text("label", true),
			text("url"),
			select("variant", ["primary", "secondary", "ghost"]),
			checkbox("newTab"),
			text("icon"),
		],
	},
	{
		slug: "list",
		fields: [
			select("style", ["bullet", "numbered", "check"]),
			array("items", [text("text", true), textarea("note")]),
		],
	},
	{
		slug: "table",
		fields: [text("caption"), array("rows", [text("label"), text("value"), checkbox("highlight")])],
	},
	{
		slug: "gallery",
		fields: [array("images", [relationship("image"), text("caption")]), number("columns")],
	},
	{
		slug: "banner",
		fields: [
			text("text", true),
			select("tone", ["info", "warning", "success"]),
			checkbox("dismissible"),
			text("link"),
		],
	},
	{ slug: "audio", fields: [text("url", true), text("title"), textarea("transcript")] },
	{ slug: "download", fields: [relationship("file"), text("label"), number("size")] },
	{ slug: "author-bio", fields: [relationship("author"), checkbox("showAvatar"), textarea("bio")] },
];

const containerNames = [
	"column",
	"card",
	"tab-item",
	"accordion-item",
	"slide",
	"panel",
	"columns",
	"tabs",
	"accordion",
	"card-grid",
	"carousel",
	"grid",
	"section",
	"container",
	"split",
	"stack",
	"feature-grid",
	"timeline",
	"hero",
	"band",
];

function containerFields(children: string[]): FieldSpec[] {
	return [
		text("anchor"),
		select("background", ["none", "muted", "accent", "inverse"]),
		checkbox("fullWidth"),
		group("spacing", [
			select("paddingTop", ["none", "small", "medium", "large"]),
			select("paddingBottom", ["none", "small", "medium", "large"]),
		]),
		blocks("content", children),
	];
}

export function layeredBlocks(leafCount: number, layers: number[]): BlockSpec[] {
	if (leafCount > leafTemplates.length) throw new Error(`at most ${leafTemplates.length} leaves`);
	const leaves = leafTemplates.slice(0, leafCount);
	const result: BlockSpec[] = [];
	const lower: string[] = [];
	let nameIndex = 0;
	for (const width of layers) {
		const layer: string[] = [];
		for (let index = 0; index < width; index += 1) {
			const slug = containerNames[nameIndex] ?? `container-${nameIndex}`;
			nameIndex += 1;
			result.push({
				slug,
				fields: containerFields([...leaves.map((leaf) => leaf.slug), ...lower]),
			});
			layer.push(slug);
		}
		lower.push(...layer);
	}
	return [...result, ...leaves];
}

// A typical site builder: 20 leaves, five item containers (column, card, tab, accordion item,
// slide) that hold any leaf, and three wrappers (columns, tabs, accordion) that hold their item.
function typicalBlocks(): BlockSpec[] {
	const leaves = leafTemplates.slice(0, 20);
	const leafSlugs = leaves.map((leaf) => leaf.slug);
	const items = ["column", "card", "tab-item", "accordion-item", "slide"];
	const wrappers: Array<[string, string]> = [
		["columns", "column"],
		["tabs", "tab-item"],
		["accordion", "accordion-item"],
	];
	return [
		...wrappers.map(([slug, item]) => ({ slug, fields: containerFields([item]) })),
		...items.map((slug) => ({ slug, fields: containerFields(leafSlugs) })),
		...leaves,
	];
}

function secondReport(dense: boolean): Omit<Spec, "references" | "hooks"> {
	const registry = dense ? layeredBlocks(18, [5, 3, 2]) : typicalBlocks();
	const slugs = registry.map((block) => block.slug);
	const collections: ResourceSpec[] = [];
	// 39 collections: 34 drafts-enabled page-like collections with a layout, five plain ones.
	for (let index = 0; index < 34; index += 1) {
		collections.push({
			slug: `content-${String(index).padStart(2, "0")}`,
			drafts: true,
			fields: [text("title", true), textarea("summary"), blocks("layout", slugs)],
		});
	}
	collections.push(mediaCollection());
	for (const slug of ["categories", "tags", "redirects", "forms"]) {
		collections.push({ slug, drafts: false, fields: [text("title", true), text("note")] });
	}
	return {
		version: 1,
		scenario: dense ? "B-dense" : "B",
		description: dense
			? "Second report, dense graph: 39 collections, 28 registry blocks (18 leaves, containers in layers 5/3/2 referencing every lower block), 34 drafts-enabled collections referencing every block"
			: "Second report shape: 39 collections, 28 registry blocks (20 leaves, 5 item containers, 3 wrappers), 34 drafts-enabled collections referencing every block",
		blocks: registry,
		collections,
		globals: [{ slug: "site", drafts: false, fields: [text("siteName"), blocks("footer", slugs)] }],
		workload: {
			collection: "content-00",
			layoutField: "layout",
			topLevelBlocks: 50,
			maxDepth: 4,
			targetInstances: 400,
		},
	};
}

// Scenario C's generic layout builder: 18 leaves and six container layers, 96,955 field placements
// for its one layout collection. It was sized as the largest graph of its family that Ridu's former
// 100,000-placement budget accepted; Ridu's cost now follows the 32 definitions. C-over shares the
// same registry with two more layout collections, as sites with several page-like collections do.
export const deepLayers = { leaves: 18, layers: [4, 3, 3, 2, 1, 1] };

function deepDag(over: boolean): Omit<Spec, "references" | "hooks"> {
	const registry = layeredBlocks(deepLayers.leaves, deepLayers.layers);
	const slugs = registry.map((block) => block.slug);
	const layoutCollections = over ? ["pages", "articles", "landing-pages"] : ["pages"];
	const collections: ResourceSpec[] = layoutCollections.map((slug) => ({
		slug,
		drafts: true,
		fields: [text("title", true), textarea("summary"), blocks("layout", slugs)],
	}));
	collections.push(mediaCollection());
	return {
		version: 1,
		scenario: over ? "C-over" : "C",
		description: `Deep layered DAG: ${deepLayers.leaves} leaves, container layers ${deepLayers.layers.join("/")}, each container referencing every leaf and every lower container; ${layoutCollections.length} drafts-enabled layout collection(s)`,
		blocks: registry,
		collections,
		globals: [],
		workload: {
			collection: "pages",
			layoutField: "layout",
			topLevelBlocks: 50,
			maxDepth: 4,
			targetInstances: 400,
		},
	};
}

export function buildSpec(scenario: ScenarioName, references: boolean, hooks = false): Spec {
	const base =
		scenario === "A" || scenario === "A-full"
			? issue17214(scenario === "A-full")
			: scenario === "B" || scenario === "B-dense"
				? secondReport(scenario === "B-dense")
				: deepDag(scenario === "C-over");
	return { ...base, references, hooks };
}

// ---------------------------------------------------------------------------------------------
// Static graph statistics.

export function blockIndex(spec: Spec): Map<string, BlockSpec> {
	return new Map(spec.blocks.map((block) => [block.slug, block]));
}

/**
 * Field placements as Ridu counts them for one field list. Ridu adds an implicit `blockName` text
 * field to every block, so each block placement contributes one field beyond its declared ones.
 */
export function riduPlacements(spec: Spec, fields: FieldSpec[]): number {
	const index = blockIndex(spec);
	const memo = new Map<string, number>();
	const count = (list: FieldSpec[]): number => {
		let total = 0;
		for (const field of list) {
			total += 1;
			if (field.type === "group" || field.type === "array") total += count(field.fields);
			if (field.type === "blocks") {
				for (const slug of field.blocks) {
					let value = memo.get(slug);
					if (value === undefined) {
						value = 1 + count(index.get(slug)!.fields);
						memo.set(slug, value);
					}
					total += value;
				}
			}
		}
		return total;
	};
	return count(fields);
}

/** Block placements (block occurrences along every path) beneath one field list. */
export function blockPlacements(spec: Spec, fields: FieldSpec[]): number {
	const index = blockIndex(spec);
	const memo = new Map<string, number>();
	const count = (list: FieldSpec[]): number => {
		let total = 0;
		for (const field of list) {
			if (field.type === "group" || field.type === "array") total += count(field.fields);
			if (field.type === "blocks") {
				for (const slug of field.blocks) {
					let value = memo.get(slug);
					if (value === undefined) {
						value = 1 + count(index.get(slug)!.fields);
						memo.set(slug, value);
					}
					total += value;
				}
			}
		}
		return total;
	};
	return count(fields);
}

export function maxBlockDepth(spec: Spec): number {
	const index = blockIndex(spec);
	const memo = new Map<string, number>();
	const depth = (slug: string): number => {
		const cached = memo.get(slug);
		if (cached !== undefined) return cached;
		let deepest = 0;
		const visit = (list: FieldSpec[]) => {
			for (const field of list) {
				if (field.type === "group" || field.type === "array") visit(field.fields);
				if (field.type === "blocks")
					for (const child of field.blocks) deepest = Math.max(deepest, depth(child));
			}
		};
		visit(index.get(slug)!.fields);
		memo.set(slug, deepest + 1);
		return deepest + 1;
	};
	return Math.max(...spec.blocks.map((block) => depth(block.slug)));
}

export function specStatistics(spec: Spec) {
	const perResource = [...spec.collections, ...spec.globals].map((resource) => ({
		slug: resource.slug,
		drafts: resource.drafts,
		riduPlacements: riduPlacements(spec, resource.fields),
		blockPlacements: blockPlacements(spec, resource.fields),
	}));
	const edges = spec.blocks.reduce(
		(total, block) =>
			total +
			block.fields.reduce(
				(sum, field) => sum + (field.type === "blocks" ? field.blocks.length : 0),
				0
			),
		0
	);
	return {
		blocks: spec.blocks.length,
		referenceEdges: edges,
		maxBlockNesting: maxBlockDepth(spec),
		collections: spec.collections.length,
		draftsCollections: spec.collections.filter((collection) => collection.drafts).length,
		globals: spec.globals.length,
		maxResourceRiduPlacements: Math.max(...perResource.map((resource) => resource.riduPlacements)),
		totalRiduPlacements: perResource.reduce((sum, resource) => sum + resource.riduPlacements, 0),
		totalBlockPlacements: perResource.reduce((sum, resource) => sum + resource.blockPlacements, 0),
		perResource,
	};
}

// ---------------------------------------------------------------------------------------------
// Deterministic block-heavy document generation and response canonicalization.

/** A relationship placeholder; runners substitute each framework's real media ID. */
export type MediaRef = { $media: number };
export type LogicalValue =
	string | number | boolean | null | MediaRef | LogicalValue[] | { [key: string]: LogicalValue };

function prng(seed: number): () => number {
	let state = seed >>> 0 || 1;
	return () => {
		state ^= state << 13;
		state ^= state >>> 17;
		state ^= state << 5;
		return (state >>> 0) / 4294967296;
	};
}

const words =
	"layout section feature content block nested page editor marketing campaign launch product guide reference story signal quality design system pricing customer support release update".split(
		" "
	);

function sentence(random: () => number, minimum: number, maximum: number): string {
	const length = minimum + Math.floor(random() * (maximum - minimum + 1));
	const out: string[] = [];
	for (let index = 0; index < length; index += 1)
		out.push(words[Math.floor(random() * words.length)]!);
	return out.join(" ");
}

function fieldValue(
	field: FieldSpec,
	random: () => number,
	mediaCount: number,
	path: string
): LogicalValue | undefined {
	switch (field.type) {
		case "text":
			return `${sentence(random, 2, 6)} ${path}`;
		case "textarea":
			return sentence(random, 20, 45);
		case "number":
			return Math.floor(random() * 1000);
		case "checkbox":
			return random() < 0.5;
		case "date":
			return new Date(
				Date.UTC(2026, Math.floor(random() * 12), 1 + Math.floor(random() * 27))
			).toISOString();
		case "select":
			return field.options[Math.floor(random() * field.options.length)];
		case "relationship":
			return { $media: Math.floor(random() * mediaCount) };
		case "group": {
			const value: Record<string, LogicalValue> = {};
			for (const child of field.fields) {
				const childValue = fieldValue(child, random, mediaCount, `${path}.${child.name}`);
				if (childValue !== undefined) value[child.name] = childValue;
			}
			return value;
		}
		case "array": {
			const rows: LogicalValue[] = [];
			const count = 2 + Math.floor(random() * 2);
			for (let row = 0; row < count; row += 1) {
				const value: Record<string, LogicalValue> = {};
				for (const child of field.fields) {
					const childValue = fieldValue(child, random, mediaCount, `${path}.${row}.${child.name}`);
					if (childValue !== undefined) value[child.name] = childValue;
				}
				rows.push(value);
			}
			return rows;
		}
		case "blocks":
			return undefined;
	}
}

/**
 * Builds `topLevelBlocks` blocks for the workload layout field. Two of every three top-level
 * positions prefer containers, and each top-level container receives an equal share of the
 * remaining instance target, nested down to `maxDepth` levels. Leaves fill every field.
 */
export function generateLayout(spec: Spec, seed: number, mediaCount: number): LogicalValue[] {
	const index = blockIndex(spec);
	const random = prng(seed * 7919 + 17);
	const workload = spec.workload;
	const collection = spec.collections.find((resource) => resource.slug === workload.collection)!;
	const layoutField = collection.fields.find((field) => field.name === workload.layoutField);
	if (!layoutField || layoutField.type !== "blocks")
		throw new Error("workload layout field missing");
	const containerMemo = new Map<string, boolean>();
	const isContainer = (slug: string) => {
		let value = containerMemo.get(slug);
		if (value === undefined) {
			value = index.get(slug)!.fields.some(hasNestedBlocks);
			containerMemo.set(slug, value);
		}
		return value;
	};
	const choose = (slugs: string[], preferContainer: boolean) => {
		const containers = slugs.filter(isContainer);
		const leaves = slugs.filter((slug) => !isContainer(slug));
		const pool =
			preferContainer && containers.length > 0
				? containers
				: leaves.length > 0
					? leaves
					: containers;
		return pool[Math.floor(random() * pool.length)]!;
	};
	const topLevel: string[] = [];
	for (let position = 0; position < workload.topLevelBlocks; position += 1) {
		topLevel.push(choose(layoutField.blocks, position % 3 !== 2));
	}
	const topContainers = topLevel.filter(isContainer).length;
	const share = Math.max(
		0,
		Math.floor((workload.targetInstances - workload.topLevelBlocks) / Math.max(1, topContainers))
	);
	let budget = 0;
	const build = (slug: string, depth: number, path: string): LogicalValue => {
		const block = index.get(slug)!;
		const value: Record<string, LogicalValue> = { blockType: slug };
		for (const field of block.fields) {
			if (field.type === "blocks") {
				const items: LogicalValue[] = [];
				if (depth < workload.maxDepth) {
					const wanted = 3;
					for (let child = 0; child < wanted && budget > 0; child += 1) {
						const pick = choose(field.blocks, depth < workload.maxDepth - 1 && child % 3 !== 2);
						budget -= 1;
						items.push(build(pick, depth + 1, `${path}.${field.name}.${child}`));
					}
				}
				value[field.name] = items;
				continue;
			}
			const fieldResult = fieldValue(field, random, mediaCount, `${path}.${field.name}`);
			if (fieldResult !== undefined) value[field.name] = fieldResult;
		}
		return value;
	};
	return topLevel.map((slug, position) => {
		budget = isContainer(slug) ? share : 0;
		return build(slug, 1, `layout.${position}`);
	});
}

function hasNestedBlocks(field: FieldSpec): boolean {
	if (field.type === "blocks") return true;
	if (field.type === "group" || field.type === "array") return field.fields.some(hasNestedBlocks);
	return false;
}

export function countInstances(layout: unknown): number {
	if (!Array.isArray(layout)) return 0;
	let total = 0;
	for (const item of layout) {
		total += 1;
		if (item && typeof item === "object") {
			for (const value of Object.values(item)) {
				if (Array.isArray(value)) total += countInstances(value.filter(isBlock));
			}
		}
	}
	return total;
}

function isBlock(value: unknown): boolean {
	return Boolean(value && typeof value === "object" && "blockType" in (value as object));
}

/** Replaces media placeholders with a framework's IDs, preserving everything else. */
export function resolveMedia(value: LogicalValue, mediaIDs: Array<string | number>): unknown {
	if (Array.isArray(value)) return value.map((item) => resolveMedia(item, mediaIDs));
	if (value && typeof value === "object") {
		if ("$media" in value) return mediaIDs[(value as MediaRef).$media];
		const out: Record<string, unknown> = {};
		for (const [key, child] of Object.entries(value)) out[key] = resolveMedia(child, mediaIDs);
		return out;
	}
	return value;
}

/**
 * Projects a returned or submitted layout onto the declared fields only, dropping framework
 * identity/metadata (`id`, `_key`, `blockName`), normalizing dates and relationship IDs, and
 * treating null and absent values alike. Equal canonical forms mean equivalent content.
 */
export function canonicalLayout(
	spec: Spec,
	layout: unknown,
	references = blockIndex(spec)
): unknown {
	if (!Array.isArray(layout)) return [];
	return layout.map((item) => {
		const record = item as Record<string, unknown>;
		const block = references.get(String(record.blockType));
		if (!block) return { blockType: record.blockType, unknown: true };
		return { blockType: block.slug, ...canonicalFields(spec, block.fields, record, references) };
	});
}

function canonicalFields(
	spec: Spec,
	fields: FieldSpec[],
	record: Record<string, unknown>,
	references: Map<string, BlockSpec>
): Record<string, unknown> {
	const out: Record<string, unknown> = {};
	for (const field of fields) {
		const value = record?.[field.name];
		if (value === undefined || value === null) continue;
		switch (field.type) {
			case "group":
				out[field.name] = canonicalFields(
					spec,
					field.fields,
					value as Record<string, unknown>,
					references
				);
				break;
			case "array":
				if (Array.isArray(value) && value.length === 0) break;
				out[field.name] = (value as Array<Record<string, unknown>>).map((row) =>
					canonicalFields(spec, field.fields, row, references)
				);
				break;
			case "blocks":
				if (Array.isArray(value) && value.length === 0) break;
				out[field.name] = canonicalLayout(spec, value, references);
				break;
			case "date":
				out[field.name] = new Date(String(value)).toISOString();
				break;
			case "relationship":
				out[field.name] = String(
					typeof value === "object" && value !== null && "id" in value
						? (value as { id: unknown }).id
						: value
				);
				break;
			default:
				out[field.name] = value;
		}
	}
	return out;
}

if (import.meta.main) {
	const [scenarioArgument, variant = "references", output] = Bun.argv.slice(2);
	const names = scenarioArgument ? [scenarioArgument as ScenarioName] : scenarioNames;
	for (const name of names) {
		if (!scenarioNames.includes(name)) throw new Error(`unknown scenario ${name}`);
		const spec = buildSpec(name, variant !== "inline", variant === "hooks");
		if (output) {
			await Bun.write(output, `${JSON.stringify(spec, null, "\t")}\n`);
		}
		const statistics = specStatistics(spec);
		const layout = generateLayout(spec, 1, 10);
		console.log(
			JSON.stringify(
				{
					scenario: name,
					...statistics,
					perResource: statistics.perResource.slice(0, 3),
					workloadInstances: countInstances(layout),
					workloadJSONBytes: JSON.stringify(layout).length,
				},
				null,
				2
			)
		);
	}
}
