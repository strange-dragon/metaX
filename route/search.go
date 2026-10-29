package route

import (
	stderrors "errors"
	"net/http"
	pkgurl "net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/datatypes"

	"github.com/strange-dragon/metaX/engine"
	sdkerrors "github.com/strange-dragon/metaX/errors"
	"github.com/strange-dragon/metaX/model"
	"github.com/strange-dragon/metaX/routing"
)

type searchType uint8

const (
	actorSearchType searchType = iota
	movieSearchType
)

type searchQuery struct {
	Q        string `form:"q" binding:"required"`
	Provider string `form:"provider"`
	Category string `form:"category"`
	Fallback bool   `form:"fallback"`
}

func getSearch(app *engine.Engine, typ searchType) gin.HandlerFunc {
	return getSearchWithConfig(app, typ, routing.ActiveConfig())
}

func resolveMovieRoute(app *engine.Engine, config routing.Config, category routing.Category) ([]string, error) {
	registered := make([]string, 0, len(app.GetMovieProviders()))
	for name := range app.GetMovieProviders() {
		registered = append(registered, name)
	}
	return config.Resolve(category, "", registered)
}

// searchMovieProvidersAggregated keeps the per-provider results and prepends
// one merged candidate under the reserved provider name. Jellyfin then renders
// both the merged result and every original provider result as choices.
func searchMovieProvidersAggregated(app *engine.Engine, keyword string, providers []string, fallback bool) ([]*model.MovieSearchResult, error) {
	results, err := app.SearchMovieProviders(keyword, providers, fallback)
	if err != nil {
		return nil, err
	}
	return prependAggregatedCandidate(results), nil
}

func prependAggregatedCandidate(results []*model.MovieSearchResult) []*model.MovieSearchResult {
	return prependAggregatedCandidateWithPreference(results, engine.ActiveMergePreference())
}

// prependAggregatedCandidateWithPreference keeps the merged identity on the
// first identified result so "open this record" and its homepage stay stable,
// but fills title/actors/score from the preferred metadata provider and
// artwork from the preferred image provider. Both preferences fall back to
// result order, which is route order.
func prependAggregatedCandidateWithPreference(results []*model.MovieSearchResult, pref engine.MergePreference) []*model.MovieSearchResult {
	var base *model.MovieSearchResult
	for _, result := range results {
		if result != nil && strings.TrimSpace(result.Number) != "" {
			base = result
			break
		}
	}
	if base == nil {
		return results
	}
	merged := *base
	merged.Provider = engine.AggregatedProviderName
	merged.ID = merged.Number
	merged.Title = ""
	merged.ThumbURL = ""
	merged.CoverURL = ""
	merged.Actors = nil
	merged.Score = 0
	merged.ReleaseDate = datatypes.Date{}

	for _, result := range engine.OrderByProviderPreference(results, pref.Detail, searchResultProviderName) {
		if result == nil {
			continue
		}
		if strings.TrimSpace(merged.Title) == "" {
			merged.Title = result.Title
		}
		if strings.TrimSpace(merged.Homepage) == "" {
			merged.Homepage = result.Homepage
		}
		if merged.Score == 0 {
			merged.Score = result.Score
		}
		if time.Time(merged.ReleaseDate).IsZero() {
			merged.ReleaseDate = result.ReleaseDate
		}
		for _, actor := range result.Actors {
			actor = strings.TrimSpace(actor)
			if actor == "" || containsFold(merged.Actors, actor) {
				continue
			}
			merged.Actors = append(merged.Actors, actor)
		}
	}
	for _, result := range engine.OrderByProviderPreference(results, pref.Image, searchResultProviderName) {
		if result == nil {
			continue
		}
		if strings.TrimSpace(merged.ThumbURL) == "" {
			merged.ThumbURL = result.ThumbURL
		}
		if strings.TrimSpace(merged.CoverURL) == "" {
			merged.CoverURL = result.CoverURL
		}
	}
	out := make([]*model.MovieSearchResult, 0, len(results)+1)
	out = append(out, &merged)
	return append(out, results...)
}

func searchResultProviderName(result *model.MovieSearchResult) string {
	if result == nil {
		return ""
	}
	return result.Provider
}

func containsFold(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(value, target) {
			return true
		}
	}
	return false
}

func getSearchWithConfig(app *engine.Engine, typ searchType, routingConfig routing.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		query := &searchQuery{
			Fallback: true, // enable fallback by default.
		}
		if err := c.ShouldBindQuery(query); err != nil {
			abortWithStatusMessage(c, http.StatusBadRequest, err)
			return
		}

		isValidURL := true
		if _, err := pkgurl.ParseRequestURI(query.Q); err != nil {
			isValidURL = false
		}

		requestApp, requestErr := requestEngine(c, app)
		if requestErr != nil {
			abortWithStatusMessage(c, http.StatusBadRequest, requestErr)
			return
		}
		app = requestApp

		// if provider is not specified, search with all providers.
		searchAll := query.Provider == ""

		var (
			results any
			err     error
		)
		switch typ {
		case actorSearchType:
			if isValidURL {
				results, err = app.GetActorInfoByURL(query.Q, true /* always lazy */)
			} else if searchAll {
				results, err = app.SearchActorAll(query.Q, query.Fallback)
			} else {
				results, err = app.SearchActor(query.Q, query.Provider, query.Fallback)
			}
		case movieSearchType:
			if isValidURL {
				results, err = app.GetMovieInfoByURL(query.Q, true /* always lazy */)
			} else if query.Provider != "" {
				// An explicit provider remains a manual override, even if a category is supplied.
				results, err = app.SearchMovie(query.Q, query.Provider, query.Fallback)
			} else if query.Category != "" {
				providers, routeErr := resolveMovieRoute(app, routingConfig, routing.Category(query.Category))
				if routeErr != nil {
					err = routeErr
				} else {
					results, err = searchMovieProvidersAggregated(app, query.Q, providers, query.Fallback)
				}
			} else if inferredCategory, ok := routing.InferCategory(query.Q); searchAll && ok {
				providers, routeErr := resolveMovieRoute(app, routingConfig, inferredCategory)
				if routeErr != nil {
					// Automatic classification is best-effort; preserve broad-search compatibility if no route provider is installed.
					results, err = app.SearchMovieAll(query.Q, query.Fallback)
				} else {
					results, err = searchMovieProvidersAggregated(app, query.Q, providers, query.Fallback)
				}
			} else if searchAll {
				results, err = app.SearchMovieAll(query.Q, query.Fallback)
			} else {
				results, err = app.SearchMovie(query.Q, query.Provider, query.Fallback)
			}
		default:
			panic("invalid search type")
		}
		if err != nil {
			if stderrors.Is(err, routing.ErrUnknownCategory) || stderrors.Is(err, routing.ErrNoAvailableProvider) {
				abortWithStatusMessage(c, http.StatusBadRequest, err)
				return
			}
			abortWithError(c, err)
			return
		}

		// length is at least 1.
		resultsLength := 1

		// convert to search results.
		switch v := results.(type) {
		case *model.ActorInfo:
			results = []*model.ActorSearchResult{v.ToSearchResult()}
		case *model.MovieInfo:
			results = []*model.MovieSearchResult{v.ToSearchResult()}
		case []*model.ActorSearchResult:
			resultsLength = len(v)
		case []*model.MovieSearchResult:
			resultsLength = len(v)
		default:
			panic("unexpected search results type")
		}
		if resultsLength == 0 {
			abortWithError(c, sdkerrors.FromCode(http.StatusNotFound))
			return
		}

		c.JSON(http.StatusOK, &responseMessage{Data: results})
	}
}
