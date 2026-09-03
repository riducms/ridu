#!/usr/bin/env bash
set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
workspace="$(mktemp -d "${TMPDIR:-/tmp}/ridu-release-reproducibility.XXXXXX")"
dist_root="$repository_root/dist"
original_dist="$workspace/original-dist"
had_original_dist=false
if [[ -e "$dist_root" ]]; then
	mv "$dist_root" "$original_dist"
	had_original_dist=true
fi
restore_dist() {
	if [[ -e "$dist_root" ]]; then
		mv "$dist_root" "$workspace/generated-dist"
	fi
	if [[ "$had_original_dist" == true ]]; then
		mv "$original_dist" "$dist_root"
	fi
}
cleanup() {
	restore_dist
	rm -rf -- "$workspace"
}
trap cleanup EXIT

if ! command -v goreleaser >/dev/null 2>&1; then
	echo "GoReleaser 2.18.0 is required for the release reproducibility check" >&2
	exit 1
fi
if ! goreleaser --version 2>&1 | grep -Eq '^GitVersion:[[:space:]]+v?2\.18\.0$'; then
	echo "GoReleaser 2.18.0 is required; install the pinned release tool before running make release-check" >&2
	exit 1
fi

source_date_epoch="$(git -C "$repository_root" show -s --format=%ct HEAD)"

