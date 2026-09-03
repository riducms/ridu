#!/usr/bin/env node

import { runRidu } from "./run.js";

try {
	process.exitCode = await runRidu(process.argv.slice(2));
} catch (error) {
	console.error(`ridu: ${error instanceof Error ? error.message : String(error)}`);
	process.exitCode = 1;
}
