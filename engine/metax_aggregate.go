package engine

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"

	"gorm.io/datatypes"

	"github.com/lib/pq"
	"github.com/strange-dragon/metaX/engine/providerid"
	"github.com/strange-dragon/metaX/model"
	mt "github.com/strange-dragon/metaX/provider"
	"github.com/strange-dragon/metaX/routing"
)

// AggregatedProviderName is the reserved provider name used for the merged
// search candidate and for detail requests that should be resolved from a
// catalog number instead of a single provider ID.
const AggregatedProviderName = "MetaX"

// IsAggregatedProvider reports whether name refers to the reserved merged
// provider rather than a registered provider package.
func IsAggregatedProvider(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), AggregatedProviderName)
}

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
// identify the same title (see routing.SameNumber).
func (e *Engine) GetAggregatedMovieInfoByProviderID(pid providerid.ProviderID, lazy bool) (*AggregatedMovieInfo, error) {
	return e.GetAggregatedMovieInfoByProviderIDWithConfig(pid, lazy, routing.ActiveConfig())
}

// GetAggregatedMovieInfoByProviderIDWithConfig is the explicit-route variant of
// GetAggregatedMovieInfoByProviderID. It lets embedders and tests supply their
// own route table without changing global state.
func (e *Engine) GetAggregatedMovieInfoByProviderIDWithConfig(pid providerid.ProviderID, lazy bool, config routing.Config) (*AggregatedMovieInfo, error) {
	return e.getCachedAggregatedMovieInfo(pid, lazy, config, "")
}

// GetAggregatedMovieInfoByProviderIDWithCategory is the explicit-category
// variant. When category is non-empty it takes precedence over number/provider
// inference, which is what the Jellyfin plugin sends for every scrape.
func (e *Engine) GetAggregatedMovieInfoByProviderIDWithCategory(pid providerid.ProviderID, lazy bool, config routing.Config, category routing.Category) (*AggregatedMovieInfo, error) {
	return e.getCachedAggregatedMovieInfo(pid, lazy, config, category)
}

func (e *Engine) getCachedAggregatedMovieInfo(pid providerid.ProviderID, lazy bool, config routing.Config, category routing.Category) (*AggregatedMovieInfo, error) {
	key := aggregateMovieCacheKey(pid, lazy, category, config)
	result, err := aggregateMovieCache.do(key, func() (*AggregatedMovieInfo, error) {
		return e.getAggregatedMovieInfo(pid, lazy, config, category)
	})
	return cloneAggregatedMovieInfo(result), err
}

