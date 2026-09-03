#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 2 ]]; then
	echo "usage: prepare-release-binary-metadata.sh <goreleaser-target> <binary>" >&2
	exit 2
fi

target="$1"
binary="$2"
if [[ ! "$target" =~ ^[a-z0-9._-]+$ ]]; then
	echo "invalid GoReleaser target: $target" >&2
	exit 2
fi
if [[ ! -f "$binary" ]]; then
	echo "GoReleaser binary does not exist: $binary" >&2
	exit 2
fi

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
metadata_root="$repository_root/.ridu/release-metadata"
target_root="$metadata_root/$target"
dependency_inventory="$metadata_root/admin-dependencies.json"
source_date_epoch="${SOURCE_DATE_EPOCH:-$(git -C "$repository_root" show -s --format=%ct HEAD)}"
if [[ ! "$source_date_epoch" =~ ^[1-9][0-9]*$ ]]; then
	echo "SOURCE_DATE_EPOCH must be a positive Unix timestamp" >&2
	exit 2
fi

mkdir -p "$target_root"
(
	cd "$repository_root"
	go run ./internal/releaselicense \
		--binary "$binary" \
		--output "$target_root/THIRD_PARTY_GO_LICENSES.txt"
)

if [[ "$target" == linux_amd64* ]]; then
	SOURCE_DATE_EPOCH="$source_date_epoch" \
		bun "$repository_root/scripts/generate-release-sbom.ts" \
		"$metadata_root/ridu.spdx.json" \
		"$binary" \
		"$dependency_inventory"
fi

release_files=("$binary" "$target_root/THIRD_PARTY_GO_LICENSES.txt")
if [[ "$target" == linux_amd64* ]]; then
	release_files+=("$metadata_root/ridu.spdx.json")
fi
bun -e '
	const { utimesSync } = require("node:fs");
	const epoch = Number(process.argv[1]);
	for (const path of process.argv.slice(2)) utimesSync(path, epoch, epoch);
' "$source_date_epoch" "${release_files[@]}"
