import { createClient, type ClientOptions } from "@riducms/sdk";

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
		"draft-users": Collection<
			{ id: string; email: string; bio: string | null },
			{ email: string; bio: string },
			true,
			false,
			true,
			false
		> & { draftCreate: { email: string; bio?: string | null } };
		media: Collection<
			{ id: string; alt: string | null },
			{ alt?: string | null },
			false,
			true,
			false,
			true
		>;
		"required-media": Collection<
			{ id: string; alt: string },
			{ alt: string },
			false,
			true,
			false,
			false
		>;
		"editorial-media": Collection<
			{ id: string; alt: string | null },
			{ alt: string },
			false,
			true,
			true,
			false
		> & {
			draftCreate: { alt?: string | null };
			draftUpdate: { alt?: string | null };
		};
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
declare const uploadDraft: boolean;

void client.auth.login({ collection: "users", email: "editor@example.test", password: "secret" });
void client.auth.createUser({
	collection: "draft-users",
	data: { email: "editor@example.test" },
	password: "secret",
});
void client.auth.createUser(
	{
		collection: "draft-users",
		data: { email: "editor@example.test", bio: null },
		password: "secret",
	},
	{ draft: true }
);
void client.auth.createUser(
	{
		collection: "draft-users",
		data: { email: "editor@example.test", bio: "Complete" },
		password: "secret",
	},
	{ draft: false }
);
// @ts-expect-error publication requires complete user data.
void client.auth.createUser(
	{ collection: "draft-users", data: { email: "editor@example.test" }, password: "secret" },
	{ draft: false }
);
void client.auth.createUser({
	collection: "draft-users",
	// @ts-expect-error a draft auth identity is never optional.
	data: { bio: "Working" },
	password: "secret",
});
// @ts-expect-error credentials remain mandatory for new draft users.
void client.auth.createUser({ collection: "draft-users", data: { email: "editor@example.test" } });
// @ts-expect-error non-draft auth resources cannot request draft creation.
void client.auth.createUser(
	{ collection: "users", data: { email: "editor@example.test" }, password: "secret" },
	{ draft: true }
);
void client.upload("media", new Blob(), { data: { alt: "Diagram" } });
void client.upload("media", new Blob());
void client.upload("required-media", new Blob(), { data: { alt: "Complete" } });
void client.uploadFromURL("required-media", "https://example.test/image.png", {
	data: { alt: "Complete" },
});
void client.upload("editorial-media", new Blob());
void client.upload("editorial-media", new Blob(), { data: { alt: null } });
void client.upload("editorial-media", new Blob(), { draft: true });
void client.upload("editorial-media", new Blob(), { data: {} });
void client.uploadFromURL("editorial-media", "https://example.test/image.png", {
	data: { alt: null },
	draft: true,
});
void client.uploadFromURL("editorial-media", "https://example.test/image.png", { draft: true });
void client.upload("editorial-media", new Blob(), { data: { alt: "Complete" }, draft: false });
void client.uploadFromURL("editorial-media", "https://example.test/image.png", {
	data: { alt: "Complete" },
	draft: false,
});
void client.upload("editorial-media", new Blob(), {
	data: { alt: "Complete" },
	draft: uploadDraft,
});
void client.uploadFromURL("editorial-media", "https://example.test/image.png", {
	data: { alt: "Complete" },
	draft: uploadDraft,
});
void client.updateUpload("editorial-media", "image-1", { data: { alt: null } }, { draft: true });
// @ts-expect-error updates only accept draft=true; publication is a separate action.
void client.update("posts", "post-1", { title: "Complete" }, { draft: false });
// @ts-expect-error globals follow the same explicit staging contract.
void client.updateGlobal("site-settings", { siteName: "Complete" }, { draft: false });
void client.updateUpload(
	"editorial-media",
	"image-1",
	{ data: { alt: "Complete" } },
	// @ts-expect-error upload metadata updates cannot use draft=false for publication.
	{ draft: false }
);
// @ts-expect-error published upload inputs cannot clear required editorial fields.
void client.upload("editorial-media", new Blob(), { data: { alt: null }, draft: false });
// @ts-expect-error publishing an upload requires complete editorial metadata.
void client.upload("editorial-media", new Blob(), { draft: false });
// @ts-expect-error publishing an upload requires complete editorial metadata.
void client.upload("editorial-media", new Blob(), { draft: false, data: {} });
// @ts-expect-error a dynamic draft choice may publish, so required metadata cannot be omitted.
void client.upload("editorial-media", new Blob(), { draft: uploadDraft });
// @ts-expect-error a dynamic draft choice may publish, so required metadata cannot be omitted.
void client.uploadFromURL("editorial-media", "https://example.test/image.png", {
	draft: uploadDraft,
	data: {},
});
// @ts-expect-error a non-draft upload with required metadata cannot omit its data.
void client.upload("required-media", new Blob());
// @ts-expect-error a non-draft upload with required metadata cannot omit its fields.
void client.upload("required-media", new Blob(), { data: {} });
// @ts-expect-error remote non-draft uploads require their generated metadata.
void client.uploadFromURL("required-media", "https://example.test/image.png", { data: {} });
// @ts-expect-error remote publication requires complete editorial metadata.
void client.uploadFromURL("editorial-media", "https://example.test/image.png", { draft: false });
// @ts-expect-error remote publication requires complete editorial metadata.
void client.uploadFromURL("editorial-media", "https://example.test/image.png", {
	draft: false,
	data: {},
});
// @ts-expect-error upload creation has one publication selector: draft=false.
void client.upload("editorial-media", new Blob(), { data: { alt: "Complete" }, publish: true });
// @ts-expect-error clearing a required upload field requires explicit draft intent.
void client.updateUpload("editorial-media", "image-1", { data: { alt: null } });
// @ts-expect-error pending upload drafts cannot request publication simultaneously.
void client.updateUpload(
	"editorial-media",
	"image-1",
	{ data: { alt: null }, publish: true },
	{ draft: true }
);
// @ts-expect-error draft uploads still require a file.
void client.upload("editorial-media", { data: { alt: null } });
void client.versions("posts", "post-1");
void client.countVersions("posts", "post-1", { locale: "all", fallbackLocale: false });
void client.publish("posts", "post-1");
void client.create("posts", { id: "payload-42", title: "Imported" });
void client.duplicate("posts", "post-1", { title: "Copy" });
void client.restoreDeleted("media", "media-1");
void client.globalVersions("site-settings");
void client.countGlobalVersions("site-settings", { locale: "all" });
void client.publishGlobal("site-settings");

