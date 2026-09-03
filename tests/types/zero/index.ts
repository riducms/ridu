import { createClient } from "@riducms/sdk";

const client = createClient({ baseURL: "https://cms.example.test" });
void client.schema();

// @ts-expect-error raw collection calls require a generated config or an explicit generic.
void client.list("posts");
