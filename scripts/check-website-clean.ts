import { cp, lstat, mkdir, mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";

// Copy current source, including pending edits, without borrowing ignored dependency or build output.
const repository = resolve(import.meta.dir, "..");
const temporary = await mkdtemp(join(tmpdir(), "ridu-website-clean-"));
const checkout = join(temporary, "checkout");
const env: NodeJS.ProcessEnv = {
	...process.env,
	BUN_INSTALL_CACHE_DIR: join(temporary, "bun-cache"),
};
delete env.NODE_PATH;

async function run(command: string[]): Promise<void> {
	console.log(`Clean website: ${command.join(" ")}`);
	const child = Bun.spawn(command, { cwd: checkout, env, stdout: "pipe", stderr: "pipe" });
	const [stdout, stderr, status] = await Promise.all([
		new Response(child.stdout).text(),
		new Response(child.stderr).text(),
		child.exited,
	]);
	if (status !== 0) {
		throw new Error(`${command.join(" ")} exited ${status}\n${stdout}\n${stderr}`);
	}
	console.log(stdout.trim().split("\n").slice(-5).join("\n"));
}

try {
	const listing = Bun.spawnSync(
		["git", "ls-files", "-z", "--cached", "--others", "--exclude-standard"],
		{
			cwd: repository,
		}
	);
	if (listing.exitCode !== 0) throw new Error(listing.stderr.toString());
	for (const path of new Set(listing.stdout.toString().split("\0").filter(Boolean))) {
		// A tracked static admin bundle is unrelated to the website and must not hide build dependencies.
		if (
			path.split("/").some((part) => ["node_modules", "dist", ".ridu", ".astro"].includes(part))
		) {
			continue;
		}
		const source = join(repository, path);
		try {
			await lstat(source);
		} catch (error) {
			// The index still lists tracked files deleted by pending working-tree edits.
			if ((error as NodeJS.ErrnoException).code === "ENOENT") continue;
			throw error;
		}
		const destination = join(checkout, path);
		await mkdir(dirname(destination), { recursive: true });
		await cp(source, destination, { verbatimSymlinks: true });
	}
	const locks = await Promise.all(
		["bun.lock", "website/bun.lock"].map((path) => readFile(join(checkout, path), "utf8"))
	);
	await run([process.execPath, "run", "build:website"]);
	await run([process.execPath, "run", "--cwd", "website", "check:build"]);
	for (const [index, path] of ["bun.lock", "website/bun.lock"].entries()) {
		if ((await readFile(join(checkout, path), "utf8")) !== locks[index]) {
			throw new Error(`Website production build modified ${path}`);
		}
	}
	console.log("Clean website production build and output checks passed; lockfiles unchanged.");
} finally {
	await rm(temporary, { recursive: true, force: true });
}