// @ts-expect-error only auth-enabled collections accept login.
void client.auth.login({ collection: "posts", email: "editor@example.test", password: "secret" });

// @ts-expect-error a client without auth.collection names the collection on each call.
void client.auth.login({ email: "editor@example.test", password: "secret" });

const memberOptions: ClientOptions<"users"> = {
	baseURL: "https://cms.example.test",
	auth: { collection: "users" },
};
const members = createClient<CapabilityConfig, "users">(memberOptions);
void members.auth.login({ email: "editor@example.test", password: "secret" });
void members.auth.getSession().then((session) => session?.collection satisfies "users" | undefined);

const draftMembers = createClient<CapabilityConfig, "draft-users">({
	baseURL: "https://cms.example.test",
	auth: { collection: "draft-users" },
});
void draftMembers.auth.createUser({ data: { email: "editor@example.test" }, password: "secret" });

// @ts-expect-error only upload-enabled collections accept blobs.
void client.upload("posts", new Blob());

// @ts-expect-error only version-enabled collections expose history.
void client.versions("pages", "page-1");

// @ts-expect-error only version-enabled collections expose history counts.
void client.countVersions("pages", "page-1");

// @ts-expect-error only trash-enabled collections can restore deleted documents.
void client.restoreDeleted("posts", "post-1");

// @ts-expect-error only version-enabled globals expose history.
void client.globalVersions("navigation");

// @ts-expect-error only version-enabled globals expose history counts.
void client.countGlobalVersions("navigation");

// @ts-expect-error generated create inputs remain exact with an explicit application contract.
void client.create("posts", {});

// @ts-expect-error duplicate overrides never select the destination document ID.
void client.duplicate("posts", "post-1", { id: "copy-id" });

void client.versions("history", "id");
void client.countVersions("history", "id");
void client.globalVersions("history");
void client.countGlobalVersions("history");
// @ts-expect-error historical versions do not enable collection draft publication.
void client.unpublish("history", "id");
// @ts-expect-error historical versions do not enable global draft publication.
void client.unpublishGlobal("history");
