#!/usr/bin/env bash
set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
metadata_root="$repository_root/.ridu/release-metadata"
dependency_inventory="$metadata_root/admin-dependencies.json"
javascript_licenses="$metadata_root/THIRD_PARTY_JS_LICENSES.txt"
source_date_epoch="${SOURCE_DATE_EPOCH:-$(git -C "$repository_root" show -s --format=%ct HEAD)}"
if [[ ! "$source_date_epoch" =~ ^[1-9][0-9]*$ ]]; then
	echo "SOURCE_DATE_EPOCH must be a positive Unix timestamp" >&2
	exit 2
fi

mkdir -p "$metadata_root"
(
	cd "$repository_root"
	RIDU_ADMIN_ASSET_CHECK=true \
		RIDU_ADMIN_DEPENDENCY_INVENTORY="$dependency_inventory" \
		bun run --cwd admin build >/dev/null
	bun ./scripts/generate-js-license-bundle.ts \
		"$dependency_inventory" \
		./admin/THIRD_PARTY_NOTICES.md \
		"$javascript_licenses" >/dev/null
)
bun -e '
	const { utimesSync } = require("node:fs");
	const epoch = Number(process.argv[1]);
	for (const path of process.argv.slice(2)) utimesSync(path, epoch, epoch);
' "$source_date_epoch" "$dependency_inventory" "$javascript_licenses"

echo "prepared bundled admin dependency and licence metadata for GoReleaser"
