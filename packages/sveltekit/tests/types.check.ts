// Compile-only contract: `bun run check` type-checks this file; it is not a runtime test.
import {
	createClient as createRuntimeClient,
	type AuthCollectionSlug,
	type ClientOptions,
	type RiduClient,
	type RiduConfigShape,
} from "@riducms/sdk";
import type { Handle, RequestEvent } from "@sveltejs/kit";

import { defineRidu, type InferClient, type InferSession } from "../src";
import { createRiduHandle, createServerClient } from "../src/server";

interface Member {
	id: string;
	email: string;
	displayName: string;
}

interface AppConfig extends RiduConfigShape {
	collections: {
		admins: {
			auth: true;
			upload: false;
			versions: false;
			drafts: false;
			trash: false;
			output: { id: string; email: string };
			create: { email: string };
			update: Record<string, unknown>;
			where: Record<string, unknown>;
			select: Record<string, boolean>;
			populate: Record<never, never>;
		};
		users: {
			auth: true;
			upload: false;
			versions: false;
			drafts: false;
			trash: false;
			output: Member;
			create: { email: string; displayName: string };
			update: Record<string, unknown>;
			where: Record<string, unknown>;
			select: Record<string, boolean>;
			populate: Record<never, never>;
		};
		posts: {
			auth: false;
			upload: false;
			versions: false;
			drafts: false;
			trash: false;
			output: { id: string; title: string };
			create: { title: string };
			update: { title?: string };
			where: Record<string, unknown>;
			select: Record<string, boolean>;
			populate: Record<never, never>;
		};
	};
}

// The same signature `ridu generate` emits.
function createClient<const DefaultAuth extends AuthCollectionSlug<AppConfig> = never>(
	options: ClientOptions<DefaultAuth>
): RiduClient<AppConfig, DefaultAuth> {
	return createRuntimeClient<AppConfig, DefaultAuth>(options);
}

const ridu = defineRidu({
	createClient,
	baseURL: "https://cms.example.test",
	authCollection: "users",
});

// @ts-expect-error posts is not an auth collection.
defineRidu({ createClient, baseURL: "https://cms.example.test", authCollection: "posts" });

type Client = InferClient<typeof ridu>;
type Session = InferSession<typeof ridu>;

export async function signIn(client: Client) {
	const session = await client.auth.login({ email: "a@example.test", password: "secret" });
	const name: string = session.user.displayName;
	const current = await client.auth.getSession();
	const collection: "users" | undefined = current?.collection;
	const reactive: string | undefined = client.auth.session?.user.displayName;
	await client.create("posts", { title: "Hello" });
	// @ts-expect-error admins sessions are not this application's sessions.
	const admin: Session["collection"] = "admins";
	return [name, collection, reactive, admin];
}

export function serverHooks(event: RequestEvent) {
	// SvelteKit 2's Handle; the SvelteKit 3 consumer check covers @sveltejs/kit/hooks.
	const handle: Handle = createRiduHandle(ridu);
	const client: Client = createServerClient(ridu, event);
	return { handle, client };
}
