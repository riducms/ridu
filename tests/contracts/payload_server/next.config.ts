import { withPayload } from "@payloadcms/next/withPayload";
import type { NextConfig } from "next";
import path from "node:path";
import { fileURLToPath } from "node:url";

const filename = fileURLToPath(import.meta.url);
const dirname = path.dirname(filename);

const nextConfig: NextConfig = {
	images: {
		localPatterns: [{ pathname: "/api/media/file/**" }],
	},
	output: process.env.RIDU_PAYLOAD_STANDALONE === "true" ? "standalone" : undefined,
	turbopack: {
		root: path.resolve(dirname),
	},
	webpack: (webpackConfig) => {
		webpackConfig.resolve.extensionAlias = {
			".cjs": [".cts", ".cjs"],
			".js": [".ts", ".tsx", ".js", ".jsx"],
			".mjs": [".mts", ".mjs"],
		};

		return webpackConfig;
	},
};

export default withPayload(nextConfig, { devBundleServerPackages: false });
