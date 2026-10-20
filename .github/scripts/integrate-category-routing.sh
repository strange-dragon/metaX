#!/usr/bin/env bash
set -euo pipefail

# Integrate Amane-derived category routing into the actual pinned MetaTube SDK
# build, not just into standalone metaX unit tests.
if [[ $# -ne 1 ]]; then
  echo "usage: $0 /path/to/metatube-sdk-go-checkout" >&2
  exit 2
fi

workspace_root="$(cd "$(dirname "$0")/../.." && pwd)"
sdk_root="$(cd "$1" && pwd)"

mkdir -p "$sdk_root/routing"
cp "$workspace_root/routing/routes.go" "$sdk_root/routing/routes.go"
cp "$workspace_root/.github/overlays/metatube-sdk-go/engine/metax_search.go" "$sdk_root/engine/metax_search.go"
cp "$workspace_root/.github/overlays/metatube-sdk-go/engine/metax_aggregate.go" "$sdk_root/engine/metax_aggregate.go"
cp "$workspace_root/.github/overlays/metatube-sdk-go/route/metax_category_api_test.go" "$sdk_root/route/metax_category_api_test.go"
cp "$workspace_root/.github/overlays/metatube-sdk-go/route/metax_category_fanout_test.go" "$sdk_root/route/metax_category_fanout_test.go"

python3 - "$sdk_root/route/search.go" <<'PY'
from pathlib import Path
import sys

path = Path(sys.argv[1])
source = path.read_text()

source = source.replace(
    'import (\n\t"net/http"\n\tpkgurl "net/url"\n',
    'import (\n\tstderrors "errors"\n\t"net/http"\n\tpkgurl "net/url"\n',
    1,
)
source = source.replace(
    '\t"github.com/metatube-community/metatube-sdk-go/errors"\n',
    '\tsdkerrors "github.com/metatube-community/metatube-sdk-go/errors"\n',
    1,
)
source = source.replace(
    '\t"github.com/metatube-community/metatube-sdk-go/model"\n',
    '\t"github.com/metatube-community/metatube-sdk-go/model"\n'
    '\t"github.com/metatube-community/metatube-sdk-go/routing"\n',
    1,
)
source = source.replace(
    '\tProvider string `form:"provider"`\n\tFallback bool   `form:"fallback"`',
    '\tProvider string `form:"provider"`\n\tCategory string `form:"category"`\n\tFallback bool   `form:"fallback"`',
    1,
)


if "func getSearchWithConfig(" not in source:
    source = source.replace(
        'func getSearch(app *engine.Engine, typ searchType) gin.HandlerFunc {',
        'func getSearch(app *engine.Engine, typ searchType) gin.HandlerFunc {\\n\\treturn getSearchWithConfig(app, typ, routing.DefaultConfig())\\n}\\n\\nfunc getSearchWithConfig(app *engine.Engine, typ searchType, config routing.Config) gin.HandlerFunc {',
        1,
    )

start = source.find('\t\tcase movieSearchType:')
end = source.find('\t\tdefault:', start)
if start < 0 or end < 0:
    raise SystemExit("could not locate movie search switch block")
source = source[:start] + '''\t\tcase movieSearchType:
\t\t\tif isValidURL {
\t\t\t\tresults, err = app.GetMovieInfoByURL(query.Q, true /* always lazy */)
\t\t\t} else if query.Provider != "" {
\t\t\t\t// An explicit provider remains a manual override, even if a category is supplied.
\t\t\t\tresults, err = app.SearchMovie(query.Q, query.Provider, query.Fallback)
\t\t\t} else if query.Category != "" {
\t\t\t\tregistered := make([]string, 0, len(app.GetMovieProviders()))
\t\t\t\tfor name := range app.GetMovieProviders() {
\t\t\t\t\tregistered = append(registered, name)
\t\t\t\t}
\t\t\t\tproviders, routeErr := config.Resolve(routing.Category(query.Category), "", registered)
\t\t\t\tif routeErr != nil {
\t\t\t\t\terr = routeErr
\t\t\t\t} else {
\t\t\t\t\tresults, err = app.SearchMovieProviders(query.Q, providers, query.Fallback)
\t\t\t\t}
\t\t\t} else if inferredCategory, ok := routing.InferCategory(query.Q); searchAll && ok {
\t\t\t\tregistered := make([]string, 0, len(app.GetMovieProviders()))
\t\t\t\tfor name := range app.GetMovieProviders() { registered = append(registered, name) }
\t\t\t\tproviders, routeErr := config.Resolve(inferredCategory, "", registered)
\t\t\t\tif routeErr != nil {
\t\t\t\t\t// Automatic classification is best-effort; preserve broad-search compatibility if no route provider is installed.
\t\t\t\t\tresults, err = app.SearchMovieAll(query.Q, query.Fallback)
\t\t\t\t} else { results, err = app.SearchMovieProviders(query.Q, providers, query.Fallback) }
\t\t\t} else if searchAll {
\t\t\t\tresults, err = app.SearchMovieAll(query.Q, query.Fallback)
\t\t\t} else { results, err = app.SearchMovie(query.Q, query.Provider, query.Fallback) }
''' + source[end:]

source = source.replace(
    'abortWithError(c, errors.FromCode(http.StatusNotFound))',
    'abortWithError(c, sdkerrors.FromCode(http.StatusNotFound))',
)
needle = '''\t\tif err != nil {
\t\t\tabortWithError(c, err)
\t\t\treturn
\t\t}'''
replacement = '''\t\tif err != nil {
\t\t\tif stderrors.Is(err, routing.ErrUnknownCategory) {
\t\t\t\tabortWithStatusMessage(c, http.StatusBadRequest, err)
\t\t\t\treturn
\t\t\t}
\t\t\tabortWithError(c, err)
\t\t\treturn
\t\t}'''
index = source.rfind(needle)
if index < 0:
    raise SystemExit("could not locate search error handler")
source = source[:index] + replacement + source[index + len(needle):]

required = [
    'stderrors "errors"',
    '"github.com/metatube-community/metatube-sdk-go/routing"',
    'Category string `form:"category"`',
    'func getSearch(app *engine.Engine, typ searchType) gin.HandlerFunc {',
    'app.SearchMovieProviders(query.Q, providers, query.Fallback)',
    'routing.InferCategory(query.Q)',
    'providers, routeErr := config.Resolve(routing.Category(query.Category), "", registered)',
    'stderrors.Is(err, routing.ErrUnknownCategory)',
]
missing = [item for item in required if item not in source]
if missing:
    raise SystemExit("could not integrate category routing; missing expected markers: " + repr(missing))
path.write_text(source)
PY


python3 - "$sdk_root/route/info.go" <<'PY'
from pathlib import Path
import sys

path = Path(sys.argv[1])
source = path.read_text()
source = source.replace(
    'type infoQuery struct {\n\tLazy bool `form:"lazy"`\n}',
    'type infoQuery struct {\n\tLazy      bool `form:"lazy"`\n\tAggregate bool `form:"aggregate"`\n}',
    1,
)
source = source.replace(
    '\t\tquery := &infoQuery{\n\t\t\tLazy: true, // enable lazy by default.\n\t\t}',
    '\t\tquery := &infoQuery{\n\t\t\tLazy: true, // enable lazy by default.\n\t\t\tAggregate: true, // aggregate movie metadata by default; callers may opt out.\n\t\t}',
    1,
)
source = source.replace(
    '\t\tcase movieInfoType:\n\t\t\tinfo, err = app.GetMovieInfoByProviderID(uri.AsProviderID(), query.Lazy)',
    '\t\tcase movieInfoType:\n\t\t\tif query.Aggregate {\n\t\t\t\tinfo, err = app.GetAggregatedMovieInfoByProviderID(uri.AsProviderID(), query.Lazy)\n\t\t\t} else {\n\t\t\t\tinfo, err = app.GetMovieInfoByProviderID(uri.AsProviderID(), query.Lazy)\n\t\t\t}',
    1,
)
required = [
    'Aggregate bool `form:"aggregate"`',
    'app.GetAggregatedMovieInfoByProviderID(uri.AsProviderID(), query.Lazy)',
    'if query.Aggregate {',
]
missing = [item for item in required if item not in source]
if missing:
    raise SystemExit("could not patch detail aggregation endpoint; missing expected markers: " + repr(missing))
path.write_text(source)
PY

gofmt -w "$sdk_root/routing/routes.go" "$sdk_root/engine/metax_search.go" "$sdk_root/engine/metax_aggregate.go" "$sdk_root/route/info.go" "$sdk_root/route/search.go" "$sdk_root/route/metax_category_api_test.go" "$sdk_root/route/metax_category_fanout_test.go"
echo "Integrated Amane category routes, field-level movie aggregation, source records, and SDK-level API/fan-out tests."
