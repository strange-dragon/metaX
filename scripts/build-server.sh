#!/usr/bin/env bash
set -euo pipefail

# Build the metaX server with an automatically derived version.
#
# Usage: scripts/build-server.sh [output-path]
#
# Version resolution precedence is documented in scripts/version.sh. Override
# it explicitly with METAX_VERSION / METAX_COMMIT when producing a release.

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
output="${1:-$root/dist/metax-server}"

read -r version commit < <("$root/scripts/version.sh")
version_pkg="github.com/strange-dragon/metaX/internal/version"
ldflags="-X ${version_pkg}.Version=${version}"
if [[ -n "$commit" ]]; then
  ldflags+=" -X ${version_pkg}.GitCommit=${commit}"
fi

mkdir -p "$(dirname "$output")"
echo "Building metaX server: ${version}${commit:+ (${commit})} -> $output"
cd "$root"
go build -ldflags "$ldflags" -o "$output" ./cmd/metax-server
echo "Build complete: $output"
