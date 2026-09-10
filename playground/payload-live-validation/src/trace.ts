import { appendFileSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
export const tracePath = path.resolve(".evidence/events.jsonl");
export function trace(event: Record<string, unknown>) {
	mkdirSync(path.dirname(tracePath), { recursive: true });
	appendFileSync(
		tracePath,
		JSON.stringify({ at: Date.now(), server: typeof window === "undefined", ...event }) + "\n"
	);
}
export function events() {
	try {
		return readFileSync(tracePath, "utf8")
			.trim()
			.split("\n")
			.filter(Boolean)
			.map((line) => JSON.parse(line));
	} catch {
		return [];
	}
}
export function reset() {
	writeFileSync(tracePath, "");
}