copy_release_outputs() {
	local destination="$1"
	mkdir -p "$destination"
	cp "$dist_root"/*.tar.gz "$destination/"
	cp "$dist_root"/*.zip "$destination/"
	cp "$dist_root/SHA256SUMS" "$destination/"
	cp "$repository_root/.ridu/release-metadata/ridu.spdx.json" "$destination/"
}

(
	cd "$repository_root"
	SOURCE_DATE_EPOCH="$source_date_epoch" goreleaser release --snapshot --clean >/dev/null
)
copy_release_outputs "$workspace/first"
(
	cd "$repository_root"
	SOURCE_DATE_EPOCH="$source_date_epoch" goreleaser release --snapshot --clean >/dev/null
)
copy_release_outputs "$workspace/second"

if ! diff -qr "$workspace/first" "$workspace/second" >/dev/null; then
	diff -qr "$workspace/first" "$workspace/second" >&2 || true
	echo "release archives, SBOM, or checksums are not reproducible" >&2
	exit 1
fi

require_exact_line() {
	local expected="$1"
	local file="$2"
	if ! grep -Fxq -- "$expected" "$file"; then
		echo "expected '$expected' in $file" >&2
		exit 1
	fi
}

require_matching_line() {
	local pattern="$1"
	local file="$2"
	if ! grep -Eq -- "$pattern" "$file"; then
		echo "expected pattern '$pattern' in $file" >&2
		exit 1
	fi
}

linux_archive="$(find "$workspace/first" -maxdepth 1 -type f -name 'ridu_*_linux_amd64.tar.gz' -print -quit)"
windows_archive="$(find "$workspace/first" -maxdepth 1 -type f -name 'ridu_*_windows_amd64.zip' -print -quit)"
if [[ -z "$linux_archive" || -z "$windows_archive" ]]; then
	echo "GoReleaser omitted a required Linux or Windows archive" >&2
	exit 1
fi
linux_archive_members="$workspace/linux-archive-members.txt"
windows_archive_members="$workspace/windows-archive-members.txt"
archive_notices="$workspace/archive-third-party-notices.md"
archive_go_licenses="$workspace/archive-third-party-go-licenses.txt"
archive_windows_go_licenses="$workspace/archive-windows-third-party-go-licenses.txt"
archive_js_licenses="$workspace/archive-third-party-js-licenses.txt"

tar -tzf "$linux_archive" > "$linux_archive_members"
unzip -Z1 "$windows_archive" > "$windows_archive_members"
tar -xOf "$linux_archive" THIRD_PARTY_NOTICES.md > "$archive_notices"
tar -xOf "$linux_archive" THIRD_PARTY_GO_LICENSES.txt > "$archive_go_licenses"
unzip -p "$windows_archive" THIRD_PARTY_GO_LICENSES.txt > "$archive_windows_go_licenses"
tar -xOf "$linux_archive" THIRD_PARTY_JS_LICENSES.txt > "$archive_js_licenses"

require_exact_line 'THIRD_PARTY_NOTICES.md' "$linux_archive_members"
require_exact_line 'THIRD_PARTY_GO_LICENSES.txt' "$linux_archive_members"
require_exact_line 'THIRD_PARTY_JS_LICENSES.txt' "$linux_archive_members"
require_exact_line 'THIRD_PARTY_NOTICES.md' "$windows_archive_members"
require_exact_line 'THIRD_PARTY_GO_LICENSES.txt' "$windows_archive_members"
require_exact_line 'THIRD_PARTY_JS_LICENSES.txt' "$windows_archive_members"
require_exact_line '## SIL Open Font License 1.1' "$archive_notices"
require_exact_line '## dnd-kit' "$archive_notices"
require_exact_line '## Lucide icons' "$archive_notices"
require_matching_line '^github\.com/fsnotify/fsnotify ' "$archive_go_licenses"
require_matching_line '^charm\.land/huh/v2 ' "$archive_go_licenses"
require_matching_line '^github\.com/charmbracelet/x/termios ' "$archive_go_licenses"
require_matching_line '^charm\.land/huh/v2 ' "$archive_windows_go_licenses"
if grep -Eq '^github\.com/charmbracelet/x/termios ' "$archive_windows_go_licenses"; then
	echo "Windows archive includes the Unix-only terminal dependency" >&2
	exit 1
fi
require_exact_line '@internationalized/date 3.12.3' "$archive_js_licenses"
jq -e '[.packages[] | select(.SPDXID | startswith("SPDXRef-Package-JS-")) | .name] | index("@playwright/test") == null and index("prettier") == null and index("esm-env") == null and index("@internationalized/date") != null' \
	"$workspace/first/ridu.spdx.json" >/dev/null
jq -e '.packages[] | select(.name == "Lucide icons") | .licenseDeclared == "ISC AND MIT" and .externalRefs == null' \
	"$workspace/first/ridu.spdx.json" >/dev/null

rendered_inventory="$workspace/rendered-admin-dependencies.json"
RIDU_ADMIN_ASSET_CHECK=true RIDU_ADMIN_DEPENDENCY_INVENTORY="$rendered_inventory" \
	bun run --cwd "$repository_root/admin" build >/dev/null
jq -S '[.[] | {name, version, license: (.license // "NOASSERTION")}] | sort_by(.name, .version)' \
	"$rendered_inventory" > "$workspace/expected-js-inventory.json"
jq -S '[.packages[] | select(.SPDXID | startswith("SPDXRef-Package-JS-")) | {name, version: .versionInfo, license: .licenseDeclared}] | sort_by(.name, .version)' \
	"$workspace/first/ridu.spdx.json" > "$workspace/sbom-js-inventory.json"
diff -u "$workspace/expected-js-inventory.json" "$workspace/sbom-js-inventory.json"

jq 'map(if .name == "@fontsource-variable/geist" then .version = "99.0.0" else . end)' \
	"$rendered_inventory" > "$workspace/stale-notice-inventory.json"
if bun "$repository_root/scripts/generate-js-license-bundle.ts" \
	"$workspace/stale-notice-inventory.json" \
	"$repository_root/admin/THIRD_PARTY_NOTICES.md" \
	"$workspace/stale-notice-licenses.txt" >/dev/null 2>&1; then
	echo "stale reviewed JavaScript notices were accepted" >&2
	exit 1
fi
jq 'map(select(.name != "@fontsource-variable/geist"))' \
	"$rendered_inventory" > "$workspace/missing-notice-dependency-inventory.json"
if bun "$repository_root/scripts/generate-js-license-bundle.ts" \
	"$workspace/missing-notice-dependency-inventory.json" \
	"$repository_root/admin/THIRD_PARTY_NOTICES.md" \
	"$workspace/missing-notice-dependency-licenses.txt" >/dev/null 2>&1; then
	echo "orphaned reviewed JavaScript notices were accepted" >&2
	exit 1
fi

echo "reproducible release archives, SPDX SBOM, and checksums verified"
