#!/usr/bin/env bash
set -euo pipefail

# Build a reproducible metaX server with the maintained upstream SDK patches
# and all fourteen native provider packages currently covered by engine/API integration tests.
# Set METAX_SKIP_UPSTREAM_TESTS=1 to skip the upstream SDK suite that includes
# live-site tests; focused metaX integration tests still run.
if [[ $# -ne 2 ]]; then
  echo "usage: $0 /path/to/clean-metatube-sdk-go-checkout /path/to/output-binary" >&2
  exit 2
fi

workspace_root="$(cd "$(dirname "$0")/../.." && pwd)"
sdk_root="$(cd "$1" && pwd)"
output_binary="$2"
expected_commit="c62584675c593816cc07e7b5b977d14c7eb505a3"

command -v git >/dev/null
command -v go >/dev/null
command -v python3 >/dev/null
test -f "$sdk_root/go.mod"
test -f "$sdk_root/engine/register.go"

actual_commit="$(git -C "$sdk_root" rev-parse HEAD)"
if [[ "$actual_commit" != "$expected_commit" ]]; then
  echo "unexpected SDK revision: $actual_commit (expected $expected_commit)" >&2
  exit 1
fi
if [[ -n "$(git -C "$sdk_root" status --porcelain)" ]]; then
  echo "SDK checkout must be clean before applying metaX patches: $sdk_root" >&2
  exit 1
fi

patches=(
  0001-javbus-validate-detail-url.patch
  0002-avbase-preserve-prefixed-detail-url.patch
  0010-avbase-exact-search-identity.patch
  0003-dahlia-verify-exact-work-slug.patch
  0011-dahlia-validate-detail-url-origin.patch
  0012-kin8-validate-and-normalize-movie-id.patch
  0013-dahlia-faleno-wordpress-metadata.patch
  0014-dahlia-faleno-wordpress-search.patch
  0015-scraper-provider-cookies.patch
  0004-fc2-validate-detail-url.patch
  0005-getchu-normalize-and-validate-detail-url.patch
  0017-getchu-exact-catalog-search.patch
  0006-fc2ppvdb-validate-detail-url.patch
  0007-mgstage-validate-detail-url.patch
  0008-theporndb-validate-detail-url.patch
  0016-theporndb-config-setter.patch
  0009-fanza-validate-detail-url.patch
)

for patch in "${patches[@]}"; do
  patch_path="$workspace_root/patches/metatube-sdk-go/$patch"
  echo "Applying $patch"
  git -C "$sdk_root" apply --check "$patch_path"
  git -C "$sdk_root" apply "$patch_path"
done

# Pin the TLS-fingerprint client used by the JavLibrary provider in this disposable SDK workspace.
cd "$sdk_root"
go get github.com/bogdanfinn/tls-client@v1.14.0
cd "$workspace_root"

bash "$workspace_root/.github/scripts/validate-provider-integration.sh" "$sdk_root"
bash "$workspace_root/.github/scripts/integrate-category-routing.sh" "$sdk_root"

cd "$sdk_root"
echo "Running native provider registration and API discovery tests"
go test ./engine ./route ./provider/httpretry ./provider/avbaseactor ./provider/airav ./provider/avsox ./provider/fc2club ./provider/freejavbt ./provider/giga ./provider/iqqtv ./provider/jav321 ./provider/javdb ./provider/javlibrary ./provider/minnano ./provider/prestige ./provider/wikipediaactor ./provider/xcity ./provider/internal/scraper -count=1

echo "Checking formatting of patched SDK files"
format_files="cmd/cmd.go routing/routes.go routing/number.go routing/config.go routing/routes_test.go routing/number_test.go routing/config_test.go engine/metax_search.go engine/metax_aggregate.go engine/metax_aggregate_cache.go engine/metax_aggregate_db.go engine/metax_fanout.go engine/metax_request_cookies.go engine/metax_request_cookies_test.go engine/init.go engine/metax_secret_redaction.go engine/metax_secret_redaction_test.go engine/metax_aggregate_test.go engine/metax_aggregate_fanout_test.go engine/metax_aggregate_cache_test.go engine/metax_aggregate_db_test.go engine/metax_fanout_test.go engine/metax_integration_test.go route/info.go route/image.go route/search.go route/review.go route/route.go route/metax_request_cookies.go route/metax_request_cookies_test.go route/metax_request_cookies_alias_test.go route/metax_status.go route/metax_status_test.go route/metax_category_api_test.go route/metax_category_fanout_test.go route/metax_search_merge_test.go route/metax_aggregate_endpoint_test.go route/metax_test_helpers_test.go route/metax_api_contract_test.go route/metax_integration_test.go provider/httpretry/retry.go provider/httpretry/retry_test.go provider/prestige/prestige.go provider/prestige/prestige_test.go provider/avbaseactor/actor.go provider/avbaseactor/actor_test.go provider/wikipediaactor/actor.go provider/wikipediaactor/actor_test.go provider/javbus/javbus.go provider/javbus/javbus_test.go provider/avbase/avbase.go provider/avbase/avbase_test.go provider/dahlia/core/core.go provider/dahlia/core/core_test.go provider/dahlia/core/core_url_validation_test.go provider/fc2/fc2.go provider/fc2/fc2_test.go provider/getchu/getchu.go provider/getchu/getchu_migration_test.go provider/getchu/getchu_search_test.go provider/fc2ppvdb/fc2ppvdb.go provider/fc2ppvdb/fc2ppvdb_migration_test.go provider/mgstage/mgstage.go provider/mgstage/mgstage_test.go provider/theporndb/video.go provider/theporndb/actor.go provider/theporndb/theporndb.go provider/theporndb/config_test.go provider/theporndb/validation_test.go provider/fanza/fanza.go provider/fanza/fanza_migration_test.go provider/kin8tengoku/kin8tengoku.go provider/kin8tengoku/kin8tengoku_migration_test.go provider/dahlia/core/core_wordpress_metadata_test.go provider/dahlia/core/core_wordpress_search_test.go provider/internal/scraper/scraper.go provider/internal/scraper/scraper_test.go"
if [[ -n "$(gofmt -l $format_files)" ]]; then
  gofmt -d $format_files
  echo "gofmt check failed" >&2
  exit 1
fi

if [[ "${METAX_SKIP_UPSTREAM_TESTS:-0}" == "1" ]]; then
  echo "Skipping full pinned SDK regression suite (METAX_SKIP_UPSTREAM_TESTS=1)"
  echo "Focused native-provider integration tests already passed above"
else
  echo "Running full pinned SDK regression suite"
  go test $(go list ./... | grep -Ev "github.com/metatube-community/metatube-sdk-go/translate")
fi

mkdir -p "$(dirname "$output_binary")"

# Stamp the product version into the server's version package. Without this the
# root endpoint reports "unknown" because the pinned SDK only knows its own
# module version when built as a dependency.
metax_version="${METAX_VERSION:-0.1.0}"
metax_commit="${METAX_COMMIT:-}"
version_pkg="github.com/metatube-community/metatube-sdk-go/internal/version"
ldflags="-X ${version_pkg}.Version=${metax_version}"
if [[ -n "$metax_commit" ]]; then
  ldflags+=" -X ${version_pkg}.GitCommit=${metax_commit}"
fi

echo "Building metaX server: $output_binary (version $metax_version${metax_commit:+, commit $metax_commit})"
go build -ldflags "$ldflags" -o "$output_binary" ./cmd/server
echo "Build complete: $output_binary"
