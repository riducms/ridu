import { spawn } from "node:child_process";
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { ensureBinary } from "./launcher.js";

const packageRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const { version } = JSON.parse(readFileSync(resolve(packageRoot, "package.json"), "utf8"));

export async function runRidu(arguments_) {
	const binary = await ensureBinary(version);
	const child = spawn(binary, arguments_, { stdio: "inherit" });
	const signalHandlers = new Map();
	for (const signal of ["SIGINT", "SIGTERM"]) {
		const handler = () => child.kill(signal);
		signalHandlers.set(signal, handler);
		process.once(signal, handler);
	}
	try {
		return await new Promise((resolvePromise, reject) => {
			child.once("error", reject);
			child.once("exit", (code) => resolvePromise(code ?? 1));
		});
	} finally {
		for (const [signal, handler] of signalHandlers) process.off(signal, handler);
	}
}
