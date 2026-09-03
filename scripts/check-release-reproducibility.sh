#!/usr/bin/env bash
set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
release_check_dir="$(mktemp -d "${TMPDIR:-/tmp}/ridu-release-check.XXXXXX")"
trap 'rm -rf -- "$release_check_dir"' EXIT

release_version="${RIDU_RELEASE_CHECK_VERSION:-v0.0.0-release-check}"
if [[ ! "$release_version" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$ ]]; then
	echo "RIDU_RELEASE_CHECK_VERSION must be a v-prefixed semantic release" >&2
	exit 2
fi
first_binary="$release_check_dir/ridu-first"
second_binary="$release_check_dir/ridu-second"

build_cli() {
	local output="$1"
	(
		cd "$repository_root"
		CGO_ENABLED=0 GOFLAGS=-mod=readonly go build \
			-trimpath \
			-buildvcs=false \
			-ldflags="-buildid= -X main.version=$release_version" \
			-o "$output" \
			./cmd/ridu
	)
}

build_cli "$first_binary"
build_cli "$second_binary"

if ! cmp -s "$first_binary" "$second_binary"; then
	echo "release CLI builds differ despite identical inputs" >&2
	exit 1
fi

reported_version="$($first_binary version)"
expected_version="ridu $release_version"
if [[ "$reported_version" != "$expected_version" ]]; then
	echo "release CLI reported $reported_version, want $expected_version" >&2
	exit 1
fi

go version -m "$first_binary" >/dev/null
echo "reproducible release CLI build verified ($release_version)"
