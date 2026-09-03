import { describe, expect, test } from "bun:test";

import { detectPackageManager, initializerArguments } from "./arguments.js";

describe("Ridu project initializer", () => {
	test("enters the native new command, detects the invoking manager, and preserves user arguments", () => {
		expect(initializerArguments([], "npm/11.6.2 node/v24.13.0")).toEqual([
			"new",
			"--package-manager",
			"npm",
		]);
		expect(
			initializerArguments(
				["content", "--template", "blank", "--database", "sqlite"],
				"pnpm/10.17.1 npm/? node/v24.13.0"
			)
		).toEqual([
			"new",
			"--package-manager",
			"pnpm",
			"content",
			"--template",
			"blank",
			"--database",
			"sqlite",
		]);
	});

	test("preserves an explicit package-manager override", () => {
		expect(initializerArguments(["--package-manager", "yarn", "content"], "bun/1.4.0")).toEqual([
			"new",
			"--package-manager",
			"yarn",
			"content",
		]);
		expect(initializerArguments(["--package-manager=bun", "content"], "npm/11.6.2")).toEqual([
			"new",
			"--package-manager=bun",
			"content",
		]);
	});

	test("falls back to npm for direct or unknown launchers", () => {
		expect(detectPackageManager("")).toBe("npm");
		expect(detectPackageManager("corepack/0.31.0")).toBe("npm");
		expect(detectPackageManager("yarn/4.7.0 npm/? node/v22.0.0")).toBe("yarn");
		expect(detectPackageManager("", "/opt/homebrew/bin/pnpm.cjs")).toBe("pnpm");
	});
});
