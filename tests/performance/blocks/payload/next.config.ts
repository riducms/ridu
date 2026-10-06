import { withPayload } from "@payloadcms/next/withPayload";
import type { NextConfig } from "next";
import path from "node:path";
import { fileURLToPath } from "node:url";

const dirname = path.dirname(fileURLToPath(import.meta.url));

// Always a standalone production server, like the measured Payload deployment in compare.ts.
const nextConfig: NextConfig = {
	output: "standalone",
	turbopack: { root: path.resolve(dirname) },
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
