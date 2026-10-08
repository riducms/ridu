import { expect, test } from "bun:test";
import { mkdir, mkdtemp, realpath, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { posix, resolve, win32 } from "node:path";

import { modulePackageName } from "../../packages/build/src/vite/application";
import { findPackageSourceRoot, moduleFilePath } from "../../packages/build/src/vite/source-alias";

test("module IDs name files on Windows and POSIX hosts", () => {
	const windowsID = "C:/Users/ridu/app/node_modules/@riducms/admin/src/index.ts";
	expect(moduleFilePath(`${windowsID}?v=4565b57e`, win32.isAbsolute)).toBe(windowsID);
	expect(moduleFilePath(`\0${windowsID}?svelte&type=style&lang.css`, win32.isAbsolute)).toBe(
		windowsID
	);
	expect(moduleFilePath("\0virtual:ridu-admin-plugins", win32.isAbsolute)).toBeUndefined();

	const posixID = "/home/ridu/app/node_modules/@riducms/admin/src/index.ts";
	expect(moduleFilePath(`\0${posixID}?v=4565b57e`, posix.isAbsolute)).toBe(posixID);
	expect(moduleFilePath("\0virtual:ridu-admin-plugins", posix.isAbsolute)).toBeUndefined();
});

test("package lookups accept prefixed and queried module IDs", async () => {
	const root = await realpath(await mkdtemp(resolve(tmpdir(), "ridu-source-alias-")));
	try {
		for (const name of ["@riducms/admin", "@riducms/ui"]) {
			await mkdir(resolve(root, "node_modules", name, "src"), { recursive: true });
			await writeFile(
				resolve(root, "node_modules", name, "package.json"),
				JSON.stringify({ name })
			);
		}
		const adminSource = resolve(root, "node_modules/@riducms/admin/src");
		const adminID = resolve(adminSource, "index.ts");
		expect(findPackageSourceRoot(`${adminID}?v=4565b57e`, new Map())).toBe(adminSource);
		expect(findPackageSourceRoot(`\0${adminID}`, new Map())).toBe(adminSource);
		expect(findPackageSourceRoot("\0virtual:ridu-admin-plugins", new Map())).toBeUndefined();

		const uiID = resolve(root, "node_modules/@riducms/ui/src/button.svelte");
		expect(modulePackageName(`${uiID}?v=4565b57e`)).toBe("@riducms/ui");
		expect(modulePackageName(`\0${uiID}?svelte&type=style&lang.css`)).toBe("@riducms/ui");
		expect(modulePackageName("\0virtual:ridu-admin-plugins")).toBeUndefined();
	} finally {
		await rm(root, { recursive: true, force: true });
	}
});
