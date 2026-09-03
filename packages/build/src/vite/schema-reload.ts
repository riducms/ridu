import { readFileSync } from "node:fs";
import { basename } from "node:path";

import type { Plugin } from "vite";

interface RiduSchemaUpdateSignal {
	version: 1;
	revision: string;
	fullReload: boolean;
}

export function riduSchemaReloadPlugin(signalPath: string): Plugin {
	return {
		name: "ridu-schema-reload",
		configureServer(server) {
			let lastRevision = "";
			server.watcher.add(signalPath);
			server.watcher.on("all", (event, changedPath) => {
				if ((event !== "add" && event !== "change") || changedPath !== signalPath) return;
				try {
					const signal = JSON.parse(readFileSync(signalPath, "utf8")) as RiduSchemaUpdateSignal;
					if (signal.version !== 1 || signal.revision === lastRevision) return;
					lastRevision = signal.revision;
					server.ws.send(
						signal.fullReload
							? { type: "full-reload" }
							: {
									type: "custom",
									event: "ridu:schema-update",
									data: { revision: signal.revision },
								}
					);
				} catch {
					server.ws.send({ type: "full-reload" });
				}
			});
		},
		handleHotUpdate({ file }) {
			if (
				basename(file) === "ridu.generated.ts" ||
				basename(file) === "ridu.plugins.generated.ts"
			) {
				return [];
			}
		},
	};
}
