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
providers=(jav321 airav javdb minnano)
test -f "$sdk_root/engine/register.go"

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

python3 - "$sdk_root/engine/register.go" <<'PY'
from pathlib import Path
import sys

path = Path(sys.argv[1])
text = path.read_text()
marker = "import (\n"
if marker not in text:
    raise SystemExit("engine/register.go import block not found")
for provider in ("jav321", "airav", "javdb", "minnano"):
    line = f'\t_ "github.com/metatube-community/metatube-sdk-go/provider/{provider}"'
    if line not in text:
        text = text.replace(marker, marker + line + "\n", 1)
        marker = marker + line + "\n"
path.write_text(text)
PY

cat > "$sdk_root/engine/metax_integration_test.go" <<'EOF'
package engine

import "testing"

func TestMetaXNativeProvidersAreRegistered(t *testing.T) {
	e := New(nil)
	for _, name := range []string{"Jav321", "Airav"} {
		if !e.IsMovieProvider(name) {
			t.Errorf("movie provider %s was not discovered by the SDK engine", name)
		}
	}
	for _, name := range []string{"JavDBActor", "MinnanoActor"} {
		if !e.IsActorProvider(name) {
			t.Errorf("actor provider %s was not discovered by the SDK engine", name)
		}
	}
}
EOF

gofmt -w "$sdk_root/engine/register.go" "$sdk_root/engine/metax_integration_test.go"

echo "Four-package movie/actor integration overlay applied. Run the SDK test suite from: $sdk_root"
echo "At minimum: go test ./engine ./provider/jav321 ./provider/airav ./provider/javdb ./provider/minnano"
