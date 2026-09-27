import { createClient, type ClientOptions } from "@riducms/sdk";

const options: ClientOptions = { baseURL: "https://cms.example.test" };
const client = createClient(options);
void client.schema();

// @ts-expect-error raw collection calls require a generated config or an explicit generic.
void client.list("posts");
