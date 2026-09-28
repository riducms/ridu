import type { SchemaAdminLoader } from "@riducms/protocol";
import { createAdminLoader } from "@riducms/sdk";

/** What the Go plugin's playground loader returns. */
export interface PlaygroundData {
	/** Same-origin path of the GraphQL transport, such as `/api/graphql`. */
	endpoint: string;
	/** The transport's SDL, identical to the file `ridu generate` writes. */
	schema: string;
}

// Mirrors the Go loader's manifest contract property for property. The admin build compares the
// two exactly, so a mismatch fails before the playground can request data.
const playgroundContract: SchemaAdminLoader = {
	key: "graphql-playground",
	input: { kind: "object" },
	output: {
		kind: "object",
		fields: { endpoint: { kind: "string" }, schema: { kind: "string" } },
	},
};

/** Reads the playground's endpoint and schema through the admin's existing access policy. */
export const playgroundLoader = createAdminLoader<Record<string, never>, PlaygroundData>(
	playgroundContract
);