func (e *Engine) getAggregatedMovieInfo(pid providerid.ProviderID, lazy bool, config routing.Config, requested routing.Category) (*AggregatedMovieInfo, error) {
	baseline, err := e.GetMovieInfoByProviderID(pid, lazy)
	if err != nil {
		return nil, err
	}

	var (
		category routing.Category
		ok       bool
	)
	if requested != "" {
		canonical, parseErr := routing.ParseCategory(string(requested))
		if parseErr != nil {
			return nil, parseErr
		}
		category, ok = canonical, true
	} else {
		// These providers represent category-specific catalogues whose
		// identifiers may be slugs or numeric product IDs rather than standard
		// catalog numbers.
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
	}
	if !ok {
		return &AggregatedMovieInfo{
			MovieInfo: baseline,
			Category:  "unknown",
			Sources: []MovieSourceRecord{{
				Provider: baseline.Provider, ID: baseline.ID,
				Status:   "baseline_only",
				Error:    "category could not be inferred safely; cross-provider aggregation was skipped",
				Metadata: baseline,
			}},
		}, nil
	}

	registered := make([]string, 0, len(e.GetMovieProviders()))
	for name := range e.GetMovieProviders() {
		registered = append(registered, name)
	}
	providers, routeErr := config.Resolve(category, "", registered)
	if routeErr != nil {
		// An explicit category is a routing instruction: surfacing the error
		// gives callers a 400 instead of a misleading baseline-only success.
		// Inferred categories remain best-effort and preserve the baseline.
		if requested != "" {
			return nil, routeErr
		}
		return &AggregatedMovieInfo{
			MovieInfo: baseline,
			Category:  string(category.Canonical()),
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

			release := aggregateFanoutLimiter.acquire()
			defer release()
			candidates, searchErr := e.SearchMovie(baseline.Number, name, false)
			if searchErr != nil {
				records[i] = MovieSourceRecord{Provider: name, Status: "error", Error: searchErr.Error()}
				return
			}
			var candidate *model.MovieSearchResult
			for _, result := range candidates {
				if result != nil && strings.EqualFold(result.Provider, name) &&
					routing.SameNumber(result.Number, baseline.Number) {
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
			if !routing.SameNumber(info.Number, baseline.Number) {
				records[i] = MovieSourceRecord{
					Provider: name, ID: info.ID, Status: "identity_mismatch",
					Error:    fmt.Sprintf("returned catalog number %q does not match selected number %q", info.Number, baseline.Number),
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
		Category:  string(category.Canonical()),
		Sources:   records,
	}, nil
}

// GetAggregatedMovieInfoByNumber resolves the reserved aggregated provider
// identity (AggregatedProviderName + catalog number). It searches the category
// route in order, uses the first provider whose detail succeeds as the public
// identity, and returns the merged view plus every provider's original record.
func (e *Engine) GetAggregatedMovieInfoByNumber(number string, category routing.Category, lazy bool, config routing.Config) (*AggregatedMovieInfo, error) {
	key := aggregateNumberCacheKey(number, lazy, category, config)
	result, err := aggregateMovieCache.do(key, func() (*AggregatedMovieInfo, error) {
		return e.getAggregatedMovieInfoByNumber(number, category, lazy, config)
	})
	return cloneAggregatedMovieInfo(result), err
}

func (e *Engine) getAggregatedMovieInfoByNumber(number string, category routing.Category, lazy bool, config routing.Config) (*AggregatedMovieInfo, error) {
	number = strings.TrimSpace(number)
	if number == "" {
		return nil, mt.ErrInvalidKeyword
	}
	candidateCategory, providers, err := e.aggregateRouteProviders(number, category, config)
	if err != nil {
		return nil, err
	}

	// lazy=true means the caller prefers cached metadata. Resolve the number
	// from the local catalog first and only search providers when the cache
	// does not contain a usable record.
	if lazy {
		if pid, ok := e.lookupProviderIDByNumber(number, providers); ok {
			if aggregated, aggErr := e.getAggregatedMovieInfo(pid, true, config, candidateCategory); aggErr == nil {
				return aggregated, nil
			}
		}
	}

	for _, name := range providers {
		results, err := e.SearchMovie(number, name, false)
		if err != nil {
			continue
		}
		for _, result := range results {
			if result == nil || !strings.EqualFold(result.Provider, name) ||
				!routing.SameNumber(result.Number, number) {
				continue
			}
			aggregated, aggErr := e.getAggregatedMovieInfo(
				providerid.ProviderID{Provider: result.Provider, ID: result.ID},
				lazy, config, candidateCategory,
			)
			if aggErr == nil {
				return aggregated, nil
			}
		}
	}
	return nil, mt.ErrInfoNotFound
}

// ResolveAggregatedProviderID finds the provider whose artwork should be served
// for a catalog number without fetching or merging details. Image endpoints
// only need a real provider identity, so this avoids a full aggregation
// fan-out. The configured image preference is applied before route order: the
// proxy route turns this identity into the primary/thumb/backdrop image, which
// is why the image source has to be decided here as well as in the merge.
func (e *Engine) ResolveAggregatedProviderID(number string, category routing.Category, config routing.Config) (providerid.ProviderID, error) {
	candidates, err := e.ResolveAggregatedProviderIDs(number, category, config, 1)
	if err != nil {
		return providerid.ProviderID{}, err
	}
	return candidates[0], nil
}

// ResolveAggregatedProviderIDs returns up to limit provider identities for a
// catalog number in artwork order: the configured image provider first, then
// route order. The image endpoint walks this chain so a title that the
// preferred provider does not carry, or whose artwork that provider cannot
// serve, still yields a real image from the next source.
func (e *Engine) ResolveAggregatedProviderIDs(number string, category routing.Category, config routing.Config, limit int) ([]providerid.ProviderID, error) {
	if limit <= 0 {
		limit = 1
	}
	key := aggregateResolveCacheKey(number, category, config, limit)
	return aggregateResolveCache.do(key, func() ([]providerid.ProviderID, error) {
		candidates, err := e.resolveAggregatedProviderIDs(number, category, config, limit)
		if err != nil {
			return nil, err
		}
		return append([]providerid.ProviderID(nil), candidates...), nil
	})
}

func (e *Engine) resolveAggregatedProviderIDs(number string, category routing.Category, config routing.Config, limit int) ([]providerid.ProviderID, error) {
	number = strings.TrimSpace(number)
	if number == "" {
		return nil, mt.ErrInvalidKeyword
	}
	_, providers, err := e.aggregateRouteProviders(number, category, config)
	if err != nil {
		return nil, err
	}
	providers = OrderByProviderPreference(providers, ActiveMergePreference().Image, func(name string) string {
		return name
	})

	// Image requests only need a real provider identity. Reuse the local
	// catalog when possible so primary/thumb/backdrop fetches do not each
	// trigger a provider search. A stale record merely yields a failed image
	// until the next lazy detail refresh updates it.
	candidates := make([]providerid.ProviderID, 0, limit)
	seen := make(map[string]struct{}, limit)
	add := func(pid providerid.ProviderID) bool {
		folded := strings.ToLower(strings.TrimSpace(pid.Provider))
		if _, exists := seen[folded]; exists {
			return len(candidates) >= limit
		}
		seen[folded] = struct{}{}
		candidates = append(candidates, pid)
		return len(candidates) >= limit
	}
	for _, name := range providers {
		if len(candidates) >= limit {
			return candidates, nil
		}
		if pid, ok := e.lookupProviderIDByNumber(number, []string{name}); ok {
			if add(pid) {
				return candidates, nil
			}
		}
	}

	for _, name := range providers {
		if len(candidates) >= limit {
			break
		}
		if _, done := seen[strings.ToLower(strings.TrimSpace(name))]; done {
			continue
		}
		results, err := e.SearchMovie(number, name, false)
		if err != nil {
			continue
		}
		for _, result := range results {
			if result != nil && strings.EqualFold(result.Provider, name) &&
				routing.SameNumber(result.Number, number) {
				add(providerid.ProviderID{Provider: result.Provider, ID: result.ID})
				break
			}
		}
	}
	if len(candidates) == 0 {
		return nil, mt.ErrInfoNotFound
	}
	return candidates, nil
}

// aggregateRouteProviders resolves the provider chain for a category, with a
// broad-search fallback when the number cannot be classified safely. An explicit
// category that cannot be resolved is returned as an error instead of silently
// broadening the search, so misconfiguration is visible to the caller.
func (e *Engine) aggregateRouteProviders(number string, category routing.Category, config routing.Config) (routing.Category, []string, error) {
	candidateCategory := category
	if candidateCategory == "" {
		if inferred, ok := routing.InferCategory(number); ok {
			candidateCategory = inferred
		}
	} else {
		canonical, err := routing.ParseCategory(string(candidateCategory))
		if err != nil {
			return "", nil, err
		}
		candidateCategory = canonical
	}

	registered := make([]string, 0, len(e.GetMovieProviders()))
	for name := range e.GetMovieProviders() {
		registered = append(registered, name)
	}
	if candidateCategory != "" {
		providers, err := config.Resolve(candidateCategory, "", registered)
		if err == nil {
			return candidateCategory, providers, nil
		}
		if category != "" {
			return candidateCategory, nil, err
		}
	}
	// Ambiguous number or no route provider registered: fall back to all
	// providers so the merged candidate still resolves.
	providers := append([]string(nil), registered...)
	sort.Strings(providers)
	return candidateCategory, providers, nil
}

// mergeMovieInfoInRouteOrder merges metadata and artwork with the configured
// field preferences applied on top of route order.
func mergeMovieInfoInRouteOrder(identity *model.MovieInfo, infos []*model.MovieInfo) *model.MovieInfo {
	return mergeMovieInfoWithPreference(identity, infos, ActiveMergePreference())
}

// mergeMovieInfoWithPreference uses the first non-empty value in route order for
// scalar fields and stable unions for list/image fields. Fields are filled in
// two independent passes: metadata follows the detail preference and artwork
// follows the image preference, each falling back to route order. Scores are
// intentionally not averaged or replaced across providers because their scales
// may differ.
func mergeMovieInfoWithPreference(identity *model.MovieInfo, infos []*model.MovieInfo, pref MergePreference) *model.MovieInfo {
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

	detailOrder := OrderByProviderPreference(infos, pref.Detail, movieInfoProviderName)
	imageOrder := OrderByProviderPreference(infos, pref.Image, movieInfoProviderName)

	for _, info := range detailOrder {
		if info == nil {
			continue
		}
		firstNonEmpty(&result.Title, info.Title)
		firstNonEmpty(&result.Director, info.Director)
		firstNonEmpty(&result.Summary, info.Summary)
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

	for _, info := range imageOrder {
		if info == nil {
			continue
		}
		firstNonEmpty(&result.ThumbURL, info.ThumbURL)
		firstNonEmpty(&result.BigThumbURL, info.BigThumbURL)
		firstNonEmpty(&result.CoverURL, info.CoverURL)
		firstNonEmpty(&result.BigCoverURL, info.BigCoverURL)
		if len(info.PreviewImages) > 0 {
			result.PreviewImages = pq.StringArray(appendUniqueStrings([]string(result.PreviewImages), []string(info.PreviewImages)...))
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

func movieInfoProviderName(info *model.MovieInfo) string {
	if info == nil {
		return ""
	}
	return info.Provider
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
