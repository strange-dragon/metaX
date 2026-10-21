#!/usr/bin/env bash
set -euo pipefail

# Overlay the native-provider integration into a disposable SDK checkout of the
# pinned metatube-sdk-go repository.
#
# This script only copies provider packages, registers them with the SDK engine,
# and copies the integration/contract tests. Route and aggregation integration
# live in integrate-category-routing.sh so that route/search.go is patched by
# exactly one script.
if [[ $# -ne 1 ]]; then
  echo "usage: $0 /path/to/metatube-sdk-go" >&2
  exit 2
fi

workspace_root="$(cd "$(dirname "$0")/../.." && pwd)"
sdk_root="$(cd "$1" && pwd)"
providers=(avbaseactor airav avsox fc2club freejavbt giga iqqtv jav321 javdb javlibrary minnano prestige wikipediaactor xcity)
test -f "$sdk_root/engine/register.go"

retry_target="$sdk_root/provider/httpretry"
test -d "$workspace_root/provider/httpretry"
if [[ -e "$retry_target" ]]; then
  echo "refusing to overwrite existing directory: $retry_target" >&2
  exit 1
fi
cp -R "$workspace_root/provider/httpretry" "$retry_target"

for provider in "${providers[@]}"; do
  source_dir="$workspace_root/provider/$provider"
  target_dir="$sdk_root/provider/$provider"
  test -d "$source_dir"
  if [[ -e "$target_dir" ]]; then
    echo "refusing to overwrite existing directory: $target_dir" >&2
    exit 1
  fi
  cp -R "$source_dir" "$target_dir"
done

python3 - "$sdk_root" <<'PY'
from pathlib import Path
import sys

sdk_root = Path(sys.argv[1])
old = "github.com/strange-dragon/metaX/provider/httpretry"
new = "github.com/metatube-community/metatube-sdk-go/provider/httpretry"
for path in (sdk_root / "provider").glob("*/**/*.go"):
    if path.parts[-2] == "httpretry":
        continue
    source = path.read_text()
    if old in source:
        path.write_text(source.replace(old, new))
PY

python3 - "$sdk_root/engine/register.go" <<'PY'
from pathlib import Path
import sys

path = Path(sys.argv[1])
text = path.read_text()
marker = "import (\n"
if marker not in text:
    raise SystemExit("engine/register.go import block not found")
for provider in ("avbaseactor", "airav", "avsox", "fc2club", "freejavbt", "giga", "iqqtv", "jav321", "javdb", "javlibrary", "minnano", "prestige", "wikipediaactor", "xcity"):
    line = f'\t_ "github.com/metatube-community/metatube-sdk-go/provider/{provider}"'
    if line not in text:
        text = text.replace(marker, marker + line + "\n", 1)
        marker = marker + line + "\n"
path.write_text(text)
PY

overlay="$workspace_root/.github/overlays/metatube-sdk-go"
cp "$overlay/engine/metax_integration_test.go" "$sdk_root/engine/metax_integration_test.go"
cp "$overlay/route/metax_integration_test.go" "$sdk_root/route/metax_integration_test.go"
cp "$overlay/route/metax_api_contract_test.go" "$sdk_root/route/metax_api_contract_test.go"
cp "$overlay/route/metax_test_helpers_test.go" "$sdk_root/route/metax_test_helpers_test.go"

gofmt -w "$sdk_root/engine/register.go" "$sdk_root/engine/metax_integration_test.go" \
  "$sdk_root/route/metax_integration_test.go" "$sdk_root/route/metax_api_contract_test.go" \
  "$sdk_root/route/metax_test_helpers_test.go"

echo "Native movie/actor provider overlay applied. Validate from: $sdk_root"
echo "Focused tests: go test ./engine ./route ./provider/httpretry ./provider/avbaseactor ./provider/airav ./provider/avsox ./provider/fc2club ./provider/freejavbt ./provider/giga ./provider/iqqtv ./provider/jav321 ./provider/javdb ./provider/javlibrary ./provider/minnano ./provider/prestige ./provider/wikipediaactor ./provider/xcity -count=1"
echo "Full regression: go test \$(go list ./... | grep -Ev \"github.com/metatube-community/metatube-sdk-go/translate\")"
echo "Server build: go build -o /tmp/metatube-server ./cmd/server"
