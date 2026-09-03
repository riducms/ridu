import { createClient } from "@riducms/sdk";

interface Contract<Output, Create> {
	auth: false;
	upload: false;
	versions: false;
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

declare module "@riducms/sdk" {
	interface GeneratedRiduConfigRegistry {
		"manifest-content-primary": ContentConfig;
		"manifest-content-secondary": ContentConfig;
	}
}

// More than one generated manifest is intentionally not selected as the raw SDK default, even
// when two manifests happen to generate the same TypeScript config shape.
const unbound = createClient({ baseURL: "https://cms.example.test" });
// @ts-expect-error choose a generated wrapper or pass an explicit config when manifests coexist.
void unbound.list("posts");

const content = createClient<ContentConfig>({ baseURL: "https://cms.example.test" });
void content.create("posts", { title: "Explicit content config" });

// @ts-expect-error explicit configs retain their exact collection set.
void content.list("comments");
