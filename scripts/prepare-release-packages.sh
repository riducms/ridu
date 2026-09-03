#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
	echo "usage: prepare-release-packages.sh <output-directory>" >&2
	exit 2
fi

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
output_root="$1"
release_workspace="$(mktemp -d "${TMPDIR:-/tmp}/ridu-npm-release.XXXXXX")"
cleanup() {
	chmod -R u+w "$release_workspace" 2>/dev/null || true
	rm -rf -- "$release_workspace"
}
trap cleanup EXIT
export GOMODCACHE="$release_workspace/go-module-cache"
export GOCACHE="$release_workspace/go-build-cache"
version="$(bun "$repository_root/scripts/check-release-version.ts" --print)"
project_root="$release_workspace/release-package-stage"
mkdir -p "$output_root"
output_root="$(cd "$output_root" && pwd)"

(
	cd "$repository_root"
	go run ./internal/dogfood/new \
		--release "v$version" \
		--module example.com/ridu-release-packages \
		--scope @ridu-release-packages \
		--target "$project_root"
)

for package_directory in "$project_root/.ridu/packages"/*; do
	package_target="$(basename "$package_directory")"
	(
		cd "$package_directory"
		bun pm pack \
			--filename "$output_root/$package_target.tgz" \
			--ignore-scripts \
			--quiet
	)
done

echo "prepared twelve release-shaped npm tarballs in $output_root"
