package engine

import (
	"fmt"
	"reflect"
	"strings"
	"sync"

	"gorm.io/datatypes"

	"github.com/metatube-community/metatube-sdk-go/engine/providerid"
	"github.com/lib/pq"
	"github.com/metatube-community/metatube-sdk-go/model"
	"github.com/metatube-community/metatube-sdk-go/routing"
)

// MovieSourceRecord preserves one provider's independent, normalized SDK
// metadata. It is not the upstream HTML/HTTP payload: MovieProvider currently
// exposes only model.MovieInfo.
type MovieSourceRecord struct {
	Provider string           `json:"provider"`
	ID       string           `json:"id,omitempty"`
	Status   string           `json:"status"`
	Error    string           `json:"error,omitempty"`
	Metadata *model.MovieInfo `json:"metadata,omitempty"`
}

// AggregatedMovieInfo keeps the stock MovieInfo JSON fields at the top level
// and adds source records without changing the existing detail response shape.
type AggregatedMovieInfo struct {
	*model.MovieInfo
	Category string              `json:"category"`
	Sources  []MovieSourceRecord `json:"sources"`
}

// GetAggregatedMovieInfoByProviderID fetches the selected provider's detail,
// resolves an Amane-derived route from its catalog number, and concurrently
// fetches matching details from eligible providers. Provider + ID remains the
// public identity; supplemental records are merged only when catalog numbers
// match exactly after conservative punctuation normalization.
func (e *Engine) GetAggregatedMovieInfoByProviderID(pid providerid.ProviderID, lazy bool) (*AggregatedMovieInfo, error) {
	baseline, err := e.GetMovieInfoByProviderID(pid, lazy)
	if err != nil {
		return nil, err
	}

	var (
		category routing.Category
		ok       bool
	)
	// These providers represent category-specific catalogues whose identifiers
	// may be slugs or numeric product IDs rather than standard catalog numbers.
	switch strings.ToLower(strings.TrimSpace(baseline.Provider)) {
	case "theporndb":
		category, ok = routing.CategoryWestern, true
	case "getchu":
		category, ok = routing.CategoryHentai, true
	case "fc2", "fc2ppvdb":
		category, ok = routing.CategoryFC2, true
	default:
		category, ok = routing.InferCategory(baseline.Number)
	}
	if !ok {
		return &AggregatedMovieInfo{
			MovieInfo: baseline,
			Category:  "unknown",
			Sources: []MovieSourceRecord{{
				Provider: baseline.Provider, ID: baseline.ID,
				Status: "baseline_only",
				Error:  "category could not be inferred safely; cross-provider aggregation was skipped",
				Metadata: baseline,
			}},
		}, nil
	}

	registered := make([]string, 0, len(e.GetMovieProviders()))
	for name := range e.GetMovieProviders() {
		registered = append(registered, name)
	}
	providers, routeErr := routing.DefaultConfig().Resolve(category, "", registered)
	if routeErr != nil {
		return &AggregatedMovieInfo{
			MovieInfo: baseline,
			Category:  string(category),
			Sources: []MovieSourceRecord{{
				Provider: baseline.Provider, ID: baseline.ID,
				Status: "baseline_only", Error: routeErr.Error(), Metadata: baseline,
			}},
		}, nil
	}

	// Keep the selected record even if its provider is not part of the inferred
	// route. When it is part of the route, its slot follows configured route order.
	baselineInRoute := false
	for _, name := range providers {
		if strings.EqualFold(name, baseline.Provider) {
			baselineInRoute = true
			break
		}
	}
	if !baselineInRoute {
		providers = append(providers, baseline.Provider)
	}

	records := make([]MovieSourceRecord, len(providers))
	infos := make([]*model.MovieInfo, len(providers))
	var wg sync.WaitGroup
	for i, name := range providers {
		i, name := i, name
		wg.Add(1)
		go func() {
			defer wg.Done()
			if strings.EqualFold(name, baseline.Provider) {
				records[i] = MovieSourceRecord{
					Provider: name, ID: baseline.ID, Status: "success", Metadata: baseline,
				}
				infos[i] = baseline
				return
			}

			candidates, searchErr := e.SearchMovie(baseline.Number, name, false)
			if searchErr != nil {
				records[i] = MovieSourceRecord{Provider: name, Status: "error", Error: searchErr.Error()}
				return
			}
			var candidate *model.MovieSearchResult
			for _, result := range candidates {
				if result != nil && strings.EqualFold(result.Provider, name) &&
					normalizeAggregationNumber(result.Number) == normalizeAggregationNumber(baseline.Number) {
					candidate = result
					break
				}
			}
			if candidate == nil {
				records[i] = MovieSourceRecord{Provider: name, Status: "not_found"}
				return
			}

			info, detailErr := e.GetMovieInfoByProviderID(providerid.ProviderID{
				Provider: candidate.Provider, ID: candidate.ID,
			}, lazy)
			if detailErr != nil {
				records[i] = MovieSourceRecord{
					Provider: name, ID: candidate.ID, Status: "error", Error: detailErr.Error(),
				}
				return
			}
			if normalizeAggregationNumber(info.Number) != normalizeAggregationNumber(baseline.Number) {
				records[i] = MovieSourceRecord{
					Provider: name, ID: info.ID, Status: "identity_mismatch",
					Error: fmt.Sprintf("returned catalog number %q does not match selected number %q", info.Number, baseline.Number),
					Metadata: info,
				}
				return
			}
			records[i] = MovieSourceRecord{
				Provider: name, ID: info.ID, Status: "success", Metadata: info,
			}
			infos[i] = info
		}()
	}
	wg.Wait()

	merged := mergeMovieInfoInRouteOrder(baseline, infos)
	return &AggregatedMovieInfo{
		MovieInfo: merged,
		Category:  string(category),
		Sources:   records,
	}, nil
}

