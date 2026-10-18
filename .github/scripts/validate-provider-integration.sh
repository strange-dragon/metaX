#!/usr/bin/env bash
set -euo pipefail

# Apply the first native-provider integration prototype to a disposable checkout
# of the pinned metatube-sdk-go repository.
if [[ $# -ne 1 ]]; then
  echo "usage: $0 /path/to/metatube-sdk-go" >&2
  exit 2
fi

workspace_root="$(cd "$(dirname "$0")/../.." && pwd)"
sdk_root="$(cd "$1" && pwd)"
source_dir="$workspace_root/provider/jav321"
target_dir="$sdk_root/provider/jav321"

test -d "$source_dir"
test -f "$sdk_root/engine/register.go"
if [[ -e "$target_dir" ]]; then
  echo "refusing to overwrite existing directory: $target_dir" >&2
  exit 1
fi

cp -R "$source_dir" "$target_dir"

python3 - "$sdk_root/engine/register.go" <<'PY'
from pathlib import Path
import sys

path = Path(sys.argv[1])
text = path.read_text()
line = '\t_ "github.com/metatube-community/metatube-sdk-go/provider/jav321"'
if line not in text:
    marker = 'import (\n'
    if marker not in text:
        raise SystemExit("engine/register.go import block not found")
    text = text.replace(marker, marker + line + '\n', 1)
    path.write_text(text)
PY

cat > "$sdk_root/engine/metax_integration_test.go" <<'EOF'
package engine

import "testing"

func TestMetaXJav321ProviderIsRegistered(t *testing.T) {
	e := New(nil)
	if !e.IsMovieProvider("Jav321") {
		t.Fatal("Jav321 provider was not discovered by the SDK engine")
	}
}
EOF

gofmt -w "$sdk_root/engine/register.go" "$sdk_root/engine/metax_integration_test.go"

echo "Prototype applied. Run the SDK's normal test suite from: $sdk_root"
echo "At minimum: go test ./engine ./provider/jav321"
