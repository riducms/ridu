#!/usr/bin/env node

import { runRidu } from "@riducms/cli/run";

import { initializerArguments } from "./arguments.js";

try {
	process.exitCode = await runRidu(initializerArguments(process.argv.slice(2)));
} catch (error) {
	console.error(`create-ridu: ${error instanceof Error ? error.message : String(error)}`);
	process.exitCode = 1;
}
