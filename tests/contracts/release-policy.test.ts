import { expect, test } from "bun:test";
import { readFile } from "node:fs/promises";
import { resolve } from "node:path";

const repositoryRoot = resolve(import.meta.dir, "../..");

test("public projection excludes maintainer research in every format and keeps public assets", async () => {
	const paths = [
		"docs/README.md",
		"docs/architecture/audit/source-references.json",
		"docs/benchmarks/results.csv",
		"docs/reference/releases.md",
		"docs/assets/README.md",
		"docs/assets/logo.svg",
		"tests/performance/investigation.md",
		"tests/performance/results.csv",
		"tests/performance/README.md",
	];
	const process = Bun.spawn(["git", "check-attr", "export-ignore", "--", ...paths], {
		cwd: repositoryRoot,
		stdout: "pipe",
		stderr: "pipe",
	});
	const [output, errors, status] = await Promise.all([
		new Response(process.stdout).text(),
		new Response(process.stderr).text(),
		process.exited,
	]);
	expect(status).toBe(0);
	expect(errors).toBe("");
	expect(output.trim().split("\n")).toEqual(
		paths.map((path) => {
			const publicPath = path.startsWith("docs/assets/") || path === "tests/performance/README.md";
			return `${path}: export-ignore: ${publicPath ? "unset" : "set"}`;
		})
	);
});

type ReleaseStep = {
	uses?: string;
	run?: string;
	env?: Record<string, string>;
	with?: Record<string, unknown>;
};

test("release configuration orders trusted publication and preserves reviewed artifacts", async () => {
	const [workflowSource, releaseSource] = await Promise.all([
		readFile(resolve(repositoryRoot, ".github/workflows/release.yml"), "utf8"),
		readFile(resolve(repositoryRoot, ".goreleaser.yaml"), "utf8"),
	]);
	// These assertions inspect declarative policy and command selection. They do
	// not claim to execute GitHub Actions or prove arbitrary shell semantics.
	const workflow = Bun.YAML.parse(workflowSource) as {
		on: Record<string, { inputs: Record<string, { required: boolean; type: string }> }>;
		env?: Record<string, string>;
		jobs: {
			publish: {
				env?: Record<string, string>;
				permissions: Record<string, string>;
				steps: ReleaseStep[];
			};
		};
	};
	const release = Bun.YAML.parse(releaseSource) as {
		release: {
			draft: boolean;
			target_commitish: string;
			replace_existing_draft: boolean;
			replace_existing_artifacts: boolean;
			extra_files: { glob: string }[];
		};
		checksum: { name_template: string; extra_files: { glob: string }[] };
		changelog: { use: string };
	};
	expect(Object.keys(workflow.on)).toEqual(["workflow_dispatch"]);
	expect(workflow.on.workflow_dispatch?.inputs.commit_sha).toMatchObject({
		required: true,
		type: "string",
	});
	expect(workflow.on.workflow_dispatch?.inputs).not.toHaveProperty("use_initial_npm_token");
	for (const environment of [workflow.env, workflow.jobs.publish.env]) {
		expect(environment ?? {}).not.toHaveProperty("NPM_TOKEN");
	}
	expect(workflow.jobs.publish.permissions["id-token"]).toBe("write");
	const steps = workflow.jobs.publish.steps;
	const commandIndex = (pattern: RegExp) => {
		const matching = steps.flatMap((step, index) =>
			step.run && pattern.test(step.run) ? [index] : []
		);
		expect(matching).toHaveLength(1);
		return matching[0]!;
	};
	const npm = commandIndex(/\b(?:bash\s+)?\.\/scripts\/publish-release-packages\.sh\b/);
	const tag = commandIndex(/^\s*git\s+tag\s+/m);
	const draft = commandIndex(/^\s*goreleaser\s+release\s+--clean\s+--release-notes\s/m);
	const publish = commandIndex(/\bgh\s+release\s+edit\b[^\n]*--draft=false/);
	const notes = commandIndex(/test -s "\.github\/release-notes\/\$\{REQUESTED_VERSION\}\.md"/);
	expect(notes).toBeLessThan(npm);
	expect(steps[notes]?.env?.REQUESTED_VERSION).toBe("${{ inputs.version }}");
	expect(steps[notes]?.run).toContain(
		'bun ./scripts/check-release-version.ts "v${REQUESTED_VERSION}"'
	);
	expect(steps[notes]?.run?.indexOf("check-release-version.ts")).toBeLessThan(
		steps[notes]!.run!.indexOf("test -s")
	);
	expect(steps[notes]?.run).toContain('echo "version=${REQUESTED_VERSION}" >> "${GITHUB_OUTPUT}"');
	expect(steps[draft]?.env?.RELEASE_VERSION).toBe("${{ steps.release.outputs.version }}");
	expect(steps[draft]?.run).toContain(
		'--release-notes ".github/release-notes/${RELEASE_VERSION}.md"'
	);
	expect(npm).toBeLessThan(tag);
	expect(tag).toBeLessThan(draft);
	expect(draft).toBeLessThan(publish);
	expect(steps[tag]?.env?.RELEASE_TAG).toBe("${{ steps.release.outputs.tag }}");
	const guard = steps.find((step) => step.env?.EXPECTED_COMMIT_SHA === "${{ inputs.commit_sha }}");
	expect(guard).toBeDefined();
	expect(guard?.run).toMatch(/\btest\s+"\$\{GITHUB_REF\}"\s*=\s*"refs\/heads\/main"/);
	expect(guard?.run).toMatch(/\btest\s+"\$\{GITHUB_SHA\}"\s*=\s*"\$\{EXPECTED_COMMIT_SHA\}"/);
	expect(guard?.run).toMatch(
		/\btest\s+"\$\(git\s+rev-parse\s+HEAD\)"\s*=\s*"\$\{EXPECTED_COMMIT_SHA\}"/
	);
	expect(steps[tag]?.run).toMatch(/\bgit\s+tag\s+"\$\{RELEASE_TAG\}"\s+"\$\{GITHUB_SHA\}"/);
	const attestation = steps.find(
		(step) => step.uses?.startsWith("actions/attest@") && step.with?.["subject-checksums"]
	);
	expect(attestation?.with?.["subject-checksums"]).toBe("dist/SHA256SUMS");
	for (const step of steps) {
		expect(step.env).not.toHaveProperty("NPM_TOKEN");
		expect(step.run ?? "").not.toMatch(
			/--clobber|\bgh\s+release\s+(?:create|upload)\b|\bNPM_TOKEN\b/
		);
	}
	expect(release.release).toMatchObject({
		draft: true,
		target_commitish: "{{ .Commit }}",
		replace_existing_draft: false,
		replace_existing_artifacts: false,
	});
	expect(release.checksum.name_template).toBe("SHA256SUMS");
	expect(release.checksum.extra_files).toContainEqual({ glob: ".ridu/release-npm/*.tgz" });
	expect(release.release.extra_files).toContainEqual({ glob: ".ridu/release-npm/*.tgz" });
	expect(release.changelog.use).toBe("github-native");
});
