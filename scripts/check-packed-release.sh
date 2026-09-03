#!/usr/bin/env bash
set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
release_workspace="$(mktemp -d "${TMPDIR:-/tmp}/ridu-packed-release.XXXXXX")"
cleanup() {
	chmod -R u+w "$release_workspace" 2>/dev/null || true
	rm -rf -- "$release_workspace"
}
trap cleanup EXIT
export GOMODCACHE="$release_workspace/go-module-cache"
export GOCACHE="$release_workspace/go-build-cache"

version="$(bun "$repository_root/scripts/check-release-version.ts" --print)"
project_root="$release_workspace/clean-ridu-release"
artifact_root="$release_workspace/artifacts"
mkdir -p "$artifact_root"

(
	cd "$repository_root"
	go run ./internal/dogfood/new \
		--release "v$version" \
		--module example.com/ridu-packed-release \
		--scope @ridu-packed-release \
		--package-manager npm \
		--target "$project_root"
)

package_root="$project_root/.ridu/packages"
source_package_root="$release_workspace/package-sources"

for package_directory in "$package_root"/*; do
	package_target="$(basename "$package_directory")"
	(
		cd "$package_directory"
		bun pm pack \
			--filename "$artifact_root/$package_target.tgz" \
			--ignore-scripts \
			--quiet
	)
	packed_manifest_file="$release_workspace/$package_target-package.json"
	tar -xOf "$artifact_root/$package_target.tgz" package/package.json > "$packed_manifest_file"
	packed_manifest="$(<"$packed_manifest_file")"
	if grep -q 'workspace:' <<<"$packed_manifest"; then
		echo "$package_target contains an unresolved workspace dependency" >&2
		exit 1
	fi
	if ! grep -q "\"version\": \"$version\"" <<<"$packed_manifest"; then
		echo "$package_target does not carry release version $version" >&2
		exit 1
	fi
	node --input-type=module --eval '
		import { readFileSync } from "node:fs";
		const [manifestPath, releaseVersion] = process.argv.slice(1);
		const manifest = JSON.parse(readFileSync(manifestPath, "utf8"));
		for (const section of ["dependencies", "devDependencies", "peerDependencies", "optionalDependencies"]) {
			for (const [name, specifier] of Object.entries(manifest[section] ?? {})) {
				if (name.startsWith("@riducms/") && specifier !== releaseVersion) {
					console.error(`${manifest.name} has non-exact ${section} edge ${name}@${specifier}; expected ${releaseVersion}`);
					process.exit(1);
				}
			}
		}
	' "$packed_manifest_file" "$version"
	packed_members="$release_workspace/$package_target-members.txt"
	tar -tzf "$artifact_root/$package_target.tgz" > "$packed_members"
	if ! grep -Fxq -- 'package/LICENSE' "$packed_members"; then
		echo "$package_target omits package/LICENSE" >&2
		exit 1
	fi
	if [[ "$package_target" == "ridu-create" ]] && ! grep -Fxq -- 'package/README.md' "$packed_members"; then
		echo "$package_target omits package/README.md" >&2
		exit 1
	fi
	if [[ "$package_target" == "ridu-framework-admin" ]]; then
		if ! grep -Fxq -- 'package/THIRD_PARTY_NOTICES.md' "$packed_members"; then
			echo "$package_target omits package/THIRD_PARTY_NOTICES.md" >&2
			exit 1
		fi
	elif grep -Fxq -- 'package/THIRD_PARTY_NOTICES.md' "$packed_members"; then
		echo "$package_target includes unrelated third-party notices" >&2
		exit 1
	fi
	if [[ "$package_target" == "ridu-framework-protocol" || "$package_target" == "ridu-framework-sdk" ]]; then
		if grep -Fq '"./src/' <<<"$packed_manifest"; then
			echo "$package_target manifest exports raw TypeScript runtime sources" >&2
			exit 1
		fi
		for compiled_file in package/dist/index.js package/dist/index.d.ts; do
			if ! grep -Fxq -- "$compiled_file" "$packed_members"; then
				echo "$package_target omits $compiled_file" >&2
				exit 1
			fi
		done
		if grep -Eq '^package/src/.*\.ts$' "$packed_members"; then
			echo "$package_target publishes raw TypeScript runtime sources" >&2
			exit 1
		fi
	fi
done

node_consumer="$release_workspace/node-consumer"
mkdir -p "$node_consumer"
(
	cd "$node_consumer"
	npm init --yes >/dev/null
	npm install --ignore-scripts --no-audit --no-fund \
		"$artifact_root/ridu-framework-cli.tgz" \
		"$artifact_root/ridu-create.tgz" \
		"$artifact_root/ridu-framework-protocol.tgz" \
		"$artifact_root/ridu-framework-sdk.tgz" \
		'typescript@~6.0.2' \
		'@types/node@^24.13.3' >/dev/null
	node --input-type=module --eval '
		import assert from "node:assert/strict";
		import { PROTOCOL_VERSION } from "@riducms/protocol";
		import { createClient, RiduError } from "@riducms/sdk";
		assert.equal(typeof PROTOCOL_VERSION, "number");
		assert.equal(typeof createClient, "function");
		assert.equal(typeof RiduError, "function");
		assert.equal(typeof createClient({ baseURL: "https://example.test" }).schema, "function");
	'
	printf '%s\n' \
		'import { PROTOCOL_VERSION } from "@riducms/protocol";' \
		'import { createClient, type ClientOptions, type RequestOptions } from "@riducms/sdk";' \
		'const options: ClientOptions = {' \
		'  baseURL: "https://example.test",' \
		'  headers: { authorization: "Bearer test" },' \
		'  credentials: "include",' \
		'};' \
		'const requestOptions: RequestOptions = { headers: [["x-ridu", "test"]] };' \
		'void createClient(options).schema(requestOptions);' \
		'void PROTOCOL_VERSION;' > consumer.ts
	printf '%s\n' \
		'{' \
		'  "compilerOptions": {' \
		'    "lib": ["ES2023"],' \
		'    "module": "NodeNext",' \
		'    "moduleResolution": "NodeNext",' \
		'    "strict": true,' \
		'    "noEmit": true,' \
		'    "types": ["node"]' \
		'  },' \
		'  "include": ["consumer.ts"]' \
		'}' > tsconfig.json
	./node_modules/.bin/tsc -p tsconfig.json
	fake_ridu="$release_workspace/fake-ridu"
	printf '%s\n' \
		'#!/usr/bin/env sh' \
		'printf "%s\\n" "$@"' > "$fake_ridu"
	chmod +x "$fake_ridu"
	initializer_output="$(RIDU_BINARY="$fake_ridu" ./node_modules/.bin/create-ridu content --template blank --database sqlite)"
	expected_initializer_output="$(printf '%s\n' new --package-manager npm content --template blank --database sqlite)"
	if [[ "$initializer_output" != "$expected_initializer_output" ]]; then
		echo "create-ridu did not forward its arguments through ridu new" >&2
		exit 1
	fi
)

mv "$package_root" "$source_package_root"
mkdir -p "$package_root"
for artifact in "$artifact_root"/*.tgz; do
	package_target="$(basename "$artifact" .tgz)"
	mkdir -p "$package_root/$package_target"
	tar -xzf "$artifact" --strip-components=1 -C "$package_root/$package_target"
done

printf '%s\n' \
	'import { richTextMessages } from "@riducms/plugin-richtext";' \
	'import { seoMessages } from "@riducms/plugin-seo";' \
	'import { formBuilderMessages } from "@riducms/plugin-form-builder/admin";' \
	'void richTextMessages;' \
	'void seoMessages;' \
	'void formBuilderMessages;' > "$project_root/admin/src/framework-alias-contract.ts"

(
	cd "$project_root"
	if PATH="$(dirname "$(command -v npm)"):/usr/bin:/bin" npm run ridu -- version >"$release_workspace/missing-local-cli.log" 2>&1; then
		echo "generated project unexpectedly ran its local CLI before dependencies were installed" >&2
		exit 1
	fi
	if ! grep -Eqi 'not found|could not determine executable|failed to resolve' "$release_workspace/missing-local-cli.log"; then
		echo "missing project-local CLI failure did not lead the user back to dependency installation" >&2
		cat "$release_workspace/missing-local-cli.log" >&2
		exit 1
	fi
	npm install --no-audit --no-fund
	RIDU_BINARY="$project_root/.ridu/bin/ridu" npm run ridu -- version
	RIDU_BINARY="$project_root/.ridu/bin/ridu" npm run ridu -- generate --check
	RIDU_BINARY="$project_root/.ridu/bin/ridu" npm run ridu -- migrate create --name initial
	RIDU_BINARY="$project_root/.ridu/bin/ridu" npm run ridu -- check
	RIDU_BINARY="$project_root/.ridu/bin/ridu" npm run ridu -- build
)

test -x "$project_root/dist/clean-ridu-release"

# Exercise the publication command construction without touching npm. The
# release manifests deliberately enable provenance, so local first publication
# must explicitly override it rather than merely omitting --provenance.
fake_npm_bin="$release_workspace/fake-npm-bin"
npm_invocation_log="$release_workspace/npm-invocations.log"
mkdir -p "$fake_npm_bin"
printf '%s\n' \
	'#!/usr/bin/env bash' \
	'set -euo pipefail' \
	'case "$1" in' \
	'  view)' \
	'    if [[ "${RIDU_FAKE_NPM_VIEW_MODE:-missing}" == "error" ]]; then' \
	'      printf "npm error code EAI_AGAIN\\n" >&2' \
	'      exit 1' \
	'    fi' \
	'    printf "npm error code E404\\n" >&2' \
	'    exit 1' \
	'    ;;' \
	'  whoami)' \
	'    printf "ridu-test\\n"' \
	'    ;;' \
	'  org)' \
	"    printf '%s\\n' '{\"ridu-test\":\"owner\"}'" \
	'    ;;' \
	'  owner)' \
	'    printf "ridu-test <ridu@example.test>\\n"' \
	'    ;;' \
	'  publish)' \
	'    printf "%q " "$@" >> "$RIDU_NPM_INVOCATION_LOG"' \
	'    printf "\\n" >> "$RIDU_NPM_INVOCATION_LOG"' \
	'    ;;' \
	'  *)' \
	'    printf "unexpected npm command: %s\\n" "$1" >&2' \
	'    exit 2' \
	'    ;;' \
	'esac' > "$fake_npm_bin/npm"
chmod +x "$fake_npm_bin/npm"

PATH="$fake_npm_bin:$PATH" RIDU_NPM_INVOCATION_LOG="$npm_invocation_log" \
	bash "$repository_root/scripts/publish-release-packages.sh" \
	"$artifact_root" --local-first-publication >/dev/null
if [[ "$(wc -l < "$npm_invocation_log" | tr -d ' ')" != "12" ]] ||
	[[ "$(grep -Fc -- '--provenance=false' "$npm_invocation_log")" != "12" ]] ||
	grep -Eq '(^| )--provenance( |$)' "$npm_invocation_log"; then
	echo "local first-publication commands did not explicitly disable provenance" >&2
	exit 1
fi

: > "$npm_invocation_log"
PATH="$fake_npm_bin:$PATH" RIDU_NPM_INVOCATION_LOG="$npm_invocation_log" \
	bash "$repository_root/scripts/publish-release-packages.sh" "$artifact_root" >/dev/null
if [[ "$(wc -l < "$npm_invocation_log" | tr -d ' ')" != "12" ]] ||
	[[ "$(grep -Ec '(^| )--provenance( |$)' "$npm_invocation_log")" != "12" ]] ||
	grep -Fq -- '--provenance=false' "$npm_invocation_log"; then
	echo "trusted-publication commands did not require provenance" >&2
	exit 1
fi

: > "$npm_invocation_log"
if PATH="$fake_npm_bin:$PATH" RIDU_NPM_INVOCATION_LOG="$npm_invocation_log" \
	RIDU_FAKE_NPM_VIEW_MODE=error \
	bash "$repository_root/scripts/publish-release-packages.sh" "$artifact_root" >/dev/null 2>&1; then
	echo "publication accepted an indeterminate npm registry response" >&2
	exit 1
fi
if [[ -s "$npm_invocation_log" ]]; then
	echo "publication wrote packages after an indeterminate npm registry response" >&2
	exit 1
fi

echo "clean Node import, create-ridu forwarding, project-local CLI recovery/execution, strict Node-only types, scaffold, install, check, build, and publication command policy verified from twelve packed Ridu $version artifacts"
