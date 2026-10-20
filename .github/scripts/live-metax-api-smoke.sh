#!/usr/bin/env bash
set -euo pipefail

# Exercise the integrated MetaX HTTP API against live providers. This is a
# point-in-time smoke test, not a deterministic regression test: network
# blocks, regional restrictions, and upstream site changes can make it fail.
# It is intended for workflow_dispatch or a local manual run.
if [[ $# -lt 1 || $# -gt 2 ]]; then
  echo "usage: $0 /path/to/metatube-server [port]" >&2
  exit 2
fi

server="$(cd "$(dirname "$1")" && pwd)/$(basename "$1")"
port="${2:-18080}"
tmp="$(mktemp -d)"
pid=""

cleanup() {
  if [[ -n "$pid" ]]; then
    kill "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
  fi
  rm -rf "$tmp"
}
trap cleanup EXIT

request() {
  local name="$1" target="$2" code
  code="$(curl -sS --max-time 120 -o "$tmp/$name.json" -w '%{http_code}' \
    "http://127.0.0.1:$port$target" 2>/dev/null || echo 000)"
  printf '%s' "$code"
}

require_status() {
  local name="$1" want="$2" got="$3"
  if [[ "$got" != "$want" ]]; then
    echo "FAILED: $name returned HTTP $got, want $want" >&2
    cat "$tmp/$name.json" >&2 2>/dev/null || true
    exit 1
  fi
}

require_json() {
  local name="$1" filter="$2"
  if ! jq -e "$filter" "$tmp/$name.json" >/dev/null 2>&1; then
    echo "FAILED: $name does not satisfy: $filter" >&2
    jq . "$tmp/$name.json" >&2 2>/dev/null || cat "$tmp/$name.json" >&2
    exit 1
  fi
  echo "OK: $name"
}

( cd "$tmp" && "$server" --port "$port" --request-timeout=20s >"$tmp/server.log" 2>&1 ) &
pid=$!

ready=0
for _ in $(seq 1 30); do
  if ! kill -0 "$pid" 2>/dev/null; then
    echo "FAILED: server exited during startup" >&2
    cat "$tmp/server.log" >&2
    exit 1
  fi
  if curl -sS --max-time 2 -o /dev/null "http://127.0.0.1:$port/" 2>/dev/null; then
    ready=1
    break
  fi
  sleep 1
done
if [[ "$ready" != 1 ]]; then
  echo "FAILED: server did not become ready" >&2
  cat "$tmp/server.log" >&2
  exit 1
fi

grep -q 'metaX routing: categories=' "$tmp/server.log" || {
  echo "FAILED: startup fingerprint log is missing" >&2
  cat "$tmp/server.log" >&2
  exit 1
}

code="$(request censored-search '/v1/movies/search?q=ABP-123&category=censored&fallback=false')"
require_status censored-search 200 "$code"
require_json censored-search '.data[0].provider == "MetaX" and .data[0].id == "ABP-123" and (.data | length) >= 2'

code="$(request censored-detail '/v1/movies/MetaX/ABP-123?category=censored')"
require_status censored-detail 200 "$code"
require_json censored-detail '.data.provider == "MetaX" and .data.id == "ABP-123" and .data.category == "censored" and (.data.title | length) > 0 and (.data.cover_url | length) > 0 and ([.data.sources[] | select(.status == "success")] | length) >= 1'

image_code="$(curl -sS --max-time 60 -D "$tmp/image.headers" -o "$tmp/image.bin" -w '%{http_code}' \
  "http://127.0.0.1:$port/v1/images/primary/MetaX/ABP-123?category=censored" 2>/dev/null || echo 000)"
if [[ "$image_code" != 200 ]] || ! grep -qi '^content-type: image/' "$tmp/image.headers" || [[ ! -s "$tmp/image.bin" ]]; then
  echo "FAILED: image request returned HTTP $image_code or no image bytes" >&2
  cat "$tmp/image.headers" >&2 2>/dev/null || true
  exit 1
fi
echo "OK: censored image"

code="$(request fc2-search '/v1/movies/search?q=FC2-PPV-3954834&category=uncensored&fallback=false')"
require_status fc2-search 200 "$code"
require_json fc2-search '.data[0].provider == "MetaX" and (.data[0].number | test("3954834"))'

code="$(request invalid-category '/v1/movies/search?q=ABP-123&category=not-a-category')"
require_status invalid-category 400 "$code"

set +e
( cd "$tmp" && METAX_ROUTES_FINGERPRINT=deadbeef "$server" --port "$((port + 1))" >"$tmp/pin.log" 2>&1 )
pin_status=$?
set -e
if [[ "$pin_status" -eq 0 ]] || ! grep -q 'fingerprint mismatch' "$tmp/pin.log"; then
  echo "FAILED: an incorrect METAX_ROUTES_FINGERPRINT did not abort startup" >&2
  cat "$tmp/pin.log" >&2
  exit 1
fi
echo "OK: fingerprint pin mismatch aborts startup"

echo "LIVE_API_SMOKE_OK"
