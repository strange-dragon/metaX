#!/usr/bin/env bash
set -euo pipefail

# Run the native providers against the live upstream websites for one known
# catalog number. This is deliberately separate from deterministic unit tests.
if [[ $# -ne 1 ]]; then
  echo "usage: $0 /path/to/metatube-sdk-go" >&2
  exit 2
fi

workspace_root="$(cd "$(dirname "$0")/../.." && pwd)"
sdk_root="$(cd "$1" && pwd)"
test_file="$sdk_root/route/moon023_live_smoke_test.go"
trap 'rm -f "$test_file"' EXIT

cat > "$test_file" <<'EOF'
package route_test

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/metatube-community/metatube-sdk-go/model"
	"github.com/metatube-community/metatube-sdk-go/provider/airav"
	"github.com/metatube-community/metatube-sdk-go/provider/avsox"
	"github.com/metatube-community/metatube-sdk-go/provider/fc2club"
	"github.com/metatube-community/metatube-sdk-go/provider/freejavbt"
	"github.com/metatube-community/metatube-sdk-go/provider/giga"
	"github.com/metatube-community/metatube-sdk-go/provider/iqqtv"
	"github.com/metatube-community/metatube-sdk-go/provider/jav321"
	"github.com/metatube-community/metatube-sdk-go/provider/javdb"
	"github.com/metatube-community/metatube-sdk-go/provider/javlibrary"
	"github.com/metatube-community/metatube-sdk-go/provider/xcity"
)

type moon023Provider interface {
	Name() string
	SearchMovie(string) ([]*model.MovieSearchResult, error)
	GetMovieInfoByID(string) (*model.MovieInfo, error)
	GetMovieInfoByURL(string) (*model.MovieInfo, error)
}

var nonAlphaNumeric = regexp.MustCompile(`[^A-Z0-9]+`)

func normalizedNumber(s string) string {
	return nonAlphaNumeric.ReplaceAllString(strings.ToUpper(strings.TrimSpace(s)), "")
}

func TestLiveMoon023Scrape(t *testing.T) {
	providers := []moon023Provider{
		airav.New(), avsox.New(), fc2club.New(), freejavbt.New(), giga.New(),
		iqqtv.New(), jav321.New(), javdb.New(), javlibrary.New(), xcity.New(),
	}
	matched := 0
	for _, p := range providers {
		t.Run(p.Name(), func(t *testing.T) {
			results, err := p.SearchMovie("MOON-023")
			if err != nil {
				t.Logf("SEARCH_ERROR provider=%s err=%v", p.Name(), err)
				return
			}
			t.Logf("SEARCH_OK provider=%s result_count=%d", p.Name(), len(results))
			var hit *model.MovieSearchResult
			for _, result := range results {
				if result != nil && normalizedNumber(result.Number) == "MOON023" {
					hit = result
					break
				}
			}
			if hit == nil {
				sample, _ := json.Marshal(results)
				if len(sample) > 1800 {
					sample = sample[:1800]
				}
				t.Logf("NO_EXACT_MATCH provider=%s results=%s", p.Name(), string(sample))
				return
			}
			matched++
			encoded, _ := json.Marshal(hit)
			t.Logf("EXACT_MATCH provider=%s result=%s", p.Name(), string(encoded))
			time.Sleep(500 * time.Millisecond)

			var info *model.MovieInfo
			detailErr := error(nil)
			if hit.ID != "" {
				info, detailErr = p.GetMovieInfoByID(hit.ID)
			}
			if (detailErr != nil || info == nil) && hit.Homepage != "" {
				info, detailErr = p.GetMovieInfoByURL(hit.Homepage)
			}
			if detailErr != nil {
				t.Logf("DETAIL_ERROR provider=%s id=%q url=%q err=%v", p.Name(), hit.ID, hit.Homepage, detailErr)
				return
			}
			if info == nil {
				t.Logf("DETAIL_EMPTY provider=%s id=%q url=%q", p.Name(), hit.ID, hit.Homepage)
				return
			}
			detail, _ := json.Marshal(info)
			t.Logf("DETAIL_OK provider=%s detail=%s", p.Name(), string(detail))
			if normalizedNumber(info.Number) != "MOON023" {
				t.Errorf("detail returned wrong number: provider=%s number=%q", p.Name(), info.Number)
			}
			if strings.TrimSpace(info.Title) == "" {
				t.Errorf("detail has empty title: provider=%s", p.Name())
			}
			if strings.TrimSpace(info.CoverURL) == "" {
				t.Errorf("detail has empty cover URL: provider=%s", p.Name())
			}
			fmt.Printf("MOON023_DETAIL provider=%s title=%q release=%v runtime=%d actors=%v maker=%q label=%q cover=%q\n",
				p.Name(), info.Title, info.ReleaseDate, info.Runtime, info.Actors, info.Maker, info.Label, info.CoverURL)
		})
		time.Sleep(1200 * time.Millisecond)
	}
	if matched == 0 {
		t.Errorf("none of the %d live providers returned an exact MOON-023 match; inspect per-provider logs for network blocks, unsupported catalogs, or parser issues", len(providers))
	}
}
EOF

echo "Running real upstream scrape for MOON-023 against 10 native movie providers..."
cd "$sdk_root"
go test ./route -run '^TestLiveMoon023Scrape$' -v -count=1 -timeout=8m
