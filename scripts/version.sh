#!/usr/bin/env bash
set -euo pipefail

# Print "<version> <commit>" for the server build.
#
# Precedence:
#   1. METAX_VERSION / METAX_COMMIT build arguments (CI or explicit release)
#   2. local git tags and HEAD (normal source checkout)
#   3. the repository VERSION file plus the deployed ref resolved from GitHub
#      (Docker build context without .git)
#
# The commit is optional. When present, the server reports
# "v<version>-<commit>".

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
repo_url="${METAX_GIT_URL:-https://github.com/strange-dragon/metaX.git}"
git_ref="${METAX_GIT_REF:-main}"

version="${METAX_VERSION:-}"
commit="${METAX_COMMIT:-}"
resolved_from_git=0

if [[ -z "$version" && -f "$root/VERSION" ]]; then
  version="$(tr -d '[:space:]' < "$root/VERSION")"
fi

if [[ -z "$commit" ]] && git -C "$root" rev-parse --short=7 HEAD >/dev/null 2>&1; then
  commit="$(git -C "$root" rev-parse --short=7 HEAD)"
fi

if [[ -z "${METAX_VERSION:-}" ]] && git -C "$root" describe --tags --abbrev=0 >/dev/null 2>&1; then
  version="$(git -C "$root" describe --tags --abbrev=0)"
  version="${version#v}"
  resolved_from_git=1
fi

# Docker excludes .git from the build context. Resolve the built ref from the
# remote so a push to main produces a new, traceable version automatically.
if [[ -z "$commit" ]]; then
  commit="$(git ls-remote --exit-code "$repo_url" "refs/heads/$git_ref" 2>/dev/null | cut -c1-7 || true)"
fi
if [[ -z "${METAX_VERSION:-}" && "$resolved_from_git" -eq 0 ]]; then
  latest_tag="$(git ls-remote --tags --refs "$repo_url" 2>/dev/null \
    | awk -F/ '{print $NF}' \
    | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' \
    | sort -V \
    | tail -n1 || true)"
  if [[ -n "$latest_tag" ]]; then
    version="${latest_tag#v}"
  fi
fi

if [[ -z "$version" ]]; then
  version="0.0.0"
fi

if [[ -n "$commit" ]]; then
  printf '%s %s\n' "$version" "$commit"
else
  printf '%s\n' "$version"
fi
