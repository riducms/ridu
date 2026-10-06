// Measures Payload's schema-proportional initialization without opening a database, like the
// payloadcms/payload#17214 reproduction's measure-schemas.ts: it counts mongoose.Schema
// instances, then reports init time and the retained heap after a forced collection.
import mongoose from "mongoose";

let schemas = 0;
const OriginalSchema = mongoose.Schema;
(mongoose as unknown as { Schema: typeof OriginalSchema }).Schema = new Proxy(OriginalSchema, {
	construct(target, args, newTarget) {
		schemas += 1;
		return Reflect.construct(target, args, newTarget) as object;
	},
});

const gc = (globalThis as { gc?: () => void }).gc;
if (!gc) throw new Error("run the probe with node --expose-gc");
gc();
const before = process.memoryUsage();
const started = performance.now();
const { getPayload } = await import("payload");
const { default: configPromise } = await import("./payload.config.ts");
const config = await configPromise;
const configMs = performance.now() - started;
const payload = await getPayload({ config, disableDBConnect: true });
const initMs = performance.now() - started;
gc();
const after = process.memoryUsage();
console.log(
	JSON.stringify({
		scenario: process.env.BLOCKS_SCENARIO,
		database: process.env.PAYLOAD_DATABASE_URL?.split(":")[0],
		mongooseSchemas: schemas,
		configMs: Math.round(configMs * 100) / 100,
		initMs: Math.round(initMs * 100) / 100,
		heapUsedBytes: after.heapUsed,
		heapUsedDeltaBytes: after.heapUsed - before.heapUsed,
		rssBytes: after.rss,
		collections: Object.keys(payload.collections).length,
	})
);
process.exit(0);