func normalizeAggregationNumber(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	return strings.NewReplacer(" ", "", "-", "").Replace(value)
}

// mergeMovieInfoInRouteOrder uses the first non-empty value in route order for
// scalar fields and stable unions for list/image fields. Scores are intentionally
// not averaged or replaced across providers because their scales may differ.
func mergeMovieInfoInRouteOrder(identity *model.MovieInfo, infos []*model.MovieInfo) *model.MovieInfo {
	result := *identity
	result.Actors = nil
	result.PreviewImages = nil
	result.Genres = nil
	result.Title = ""
	result.Director = ""
	result.Summary = ""
	result.ThumbURL = ""
	result.BigThumbURL = ""
	result.CoverURL = ""
	result.BigCoverURL = ""
	result.PreviewVideoURL = ""
	result.PreviewVideoHLSURL = ""
	result.Maker = ""
	result.Label = ""
	result.Series = ""
	result.Runtime = 0
	result.ReleaseDate = datatypes.Date{}
	result.Score = identity.Score

	for _, info := range infos {
		if info == nil {
			continue
		}
		firstNonEmpty(&result.Title, info.Title)
		firstNonEmpty(&result.Director, info.Director)
		firstNonEmpty(&result.Summary, info.Summary)
		firstNonEmpty(&result.ThumbURL, info.ThumbURL)
		firstNonEmpty(&result.BigThumbURL, info.BigThumbURL)
		firstNonEmpty(&result.CoverURL, info.CoverURL)
		firstNonEmpty(&result.BigCoverURL, info.BigCoverURL)
		firstNonEmpty(&result.PreviewVideoURL, info.PreviewVideoURL)
		firstNonEmpty(&result.PreviewVideoHLSURL, info.PreviewVideoHLSURL)
		firstNonEmpty(&result.Maker, info.Maker)
		firstNonEmpty(&result.Label, info.Label)
		firstNonEmpty(&result.Series, info.Series)
		if len(info.Actors) > 0 {
			result.Actors = pq.StringArray(appendUniqueStrings([]string(result.Actors), []string(info.Actors)...))
		}
		if len(info.Genres) > 0 {
			result.Genres = pq.StringArray(appendUniqueStrings([]string(result.Genres), []string(info.Genres)...))
		}
		if len(info.PreviewImages) > 0 {
			result.PreviewImages = pq.StringArray(appendUniqueStrings([]string(result.PreviewImages), []string(info.PreviewImages)...))
		}
		if result.Runtime == 0 && info.Runtime > 0 {
			result.Runtime = info.Runtime
		}
		if reflect.DeepEqual(result.ReleaseDate, datatypes.Date{}) && !reflect.DeepEqual(info.ReleaseDate, datatypes.Date{}) {
			result.ReleaseDate = info.ReleaseDate
		}
		if result.Score == 0 && info.Score != 0 {
			result.Score = info.Score
		}
	}
	// Preserve the selected provider + ID identity and canonical catalog number.
	// Display title remains field-aggregated in configured route order.
	result.ID = identity.ID
	result.Provider = identity.Provider
	result.Number = identity.Number
	if result.Title == "" {
		result.Title = identity.Title
	}
	result.Homepage = identity.Homepage
	if result.CoverURL == "" {
		result.CoverURL = identity.CoverURL
	}
	if result.ThumbURL == "" {
		result.ThumbURL = identity.ThumbURL
	}
	return &result
}

func firstNonEmpty(dst *string, value string) {
	if *dst == "" && strings.TrimSpace(value) != "" {
		*dst = value
	}
}

func appendUniqueStrings(dst []string, values ...string) []string {
	seen := make(map[string]struct{}, len(dst)+len(values))
	for _, value := range dst {
		seen[value] = struct{}{}
	}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		dst = append(dst, value)
	}
	return dst
}

