#!/usr/bin/env bash
set -euo pipefail

# Integrate Amane-derived category routing and field-level movie aggregation
# into the actual pinned MetaTube SDK build, not just into standalone metaX
# unit tests.
#
# This is the single script that patches route/search.go. Running it twice is
# safe: every replacement is guarded by a marker check.
if [[ $# -ne 1 ]]; then
  echo "usage: $0 /path/to/metatube-sdk-go-checkout" >&2
  exit 2
fi

workspace_root="$(cd "$(dirname "$0")/../.." && pwd)"
sdk_root="$(cd "$1" && pwd)"
overlay="$workspace_root/.github/overlays/metatube-sdk-go"

mkdir -p "$sdk_root/routing"
cp "$workspace_root"/routing/*.go "$sdk_root/routing/"
cp "$overlay/engine/metax_search.go" "$sdk_root/engine/metax_search.go"
cp "$overlay/engine/metax_aggregate.go" "$sdk_root/engine/metax_aggregate.go"
cp "$overlay/engine/metax_aggregate_cache.go" "$sdk_root/engine/metax_aggregate_cache.go"
cp "$overlay/engine/metax_aggregate_db.go" "$sdk_root/engine/metax_aggregate_db.go"
cp "$overlay/engine/metax_aggregate_test.go" "$sdk_root/engine/metax_aggregate_test.go"
cp "$overlay/engine/metax_aggregate_fanout_test.go" "$sdk_root/engine/metax_aggregate_fanout_test.go"
cp "$overlay/engine/metax_aggregate_cache_test.go" "$sdk_root/engine/metax_aggregate_cache_test.go"
cp "$overlay/engine/metax_aggregate_db_test.go" "$sdk_root/engine/metax_aggregate_db_test.go"
cp "$overlay/route/metax_category_api_test.go" "$sdk_root/route/metax_category_api_test.go"
cp "$overlay/route/metax_test_helpers_test.go" "$sdk_root/route/metax_test_helpers_test.go"
cp "$overlay/route/metax_category_fanout_test.go" "$sdk_root/route/metax_category_fanout_test.go"
cp "$overlay/route/metax_aggregate_endpoint_test.go" "$sdk_root/route/metax_aggregate_endpoint_test.go"

python3 - "$sdk_root/route/search.go" <<'PY'
from pathlib import Path
import sys

path = Path(sys.argv[1])
source = path.read_text()

if 'stderrors "errors"' not in source:
    marker = 'import (\n\t"net/http"\n\tpkgurl "net/url"\n'
    if marker not in source:
        raise SystemExit("search.go import block not found; SDK route/search.go changed")
    source = source.replace(marker, 'import (\n\tstderrors "errors"\n\t"net/http"\n\tpkgurl "net/url"\n', 1)

if 'sdkerrors "github.com/metatube-community/metatube-sdk-go/errors"' not in source:
    source = source.replace(
        '\t"github.com/metatube-community/metatube-sdk-go/errors"\n',
        '\tsdkerrors "github.com/metatube-community/metatube-sdk-go/errors"\n',
        1,
    )

if '"github.com/metatube-community/metatube-sdk-go/routing"' not in source:
    source = source.replace(
        '\t"github.com/metatube-community/metatube-sdk-go/model"\n',
        '\t"github.com/metatube-community/metatube-sdk-go/model"\n'
        '\t"github.com/metatube-community/metatube-sdk-go/routing"\n',
        1,
    )

if 'Category string `form:"category"`' not in source:
    source = source.replace(
        '\tProvider string `form:"provider"`\n\tFallback bool   `form:"fallback"`',
        '\tProvider string `form:"provider"`\n\tCategory string `form:"category"`\n\tFallback bool   `form:"fallback"`',
        1,
    )

if '"strings"' not in source:
    source = source.replace(
        'import (\n\tstderrors "errors"\n\t"net/http"\n\tpkgurl "net/url"\n',
        'import (\n\tstderrors "errors"\n\t"net/http"\n\tpkgurl "net/url"\n\t"strings"\n',
        1,
    )

if "func getSearchWithConfig(" not in source:
    source = source.replace(
        'func getSearch(app *engine.Engine, typ searchType) gin.HandlerFunc {',
        'func getSearch(app *engine.Engine, typ searchType) gin.HandlerFunc {\n'
        '\treturn getSearchWithConfig(app, typ, routing.ActiveConfig())\n'
        '}\n\n'
        'func getSearchWithConfig(app *engine.Engine, typ searchType, routingConfig routing.Config) gin.HandlerFunc {',
        1,
    )

# Migrate checkouts integrated before route-table configuration existed.
source = source.replace(
    'return getSearchWithConfig(app, typ, routing.DefaultConfig())',
    'return getSearchWithConfig(app, typ, routing.ActiveConfig())',
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
\t\t\t\tproviders, routeErr := resolveMovieRoute(app, routingConfig, routing.Category(query.Category))
\t\t\t\tif routeErr != nil {
\t\t\t\t\terr = routeErr
\t\t\t\t} else {
\t\t\t\t\tresults, err = searchMovieProvidersAggregated(app, query.Q, providers, query.Fallback)
\t\t\t\t}
\t\t\t} else if inferredCategory, ok := routing.InferCategory(query.Q); searchAll && ok {
\t\t\t\tproviders, routeErr := resolveMovieRoute(app, routingConfig, inferredCategory)
\t\t\t\tif routeErr != nil {
\t\t\t\t\t// Automatic classification is best-effort; preserve broad-search compatibility if no route provider is installed.
\t\t\t\t\tresults, err = app.SearchMovieAll(query.Q, query.Fallback)
\t\t\t\t} else {
\t\t\t\t\tresults, err = searchMovieProvidersAggregated(app, query.Q, providers, query.Fallback)
\t\t\t\t}
\t\t\t} else if searchAll {
\t\t\t\tresults, err = app.SearchMovieAll(query.Q, query.Fallback)
\t\t\t} else {
\t\t\t\tresults, err = app.SearchMovie(query.Q, query.Provider, query.Fallback)
\t\t\t}
''' + source[end:]

if 'func resolveMovieRoute(' not in source:
    helper = '''func resolveMovieRoute(app *engine.Engine, config routing.Config, category routing.Category) ([]string, error) {
\tregistered := make([]string, 0, len(app.GetMovieProviders()))
\tfor name := range app.GetMovieProviders() {
\t\tregistered = append(registered, name)
\t}
\treturn config.Resolve(category, "", registered)
}

'''
    anchor = 'func getSearchWithConfig('
    index = source.find(anchor)
    if index < 0:
        raise SystemExit("getSearchWithConfig declaration not found")
    source = source[:index] + helper + source[index:]

if 'func searchMovieProvidersAggregated(' not in source:
    helper = '''// searchMovieProvidersAggregated keeps the per-provider results and prepends
// one merged candidate under the reserved provider name. Jellyfin then renders
// both the merged result and every original provider result as choices.
func searchMovieProvidersAggregated(app *engine.Engine, keyword string, providers []string, fallback bool) ([]*model.MovieSearchResult, error) {
\tresults, err := app.SearchMovieProviders(keyword, providers, fallback)
\tif err != nil {
\t\treturn nil, err
\t}
\treturn prependAggregatedCandidate(results), nil
}

func prependAggregatedCandidate(results []*model.MovieSearchResult) []*model.MovieSearchResult {
\tif len(results) == 0 || results[0] == nil || strings.TrimSpace(results[0].Number) == "" {
\t\treturn results
\t}
\tmerged := *results[0]
\tmerged.Provider = engine.AggregatedProviderName
\tmerged.ID = merged.Number
\tout := make([]*model.MovieSearchResult, 0, len(results)+1)
\tout = append(out, &merged)
\treturn append(out, results...)
}

'''
    anchor = 'func getSearchWithConfig('
    index = source.find(anchor)
    if index < 0:
        raise SystemExit("getSearchWithConfig declaration not found")
    source = source[:index] + helper + source[index:]

source = source.replace(
    'abortWithError(c, errors.FromCode(http.StatusNotFound))',
    'abortWithError(c, sdkerrors.FromCode(http.StatusNotFound))',
)
needle = '''\t\tif err != nil {
\t\t\tabortWithError(c, err)
\t\t\treturn
\t\t}'''
replacement = '''\t\tif err != nil {
\t\t\tif stderrors.Is(err, routing.ErrUnknownCategory) || stderrors.Is(err, routing.ErrNoAvailableProvider) {
\t\t\t\tabortWithStatusMessage(c, http.StatusBadRequest, err)
\t\t\t\treturn
\t\t\t}
\t\t\tabortWithError(c, err)
\t\t\treturn
\t\t}'''
if 'stderrors.Is(err, routing.ErrUnknownCategory)' not in source:
    index = source.rfind(needle)
    if index < 0:
        raise SystemExit("could not locate search error handler")
    source = source[:index] + replacement + source[index + len(needle):]

# Migrate checkouts that only mapped unknown categories to 400.
source = source.replace(
    'if stderrors.Is(err, routing.ErrUnknownCategory) {',
    'if stderrors.Is(err, routing.ErrUnknownCategory) || stderrors.Is(err, routing.ErrNoAvailableProvider) {',
)

required = [
    'stderrors "errors"',
    '"github.com/metatube-community/metatube-sdk-go/routing"',
    'Category string `form:"category"`',
    'func getSearchWithConfig(app *engine.Engine, typ searchType, routingConfig routing.Config) gin.HandlerFunc {',
    'func resolveMovieRoute(app *engine.Engine, config routing.Config, category routing.Category) ([]string, error) {',
    'func searchMovieProvidersAggregated(app *engine.Engine, keyword string, providers []string, fallback bool) ([]*model.MovieSearchResult, error) {',
    'engine.AggregatedProviderName',
    'searchMovieProvidersAggregated(app, query.Q, providers, query.Fallback)',
    'routing.InferCategory(query.Q)',
    'stderrors.Is(err, routing.ErrUnknownCategory)',
    'stderrors.Is(err, routing.ErrNoAvailableProvider)',
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

if 'stderrors "errors"' not in source:
    source = source.replace(
        'import (\n\t"net/http"\n',
        'import (\n\tstderrors "errors"\n\t"net/http"\n',
        1,
    )
if '"github.com/metatube-community/metatube-sdk-go/routing"' not in source:
    source = source.replace(
        '\t"github.com/metatube-community/metatube-sdk-go/engine/providerid"\n',
        '\t"github.com/metatube-community/metatube-sdk-go/engine/providerid"\n'
        '\t"github.com/metatube-community/metatube-sdk-go/routing"\n',
        1,
    )

if 'Category  string `form:"category"`' not in source:
    source = source.replace(
        'type infoQuery struct {\n\tLazy bool `form:"lazy"`\n}',
        'type infoQuery struct {\n\tLazy      bool   `form:"lazy"`\n\tAggregate bool   `form:"aggregate"`\n\tCategory  string `form:"category"`\n}',
        1,
    )

if 'func getInfoWithConfig(' not in source:
    source = source.replace(
        'func getInfo(app *engine.Engine, typ infoType) gin.HandlerFunc {\n\treturn func(c *gin.Context) {',
        'func getInfo(app *engine.Engine, typ infoType) gin.HandlerFunc {\n'
        '\treturn getInfoWithConfig(app, typ, routing.ActiveConfig())\n'
        '}\n\n'
        'func getInfoWithConfig(app *engine.Engine, typ infoType, routingConfig routing.Config) gin.HandlerFunc {\n'
        '\treturn func(c *gin.Context) {',
        1,
    )

# Migrate checkouts integrated before route-table configuration existed.
source = source.replace(
    'return getInfoWithConfig(app, typ, routing.DefaultConfig())',
    'return getInfoWithConfig(app, typ, routing.ActiveConfig())',
)

if 'Aggregate: true' in source:
    source = source.replace(
        'Aggregate: true, // aggregate movie metadata by default; callers may opt out.',
        'Aggregate: false, // stock detail by default; merged data is opt-in or requested via MetaX.',
        1,
    )
elif 'Aggregate: false' not in source and 'Aggregate bool' in source:
    source = source.replace(
        '\t\tquery := &infoQuery{\n\t\t\tLazy: true, // enable lazy by default.\n\t\t}',
        '\t\tquery := &infoQuery{\n'
        '\t\t\tLazy: true, // enable lazy by default.\n'
        '\t\t\tAggregate: false, // stock detail by default; merged data is opt-in or requested via MetaX.\n'
        '\t\t}',
        1,
    )

if 'engine.IsAggregatedProvider(uri.Provider)' not in source:
    source = source.replace(
        '\t\tcase movieInfoType:\n\t\t\tinfo, err = app.GetMovieInfoByProviderID(uri.AsProviderID(), query.Lazy)',
        '\t\tcase movieInfoType:\n'
        '\t\t\tif engine.IsAggregatedProvider(uri.Provider) {\n'
        '\t\t\t\taggregated, aggErr := app.GetAggregatedMovieInfoByNumber(uri.ID, routing.Category(query.Category), query.Lazy, routingConfig)\n'
        '\t\t\t\tif aggErr == nil {\n'
        '\t\t\t\t\t// Keep the merged identity stable so refreshes continue to\n'
        '\t\t\t\t\t// resolve through the reserved provider name.\n'
        '\t\t\t\t\taggregated.Provider = engine.AggregatedProviderName\n'
        '\t\t\t\t\taggregated.ID = uri.ID\n'
        '\t\t\t\t\tinfo = aggregated\n'
        '\t\t\t\t}\n'
        '\t\t\t\terr = aggErr\n'
        '\t\t\t} else if query.Aggregate {\n'
        '\t\t\t\tinfo, err = app.GetAggregatedMovieInfoByProviderIDWithCategory(uri.AsProviderID(), query.Lazy, routingConfig, routing.Category(query.Category))\n'
        '\t\t\t} else {\n'
        '\t\t\t\tinfo, err = app.GetMovieInfoByProviderID(uri.AsProviderID(), query.Lazy)\n'
        '\t\t\t}',
        1,
    )

if 'routing.ErrNoAvailableProvider' not in source:
    needle = '''\t\tif err != nil {
\t\t\tabortWithError(c, err)
\t\t\treturn
\t\t}'''
    replacement = '''\t\tif err != nil {
\t\t\tif stderrors.Is(err, routing.ErrUnknownCategory) || stderrors.Is(err, routing.ErrNoAvailableProvider) {
\t\t\t\tabortWithStatusMessage(c, http.StatusBadRequest, err)
\t\t\t\treturn
\t\t\t}
\t\t\tabortWithError(c, err)
\t\t\treturn
\t\t}'''
    index = source.rfind(needle)
    if index < 0:
        raise SystemExit("could not locate info error handler")
    source = source[:index] + replacement + source[index + len(needle):]

required = [
    'stderrors "errors"',
    '"github.com/metatube-community/metatube-sdk-go/routing"',
    'Aggregate bool',
    'Category  string `form:"category"`',
    'func getInfoWithConfig(app *engine.Engine, typ infoType, routingConfig routing.Config) gin.HandlerFunc {',
    'Aggregate: false',
    'engine.IsAggregatedProvider(uri.Provider)',
    'app.GetAggregatedMovieInfoByNumber(uri.ID, routing.Category(query.Category), query.Lazy, routingConfig)',
    'aggregated.Provider = engine.AggregatedProviderName',
    'app.GetAggregatedMovieInfoByProviderIDWithCategory(uri.AsProviderID(), query.Lazy, routingConfig, routing.Category(query.Category))',
    'routing.ErrNoAvailableProvider',
]
missing = [item for item in required if item not in source]
if missing:
    raise SystemExit("could not patch detail endpoint; missing expected markers: " + repr(missing))
path.write_text(source)
PY

python3 - "$sdk_root/route/image.go" <<'PY'
from pathlib import Path
import sys

path = Path(sys.argv[1])
source = path.read_text()

if '"github.com/metatube-community/metatube-sdk-go/routing"' not in source:
    source = source.replace(
        '\t"github.com/metatube-community/metatube-sdk-go/engine"\n',
        '\t"github.com/metatube-community/metatube-sdk-go/engine"\n'
        '\t"github.com/metatube-community/metatube-sdk-go/routing"\n',
        1,
    )
if 'stderrors "errors"' not in source:
    source = source.replace(
        'import (\n\t"bytes"\n',
        'import (\n\t"bytes"\n\tstderrors "errors"\n',
        1,
    )

if 'Category string  `form:"category"`' not in source:
    source = source.replace(
        '\tQuality  int     `form:"quality"`\n}',
        '\tQuality  int     `form:"quality"`\n\tCategory string  `form:"category"`\n}',
        1,
    )

if 'engine.IsAggregatedProvider(uri.Provider)' not in source:
    marker = '''\t\t// TODO: how to handle providers that implement
\t\t//   both actor and movie provider interfaces?'''
    replacement = '''\t\t// The merged search candidate carries the reserved provider name.
\t\t// Resolve it to the route-first real provider before the stock image paths.
\t\tif engine.IsAggregatedProvider(uri.Provider) {
\t\t\tresolved, resolveErr := app.ResolveAggregatedProviderID(uri.ID, routing.Category(query.Category), routing.ActiveConfig())
\t\t\tif resolveErr != nil {
\t\t\t\tif stderrors.Is(resolveErr, routing.ErrUnknownCategory) || stderrors.Is(resolveErr, routing.ErrNoAvailableProvider) {
\t\t\t\t\tabortWithStatusMessage(c, http.StatusBadRequest, resolveErr)
\t\t\t\t\treturn
\t\t\t\t}
\t\t\t\tabortWithError(c, resolveErr)
\t\t\t\treturn
\t\t\t}
\t\t\turi.Provider = resolved.Provider
\t\t\turi.ID = resolved.ID
\t\t}

''' + marker
    if marker not in source:
        raise SystemExit("image provider dispatch marker not found; SDK route/image.go changed")
    source = source.replace(marker, replacement, 1)

# Migrate checkouts that returned 500 for invalid MetaX image categories.
if 'stderrors.Is(resolveErr' not in source:
    old = '''\t\t\tif resolveErr != nil {
\t\t\t\tabortWithError(c, resolveErr)
\t\t\t\treturn
\t\t\t}'''
    new = '''\t\t\tif resolveErr != nil {
\t\t\t\tif stderrors.Is(resolveErr, routing.ErrUnknownCategory) || stderrors.Is(resolveErr, routing.ErrNoAvailableProvider) {
\t\t\t\t\tabortWithStatusMessage(c, http.StatusBadRequest, resolveErr)
\t\t\t\t\treturn
\t\t\t\t}
\t\t\t\tabortWithError(c, resolveErr)
\t\t\t\treturn
\t\t\t}'''
    if old not in source:
        raise SystemExit("could not locate MetaX image resolve error handler")
    source = source.replace(old, new, 1)

# Migrate checkouts integrated before route-table configuration existed.
source = source.replace(
    'app.ResolveAggregatedProviderID(uri.ID, routing.Category(query.Category), routing.DefaultConfig())',
    'app.ResolveAggregatedProviderID(uri.ID, routing.Category(query.Category), routing.ActiveConfig())',
)

required = [
    '"github.com/metatube-community/metatube-sdk-go/routing"',
    'stderrors "errors"',
    'Category string  `form:"category"`',
    'engine.IsAggregatedProvider(uri.Provider)',
    'app.ResolveAggregatedProviderID(uri.ID, routing.Category(query.Category), routing.ActiveConfig())',
    'stderrors.Is(resolveErr, routing.ErrUnknownCategory)',
    'routing.ErrNoAvailableProvider',
]
missing = [item for item in required if item not in source]
if missing:
    raise SystemExit("could not patch image endpoint; missing expected markers: " + repr(missing))
path.write_text(source)
PY

python3 - "$sdk_root/cmd/cmd.go" <<'PY'
from pathlib import Path
import sys

path = Path(sys.argv[1])
source = path.read_text()

if '"github.com/metatube-community/metatube-sdk-go/routing"' not in source:
    source = source.replace(
        '\t"github.com/metatube-community/metatube-sdk-go/route/auth"\n',
        '\t"github.com/metatube-community/metatube-sdk-go/route/auth"\n'
        '\t"github.com/metatube-community/metatube-sdk-go/routing"\n',
        1,
    )

marker = 'func Router(names ...string) *gin.Engine {\n'
if 'MetAX routing config' not in source:
    if marker not in source:
        raise SystemExit("cmd.Router declaration not found; SDK cmd/cmd.go changed")
    source = source.replace(
        marker,
        marker +
        '\t// MetAX routing config: load and validate the route table once at startup.\n'
        '\tif err := routing.InitActiveConfigFromEnv(); err != nil {\n'
        '\t\tlog.Fatal(err)\n'
        '\t}\n',
        1,
    )

required = [
    '"github.com/metatube-community/metatube-sdk-go/routing"',
    'routing.InitActiveConfigFromEnv()',
]
missing = [item for item in required if item not in source]
if missing:
    raise SystemExit("could not wire startup routing config; missing expected markers: " + repr(missing))
path.write_text(source)
PY

gofmt -w "$sdk_root/routing"/*.go "$sdk_root/engine/metax_search.go" "$sdk_root/engine/metax_aggregate.go" \
  "$sdk_root/engine/metax_aggregate_cache.go" "$sdk_root/engine/metax_aggregate_db.go" \
  "$sdk_root/engine/metax_aggregate_test.go" "$sdk_root/engine/metax_aggregate_fanout_test.go" \
  "$sdk_root/engine/metax_aggregate_cache_test.go" "$sdk_root/engine/metax_aggregate_db_test.go" \
  "$sdk_root/route/info.go" "$sdk_root/route/image.go" "$sdk_root/route/search.go" "$sdk_root/route/metax_category_api_test.go" \
  "$sdk_root/route/metax_test_helpers_test.go" "$sdk_root/route/metax_category_fanout_test.go" "$sdk_root/route/metax_aggregate_endpoint_test.go" \
  "$sdk_root/cmd/cmd.go"
echo "Integrated Amane category routes, FC2/uncensored merging, field-level movie aggregation, and source records."
