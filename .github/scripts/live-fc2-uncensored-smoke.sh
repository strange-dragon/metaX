#!/usr/bin/env bash
set -euo pipefail

# Validate the Amane-derived FC2 and uncensored routes against two real catalog
# numbers: one FC2 PPV ID and one underscore-style uncensored number.
if [[ $# -ne 1 ]]; then
  echo "usage: $0 /path/to/integrated/metatube-sdk-go" >&2
  exit 2
fi

sdk_root="$(cd "$1" && pwd)"
test_file="$sdk_root/route/fc2_uncensored_live_smoke_test.go"
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
	"github.com/metatube-community/metatube-sdk-go/provider/fc2"
	"github.com/metatube-community/metatube-sdk-go/provider/fc2ppvdb"
	"github.com/metatube-community/metatube-sdk-go/provider/freejavbt"
	"github.com/metatube-community/metatube-sdk-go/provider/javbus"
	"github.com/metatube-community/metatube-sdk-go/provider/javdb"
)

type liveProvider interface {
	Name() string
	SearchMovie(string) ([]*model.MovieSearchResult, error)
	GetMovieInfoByID(string) (*model.MovieInfo, error)
	GetMovieInfoByURL(string) (*model.MovieInfo, error)
}

type routeCase struct {
	name      string
	number    string
	providers []liveProvider
}

var fc2NumberRE = regexp.MustCompile(`(?i)^fc2(?:-?ppv)?-?(\d+)$`)

func normalizedNumber(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, " ", "")
	if m := fc2NumberRE.FindStringSubmatch(s); len(m) == 2 {
		return "fc2:" + m[1]
	}
	// Underscores are significant in Amane's number matching rules.
	return s
}

func TestLiveFC2AndUncensoredRoutes(t *testing.T) {
	fc2Providers := []liveProvider{javdb.New(), fc2ppvdb.New(), fc2.New(), freejavbt.New(), airav.New()}
	uncensoredProviders := []liveProvider{javdb.New(), javbus.New(), avsox.New(), freejavbt.New()}
	cases := []routeCase{
		{name: "fc2", number: "fc2-ppv3954834", providers: fc2Providers},
		{name: "uncensored", number: "101926_01", providers: uncensoredProviders},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			matched := 0
			for _, p := range tc.providers {
				t.Run(p.Name(), func(t *testing.T) {
					results, err := p.SearchMovie(tc.number)
					if err != nil {
						t.Logf("SEARCH_ERROR category=%s number=%s provider=%s err=%v", tc.name, tc.number, p.Name(), err)
						return
					}
					t.Logf("SEARCH_OK category=%s number=%s provider=%s result_count=%d", tc.name, tc.number, p.Name(), len(results))
					var hit *model.MovieSearchResult
					for _, result := range results {
						if result != nil && normalizedNumber(result.Number) == normalizedNumber(tc.number) {
							hit = result
							break
						}
					}
					if hit == nil {
						sample, _ := json.Marshal(results)
						if len(sample) > 1200 {
							sample = sample[:1200]
						}
						t.Logf("NO_EXACT_MATCH category=%s number=%s provider=%s results=%s", tc.name, tc.number, p.Name(), string(sample))
						return
					}
					matched++
					encoded, _ := json.Marshal(hit)
					t.Logf("EXACT_MATCH category=%s number=%s provider=%s result=%s", tc.name, tc.number, p.Name(), string(encoded))
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
						t.Logf("DETAIL_ERROR category=%s number=%s provider=%s err=%v", tc.name, tc.number, p.Name(), detailErr)
						return
					}
					if info == nil {
						t.Logf("DETAIL_EMPTY category=%s number=%s provider=%s", tc.name, tc.number, p.Name())
						return
					}
					detail, _ := json.Marshal(info)
					t.Logf("DETAIL_OK category=%s number=%s provider=%s detail=%s", tc.name, tc.number, p.Name(), string(detail))
					if normalizedNumber(info.Number) != normalizedNumber(tc.number) {
						t.Errorf("detail returned wrong number: category=%s provider=%s got=%q want=%q", tc.name, p.Name(), info.Number, tc.number)
					}
					if strings.TrimSpace(info.Title) == "" {
						t.Errorf("detail has empty title: category=%s provider=%s", tc.name, p.Name())
					}
					if strings.TrimSpace(info.CoverURL) == "" {
						t.Errorf("detail has empty cover URL: category=%s provider=%s", tc.name, p.Name())
					}
					fmt.Printf("LIVE_DETAIL category=%s number=%s provider=%s title=%q release=%v actors=%v maker=%q label=%q cover=%q\n",
						tc.name, tc.number, p.Name(), info.Title, info.ReleaseDate, info.Actors, info.Maker, info.Label, info.CoverURL)
				})
				time.Sleep(1200 * time.Millisecond)
			}
			if matched == 0 {
				t.Errorf("none of the %d Amane-routed providers returned an exact match for category=%s number=%s; inspect per-provider logs for network blocks, unsupported catalogs, or parser issues", len(tc.providers), tc.name, tc.number)
			}
		})
	}
}
EOF

cd "$sdk_root"
echo "Running live smoke tests for Amane's FC2 and uncensored routes..."
go test ./route -run '^TestLiveFC2AndUncensoredRoutes$' -v -count=1 -timeout=10m
