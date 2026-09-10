import { createClient, type PagesCreate } from "../generated/ridu.generated";

const client = createClient({ baseURL: process.env.RIDU_URL ?? "http://localhost:8080" });
const asset = await client.create("assets", {
	title: "Mountains",
	url: "/mountains.svg",
});
const input: PagesCreate = {
	title: "A composed page",
	layout: [
		{ blockType: "hero", heading: "Hello", appearance: { tone: "dark" } },
		{
			blockType: "content",
			title: "Story",
			body: {
				version: 1,
				root: {
					type: "root",
					children: [
						{
							type: "paragraph",
							children: [{ type: "text", text: "The story continues in rich text.", format: 1 }],
						},
					],
				},
			},
			links: [{ label: "About", href: "/about" }],
		},
		{ blockType: "media", asset: asset.id, caption: "Mountains" },
		{ blockType: "cta", label: "Read more" },
	],
};
export const page = await client.create("pages", input);
console.log(
	page.id,
	page.layout?.map((block) => block._key)
);
// Render the returned single-locale layout with render/Blocks.svelte.
await client.publish("pages", page.id, { revision: page._revision });
const read = await client.find("pages", page.id);
if (read.layout?.length !== 4 || read.layout.some((block) => !block._key)) {
	throw new Error("Created layout did not round-trip with stable identities");
}
const all = await client.find("pages", page.id, { locale: "all" });
for (const block of all.layout ?? []) {
	if (block.blockType === "hero" && block.heading?.en !== "Hello") {
		throw new Error("Localized heading did not round-trip");
	}
}
const populated = await client.find("pages", page.id, {
	populate: { "layout.media.asset": true },
});
for (const block of populated.layout ?? []) {
	if (
		block.blockType === "media" &&
		(!block.asset || typeof block.asset === "string" || block.asset.id !== asset.id)
	) {
		throw new Error("Asset population did not return a typed document");
	}
}
