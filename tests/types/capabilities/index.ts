import { createClient } from "@riducms/sdk";

type Collection<
	Output,
	Create,
	Auth extends boolean,
	Upload extends boolean,
	Versions extends boolean,
	Trash extends boolean,
	Drafts extends boolean = Versions,
> = {
	auth: Auth;
	upload: Upload;
	versions: Versions;
	drafts: Drafts;
	trash: Trash;
	output: Output;
	create: Create;
	update: Partial<Create>;
	where: Record<string, unknown>;
	select: Record<string, boolean>;
	populate: Record<never, never>;
};

type Global<Output, Update, Versions extends boolean, Drafts extends boolean = Versions> = {
	versions: Versions;
	drafts: Drafts;
	output: Output;
	update: Update;
	select: Record<string, boolean>;
	populate: Record<never, never>;
};

interface CapabilityConfig {
	collections: {
		users: Collection<{ id: string; email: string }, { email: string }, true, false, false, false>;
		media: Collection<
			{ id: string; alt: string | null },
			{ alt?: string | null },
			false,
			true,
			false,
			true
		>;
		posts: Collection<
			{ id: string; title: string; _revision: number },
			{ id?: string; title: string },
			false,
			false,
			true,
			false
		>;
		history: Collection<{ id: string }, {}, false, false, true, false, false>;
		pages: Collection<{ id: string; title: string }, { title: string }, false, false, false, false>;
	};
	globals: {
		"site-settings": Global<{ id: string; siteName: string }, { siteName?: string }, true>;
		history: Global<{ id: string }, {}, true, false>;
		navigation: Global<{ id: string; label: string }, { label?: string }, false>;
	};
}

const client = createClient<CapabilityConfig>({ baseURL: "https://cms.example.test" });

void client.login("users", { email: "editor@example.test", password: "secret" });
void client.upload("media", new Blob(), { data: { alt: "Diagram" } });
void client.versions("posts", "post-1");
void client.publish("posts", "post-1");
void client.create("posts", { id: "payload-42", title: "Imported" });
void client.duplicate("posts", "post-1", { title: "Copy" });
void client.restoreDeleted("media", "media-1");
void client.globalVersions("site-settings");
void client.publishGlobal("site-settings");

// @ts-expect-error only auth-enabled collections accept login.
void client.login("posts", { email: "editor@example.test", password: "secret" });

// @ts-expect-error only upload-enabled collections accept blobs.
void client.upload("posts", new Blob());

// @ts-expect-error only version-enabled collections expose history.
void client.versions("pages", "page-1");

// @ts-expect-error only trash-enabled collections can restore deleted documents.
void client.restoreDeleted("posts", "post-1");

// @ts-expect-error only version-enabled globals expose history.
void client.globalVersions("navigation");

// @ts-expect-error generated create inputs remain exact with an explicit application contract.
void client.create("posts", {});

// @ts-expect-error duplicate overrides never select the destination document ID.
void client.duplicate("posts", "post-1", { id: "copy-id" });

void client.versions("history", "id");
void client.globalVersions("history");
// @ts-expect-error historical versions do not enable collection draft publication.
void client.unpublish("history", "id");
// @ts-expect-error historical versions do not enable global draft publication.
void client.unpublishGlobal("history");
