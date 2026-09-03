#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 || $# -gt 2 ]]; then
	echo "usage: publish-release-packages.sh <artifact-directory> [--local-first-publication]" >&2
	exit 2
fi

local_first_publication=false
if [[ $# -eq 2 ]]; then
	if [[ "$2" != "--local-first-publication" ]]; then
		echo "unknown option: $2" >&2
		exit 2
	fi
	local_first_publication=true
fi

artifact_root="$1"
repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
version="$(bun "$repository_root/scripts/check-release-version.ts" --print)"
npm_registry="https://registry.npmjs.org/"
registry_stderr="$(mktemp "${TMPDIR:-/tmp}/ridu-npm-registry.XXXXXX")"
cleanup() {
	rm -f -- "$registry_stderr"
}
trap cleanup EXIT

is_registry_not_found() {
	local output="$1"
	grep -Eq '(^|[^[:alnum:]_])E404([^[:alnum:]_]|$)' <<<"$output"
}

artifacts=(
	ridu-framework-cli.tgz
	ridu-create.tgz
	ridu-framework-protocol.tgz
	ridu-framework-translations.tgz
	ridu-framework-ui.tgz
	ridu-framework-build.tgz
	ridu-framework-sdk.tgz
	ridu-framework-plugin.tgz
	ridu-framework-plugin-richtext.tgz
	ridu-framework-plugin-seo.tgz
	ridu-framework-plugin-form-builder.tgz
	ridu-framework-admin.tgz
)
package_names=()
package_versions=()
artifact_states=()

# Resolve every local artifact and every exact registry version before the first
# immutable write. A transient registry failure must never be mistaken for an
# unpublished version after earlier packages have already been created.
for filename in "${artifacts[@]}"; do
	artifact="$artifact_root/$filename"
	if [[ ! -f "$artifact" ]]; then
		echo "missing npm release artifact $artifact" >&2
		exit 1
	fi
	manifest="$(tar -xOf "$artifact" package/package.json)"
	package_name="$(jq -r .name <<<"$manifest")"
	package_version="$(jq -r .version <<<"$manifest")"
	if [[ "$package_version" != "$version" ]]; then
		echo "$package_name has version $package_version; expected $version" >&2
		exit 1
	fi
	local_integrity="sha512-$(openssl dgst -sha512 -binary "$artifact" | openssl base64 -A)"
	: > "$registry_stderr"
	if registry_response="$(npm view "$package_name@$package_version" dist.integrity \
		--json --registry="$npm_registry" 2>"$registry_stderr")"; then
		registry_integrity="$(jq -r 'if type == "string" then . else empty end' <<<"$registry_response")"
		if [[ -z "$registry_integrity" ]]; then
			echo "npm returned no integrity for existing $package_name@$package_version" >&2
			exit 1
		fi
		if [[ "$registry_integrity" != "$local_integrity" ]]; then
			echo "$package_name@$package_version already exists with different artifact integrity" >&2
			exit 1
		fi
		artifact_state=existing
	else
		registry_error="$registry_response"$'\n'"$(<"$registry_stderr")"
		if ! is_registry_not_found "$registry_error"; then
			echo "could not determine whether $package_name@$package_version exists; refusing to publish" >&2
			printf '%s\n' "$registry_error" >&2
			exit 1
		fi
		artifact_state=missing
	fi
	package_names+=("$package_name")
	package_versions+=("$package_version")
	artifact_states+=("$artifact_state")
done

if [[ "$local_first_publication" == true ]]; then
	: > "$registry_stderr"
	if ! npm_identity="$(npm whoami --registry="$npm_registry" 2>"$registry_stderr")"; then
		echo "npm authentication is required for the local first publication" >&2
		cat "$registry_stderr" >&2
		exit 1
	fi
	: > "$registry_stderr"
	if ! org_roster="$(npm org ls riducms "$npm_identity" --json \
		--registry="$npm_registry" 2>"$registry_stderr")" ||
		! jq -e --arg user "$npm_identity" \
		'.[$user] == "developer" or .[$user] == "admin" or .[$user] == "owner"' \
		<<<"$org_roster" >/dev/null; then
		echo "npm user $npm_identity is not a publishing member of the riducms organization" >&2
		exit 1
	fi

	# Exact-version E404 is not enough for an unscoped package: another owner may
	# already control the global name at a different version.
	: > "$registry_stderr"
	if create_ridu_record="$(npm view create-ridu name --json \
		--registry="$npm_registry" 2>"$registry_stderr")"; then
		if ! create_ridu_owners="$(npm owner ls create-ridu \
			--registry="$npm_registry" 2>"$registry_stderr")" ||
			! awk -v user="$npm_identity" '$1 == user { found = 1 } END { exit !found }' \
			<<<"$create_ridu_owners"; then
			echo "the unscoped create-ridu package exists but npm user $npm_identity is not an owner" >&2
			exit 1
		fi
	else
		registry_error="$create_ridu_record"$'\n'"$(<"$registry_stderr")"
		if ! is_registry_not_found "$registry_error"; then
			echo "could not verify ownership or availability of the unscoped create-ridu package" >&2
			printf '%s\n' "$registry_error" >&2
			exit 1
		fi
	fi
fi

publish_arguments=(--access public --tag latest --registry="$npm_registry")
if [[ "$local_first_publication" == true ]]; then
	# Every release manifest enables provenance for trusted GitHub publication.
	# A CLI flag must override that publishConfig value for the one local
	# bootstrap, where npm has no CI identity from which to create provenance.
	publish_arguments+=(--provenance=false)
else
	publish_arguments+=(--provenance)
fi

for index in "${!artifacts[@]}"; do
	package_name="${package_names[$index]}"
	package_version="${package_versions[$index]}"
	if [[ "${artifact_states[$index]}" == existing ]]; then
		echo "verified existing $package_name@$package_version"
		continue
	fi
	npm publish "$artifact_root/${artifacts[$index]}" "${publish_arguments[@]}"
done

echo "published or verified all twelve Ridu $version packages at the latest dist-tag"
