import {
	createClient,
	type CollectionContract,
	type Version,
	type ScheduledPublish,
	type RiduClient,
} from "@riducms/sdk";
import type { FieldDocument } from "@riducms/plugin";

export type AdminDocument = FieldDocument;

type AdminCollection = CollectionContract & {
	auth: true;
	upload: true;
	versions: true;
	output: AdminDocument;
	create: Record<string, unknown>;
	update: Record<string, unknown>;
	where: Record<string, unknown>;
	select: Record<string, boolean>;
	populate: Record<string, unknown>;
	trash: true;
};

type AdminGlobal = {
	versions: true;
	output: AdminDocument;
	update: Record<string, unknown>;
	select: Record<string, boolean>;
	populate: Record<string, unknown>;
};

export interface AdminConfig {
	collections: Record<string, AdminCollection>;
	globals: Record<string, AdminGlobal>;
}

export type AdminVersion = Version<AdminDocument>;
export type AdminScheduledPublish = ScheduledPublish;

export type AdminClient = RiduClient<AdminConfig>;

export function createAdminClient(baseURL = window.location.origin): AdminClient {
	return createClient<AdminConfig>({ baseURL });
}
