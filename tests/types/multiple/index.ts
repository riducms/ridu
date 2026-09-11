import { createClient } from "@riducms/sdk";

interface Contract<Output, Create> {
	auth: false;
	upload: false;
	versions: false;
	drafts: false;
	trash: false;
	output: Output;
	create: Create;
	update: Partial<Create>;
	where: Record<string, unknown>;
	select: Record<string, boolean>;
	populate: Record<never, never>;
}

interface ContentConfig {
	collections: {
		posts: Contract<{ id: string; title: string }, { title: string }>;
	};
}

// Raw SDK clients require an explicit application contract.
const unbound = createClient({ baseURL: "https://cms.example.test" });
// @ts-expect-error choose a generated wrapper or pass an explicit config when manifests coexist.
void unbound.list("posts");

const content = createClient<ContentConfig>({ baseURL: "https://cms.example.test" });
void content.create("posts", { title: "Explicit content config" });

// @ts-expect-error explicit configs retain their exact collection set.
void content.list("comments");

interface EditorialConfig {
	collections: { articles: Contract<{ id: string; headline: string }, { headline: string }> };
}
const editorial = createClient<EditorialConfig>({ baseURL: "https://editorial.example.test" });
void editorial.create("articles", { headline: "Another application" });
// @ts-expect-error one client cannot use another application's resource contract.
void editorial.create("posts", { title: "Wrong application" });
