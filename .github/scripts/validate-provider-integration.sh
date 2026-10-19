#!/usr/bin/env bash
set -euo pipefail

# Overlay the all native-provider integration overlay into a disposable SDK checkout
# of the pinned metatube-sdk-go repository.
if [[ $# -ne 1 ]]; then
  echo "usage: $0 /path/to/metatube-sdk-go" >&2
  exit 2
fi

workspace_root="$(cd "$(dirname "$0")/../.." && pwd)"
sdk_root="$(cd "$1" && pwd)"
providers=(airav avsox fc2club freejavbt giga iqqtv jav321 javdb javlibrary minnano xcity)
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
for provider in ("airav", "avsox", "fc2club", "freejavbt", "giga", "iqqtv", "jav321", "javdb", "javlibrary", "minnano", "xcity"):
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
	for _, name := range []string{"Airav", "AVSOX", "FC2Club", "Freejavbt", "GIGA", "IQQTV", "Jav321", "JavDB", "JavLibrary", "XCity"} {
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

cat > "$sdk_root/route/metax_integration_test.go" <<'EOF'
package route_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/metatube-community/metatube-sdk-go/engine"
	"github.com/metatube-community/metatube-sdk-go/route"
)

func TestMetaXProvidersEndpointListsIntegratedProviders(t *testing.T) {
	router := route.New(engine.New(nil), nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/providers", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /v1/providers status = %d, want %d; body: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var response struct {
		Data struct {
			ActorProviders map[string]string `json:"actor_providers"`
			MovieProviders map[string]string `json:"movie_providers"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode providers response: %v", err)
	}
	for _, name := range []string{"Airav", "AVSOX", "FC2Club", "Freejavbt", "GIGA", "IQQTV", "Jav321", "JavDB", "JavLibrary", "XCity"} {
		if _, ok := response.Data.MovieProviders[name]; !ok {
			t.Errorf("GET /v1/providers did not expose movie provider %q", name)
		}
	}
	for _, name := range []string{"JavDBActor", "MinnanoActor"} {
		if _, ok := response.Data.ActorProviders[name]; !ok {
			t.Errorf("GET /v1/providers did not expose actor provider %q", name)
		}
	}
}
EOF

gofmt -w "$sdk_root/engine/register.go" "$sdk_root/engine/metax_integration_test.go" "$sdk_root/route/metax_integration_test.go"

echo "Eleven-package native movie/actor integration overlay applied. Validate from: $sdk_root"
echo "Focused tests: go test ./engine ./route ./provider/airav ./provider/avsox ./provider/fc2club ./provider/freejavbt ./provider/giga ./provider/iqqtv ./provider/jav321 ./provider/javdb ./provider/javlibrary ./provider/minnano ./provider/xcity -count=1"
echo "Full regression: go test \$(go list ./... | grep -Ev \"github.com/metatube-community/metatube-sdk-go/translate\")"
echo "Server build: go build -o /tmp/metatube-server ./cmd/server"

cat > "$sdk_root/route/metax_api_contract_test.go" <<'EOF'
package route_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"golang.org/x/text/language"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"github.com/gin-gonic/gin"
		"github.com/glebarez/sqlite"

	"github.com/metatube-community/metatube-sdk-go/engine"
	"github.com/metatube-community/metatube-sdk-go/model"
	mt "github.com/metatube-community/metatube-sdk-go/provider"
	"github.com/metatube-community/metatube-sdk-go/route"
)

const (
	contractMovieName = "MetaXContractMovie"
	contractActorName = "MetaXContractActor"
)

type contractMovieProvider struct{}
func (*contractMovieProvider) Name() string { return contractMovieName }
func (*contractMovieProvider) Priority() float64 { return 1 }
func (*contractMovieProvider) SetPriority(float64) {}
func (*contractMovieProvider) Language() language.Tag { return language.English }
func (*contractMovieProvider) URL() *url.URL { u, _ := url.Parse("https://contract-movie.invalid"); return u }
func (*contractMovieProvider) NormalizeMovieID(id string) string { return id }
func (*contractMovieProvider) ParseMovieIDFromURL(raw string) (string, error) { u, err := url.Parse(raw); if err != nil { return "", err }; return u.Path, nil }
func (*contractMovieProvider) GetMovieInfoByID(id string) (*model.MovieInfo, error) { return contractMovieInfo(id), nil }
func (p *contractMovieProvider) GetMovieInfoByURL(raw string) (*model.MovieInfo, error) { id, _ := p.ParseMovieIDFromURL(raw); return p.GetMovieInfoByID(id) }
func (*contractMovieProvider) SearchMovie(string) ([]*model.MovieSearchResult, error) { return []*model.MovieSearchResult{contractMovieInfo("movie-123").ToSearchResult()}, nil }
func (*contractMovieProvider) NormalizeMovieKeyword(keyword string) string { return keyword }

type contractActorProvider struct{}
func (*contractActorProvider) Name() string { return contractActorName }
func (*contractActorProvider) Priority() float64 { return 1 }
func (*contractActorProvider) SetPriority(float64) {}
func (*contractActorProvider) Language() language.Tag { return language.English }
func (*contractActorProvider) URL() *url.URL { u, _ := url.Parse("https://contract-actor.invalid"); return u }
func (*contractActorProvider) NormalizeActorID(id string) string { return id }
func (*contractActorProvider) ParseActorIDFromURL(raw string) (string, error) { u, err := url.Parse(raw); if err != nil { return "", err }; return u.Path, nil }
func (*contractActorProvider) GetActorInfoByID(id string) (*model.ActorInfo, error) { return contractActorInfo(id), nil }
func (p *contractActorProvider) GetActorInfoByURL(raw string) (*model.ActorInfo, error) { id, _ := p.ParseActorIDFromURL(raw); return p.GetActorInfoByID(id) }
func (*contractActorProvider) SearchActor(string) ([]*model.ActorSearchResult, error) { return []*model.ActorSearchResult{contractActorInfo("actor-42").ToSearchResult()}, nil }

func contractMovieInfo(id string) *model.MovieInfo {
	return &model.MovieInfo{ID:id, Number:"TEST-123", Title:"Contract Movie", Provider:contractMovieName, Homepage:"https://contract-movie.invalid/"+id, CoverURL:"https://images.invalid/cover.jpg"}
}
func contractActorInfo(id string) *model.ActorInfo {
	return &model.ActorInfo{ID:id, Name:"Contract Actor", Provider:contractActorName, Homepage:"https://contract-actor.invalid/"+id}
}

func init() {
	mt.Register(contractMovieName, func() *contractMovieProvider { return &contractMovieProvider{} })
	mt.Register(contractActorName, func() *contractActorProvider { return &contractActorProvider{} })
}

func newContractRouter(t *testing.T) *gin.Engine {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:metax-api-contract?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil { t.Fatalf("open in-memory database: %v", err) }
	if err := db.AutoMigrate(&model.MovieInfo{}, &model.ActorInfo{}); err != nil { t.Fatalf("migrate test database: %v", err) }
	return route.New(engine.New(db), nil)
}

func requestJSON(t *testing.T, router http.Handler, target string) (int, map[string]any) {
	t.Helper()
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil { t.Fatalf("%s returned invalid JSON (%d): %v; body=%s", target, recorder.Code, err, recorder.Body.String()) }
	return recorder.Code, body
}

func TestMetaXSearchAndDetailAPIContracts(t *testing.T) {
	router := newContractRouter(t)
	cases := []struct { name, target, listKey, expectedID, expectedProvider string }{
		{"movie search", "/v1/movies/search?q=contract&provider="+contractMovieName+"&fallback=false", "", "movie-123", contractMovieName},
		{"actor search", "/v1/actors/search?q=Contract%20Actor&provider="+contractActorName+"&fallback=false", "", "actor-42", contractActorName},
		{"movie detail", "/v1/movies/"+contractMovieName+"/movie-123?lazy=false", "", "movie-123", contractMovieName},
		{"actor detail", "/v1/actors/"+contractActorName+"/actor-42?lazy=false", "", "actor-42", contractActorName},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, body := requestJSON(t, router, tc.target)
			if status != http.StatusOK { t.Fatalf("status=%d want=%d body=%v", status, http.StatusOK, body) }
			data, ok := body["data"]
			if !ok { t.Fatalf("response has no data field: %v", body) }
			var record map[string]any
			switch v := data.(type) {
			case []any:
				if len(v) != 1 { t.Fatalf("result count=%d want=1: %v", len(v), v) }
				record, ok = v[0].(map[string]any)
			case map[string]any:
				record = v
			default:
				t.Fatalf("unexpected data shape %T: %v", data, data)
			}
			if !ok { t.Fatalf("unexpected result record: %v", data) }
			if record["id"] != tc.expectedID { t.Errorf("id=%v want=%s", record["id"], tc.expectedID) }
			if record["provider"] != tc.expectedProvider { t.Errorf("provider=%v want=%s", record["provider"], tc.expectedProvider) }
			if tc.name == "movie search" || tc.name == "movie detail" {
				for _, key := range []string{"number", "title", "homepage", "cover_url"} {
					if record[key] == nil || record[key] == "" { t.Errorf("movie response missing %q: %v", key, record) }
				}
			} else {
				for _, key := range []string{"name", "homepage"} {
					if record[key] == nil || record[key] == "" { t.Errorf("actor response missing %q: %v", key, record) }
				}
			}
		})
	}
}

func TestMetaXSearchRequiresQuery(t *testing.T) {
	router := newContractRouter(t)
	for _, target := range []string{"/v1/movies/search", "/v1/actors/search"} {
		status, body := requestJSON(t, router, target)
		if status != http.StatusBadRequest { t.Errorf("%s status=%d want=%d body=%v", target, status, http.StatusBadRequest, body) }
		if body["error"] == nil { t.Errorf("%s error response missing error field: %v", target, body) }
	}
}
EOF


# Add the category router and category-scoped search implementation to the
# disposable SDK checkout. Existing SDK search result models and JSON envelope
# remain unchanged.
mkdir -p "$sdk_root/engine"
cp "$workspace_root/.github/overlays/metatube-sdk-go/engine/metax_search.go" "$sdk_root/engine/metax_search.go"
if [[ ! -e "$sdk_root/routing" ]]; then
  cp -R "$workspace_root/routing" "$sdk_root/routing"
fi

python3 - "$sdk_root/route/search.go" <<'PY'
from pathlib import Path
import sys

path = Path(sys.argv[1])
source = path.read_text()
if 'github.com/metatube-community/metatube-sdk-go/routing' not in source:
    source = source.replace(
        '"github.com/metatube-community/metatube-sdk-go/model"\n',
        '"github.com/metatube-community/metatube-sdk-go/model"\n'
        '"github.com/metatube-community/metatube-sdk-go/routing"\n',
        1,
    )
source = source.replace(
    'Provider string `form:"provider"`\n\tFallback bool   `form:"fallback"`',
    'Provider string `form:"provider"`\n\tCategory string `form:"category"`\n\tFallback bool   `form:"fallback"`',
    1,
)

old_handler = '''func getSearch(app *engine.Engine, typ searchType) gin.HandlerFunc {
\treturn func(c *gin.Context) {'''
new_handler = '''func getSearch(app *engine.Engine, typ searchType) gin.HandlerFunc {
\treturn getSearchWithConfig(app, typ, routing.DefaultConfig())
}

func getSearchWithConfig(app *engine.Engine, typ searchType, routingConfig routing.Config) gin.HandlerFunc {
\treturn func(c *gin.Context) {'''
if old_handler not in source:
    raise SystemExit("search handler declaration not found; SDK route/search.go changed")
source = source.replace(old_handler, new_handler, 1)
source = source.replace("routing.DefaultConfig().Resolve(routing.Category(query.Category), \"\", names)", "routingConfig.Resolve(routing.Category(query.Category), \"\", names)", 1)
old = '''} else if searchAll {
				results, err = app.SearchMovieAll(query.Q, query.Fallback)
			} else {
				results, err = app.SearchMovie(query.Q, query.Provider, query.Fallback)
			}'''
new = '''} else if searchAll {
				if query.Category == "" {
					results, err = app.SearchMovieAll(query.Q, query.Fallback)
				} else {
					registered := app.GetMovieProviders()
					names := make([]string, 0, len(registered))
					for name := range registered {
						names = append(names, name)
					}
					selected, resolveErr := routing.DefaultConfig().Resolve(routing.Category(query.Category), "", names)
					if resolveErr != nil {
						abortWithStatusMessage(c, http.StatusBadRequest, resolveErr)
						return
					}
					results, err = app.SearchMovieProviders(query.Q, selected, query.Fallback)
				}
			} else {
				results, err = app.SearchMovie(query.Q, query.Provider, query.Fallback)
			}'''
if old not in source:
    raise SystemExit("movie search dispatch block not found; SDK route/search.go changed")
source = source.replace(old, new, 1)
path.write_text(source)
PY

cp "$workspace_root/.github/overlays/metatube-sdk-go/route/metax_category_api_test.go" "$sdk_root/route/metax_category_api_test.go"
cp "$workspace_root/.github/overlays/metatube-sdk-go/route/metax_category_fanout_test.go" "$sdk_root/route/metax_category_fanout_test.go"

gofmt -w "$sdk_root/engine/metax_search.go" "$sdk_root/route/search.go" "$sdk_root/route/metax_category_api_test.go" "$sdk_root/route/metax_category_fanout_test.go"
