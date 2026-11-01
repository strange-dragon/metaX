package engine

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm/clause"

	"github.com/strange-dragon/metaX/collection/sets"
	"github.com/strange-dragon/metaX/collection/slices"
	"github.com/strange-dragon/metaX/common/comparer"
	"github.com/strange-dragon/metaX/common/number"
	"github.com/strange-dragon/metaX/engine/providerid"
	"github.com/strange-dragon/metaX/model"
	mt "github.com/strange-dragon/metaX/provider"
)

func (e *Engine) searchMovieFromDB(keyword string, provider mt.MovieProvider, all bool) (results []*model.MovieSearchResult, err error) {
	var infos []*model.MovieInfo
	tx := e.db.
		// Note: keyword might be an ID or just a regular number, so we should
		// query both of them for best match. Also, case should not matter.
		Where("number = ? COLLATE NOCASE", keyword).
		Or("id = ? COLLATE NOCASE", keyword)
	if all {
		err = tx.Find(&infos).Error
	} else {
		err = e.db.
			Where("provider = ?", provider.Name()).
			Where(tx).
			Find(&infos).Error
	}
	if err == nil {
		for _, info := range infos {
			if !info.IsValid() {
				// normally it is valid, but just in case.
				continue
			}
			results = append(results, info.ToSearchResult())
		}
	}
	return
}

func (e *Engine) searchMovie(keyword string, provider mt.MovieProvider, fallback bool) (results []*model.MovieSearchResult, err error) {
	// Regular keyword searching.
	if searcher, ok := provider.(mt.MovieSearcher); ok {
		if keyword = searcher.NormalizeMovieKeyword(keyword); keyword == "" {
			return nil, mt.ErrInvalidKeyword
		}
		if fallback {
			defer func() {
				if innerResults, innerErr := e.searchMovieFromDB(keyword, provider, false);
				// ignore DB query error.
				innerErr == nil && len(innerResults) > 0 {
					// overwrite error.
					err = nil
					// update results.
					msr := sets.NewOrderedSetWithHash(func(v *model.MovieSearchResult) string { return v.Provider + v.ID })
					msr.Add(results...)
					msr.Add(innerResults...)
					results = msr.AsSlice()
				}
			}()
		}
		return searcher.SearchMovie(keyword)
	}
	// Fallback to movie info querying.
	info, err := e.getMovieInfoByProviderID(provider, keyword, true)
	if err != nil {
		return nil, err
	}
	return []*model.MovieSearchResult{info.ToSearchResult()}, nil
}

func (e *Engine) SearchMovie(keyword, name string, fallback bool) ([]*model.MovieSearchResult, error) {
	if keyword = number.Trim(keyword); keyword == "" {
		return nil, mt.ErrInvalidKeyword
	}
	provider, err := e.GetMovieProviderByName(name)
	if err != nil {
		return nil, err
	}
	return e.searchMovie(keyword, provider, fallback)
}

func (e *Engine) searchMovieAll(keyword string) (results []*model.MovieSearchResult, err error) {
	type response struct {
		Results   []*model.MovieSearchResult
		Error     error
		Provider  mt.MovieProvider
		StartTime time.Time
		EndTime   time.Time
	}
	respCh := make(chan response)

	var wg sync.WaitGroup
	for _, provider := range e.movieProviders.Iterator() {
		wg.Add(1)
		// Goroutine started time.
		startTime := time.Now()
		// Async searching.
		go func(provider mt.MovieProvider) {
			defer wg.Done()
			innerResults, innerErr := e.searchMovie(keyword, provider, false)
			respCh <- response{
				Results:   innerResults,
				Error:     innerErr,
				Provider:  provider,
				StartTime: startTime,
				EndTime:   time.Now(),
			}
		}(provider)
	}
	go func() {
		wg.Wait()
		// notify when all searching tasks done.
		close(respCh)
	}()

	ds := make([]string, 0, e.movieProviders.Len())
	// response channel.
	for resp := range respCh {
		ds = append(ds, func(a, b, c any) string {
			if c == nil {
				c = "no error"
			}
			return fmt.Sprintf("%s(%s):<%v>", a, b, c)
		}(
			resp.Provider.Name(),
			resp.EndTime.Sub(resp.StartTime),
			resp.Error,
		))

		if resp.Error != nil {
			continue
		}
		results = append(results, resp.Results...)
	}

	e.logger.Printf("Search keyword %s: %s", keyword, strings.Join(ds, " | "))
	return
}

// SearchMovieAll searches the keyword from all providers.
func (e *Engine) SearchMovieAll(keyword string, fallback bool) (results []*model.MovieSearchResult, err error) {
	if keyword = number.Trim(keyword); keyword == "" {
		return nil, mt.ErrInvalidKeyword
	}

	defer func() {
		if err != nil {
			return
		}
		if len(results) == 0 {
			err = mt.ErrInfoNotFound
			return
		}
		// remove duplicate results, if any.
		msr := sets.NewOrderedSetWithHash(func(v *model.MovieSearchResult) string { return v.Provider + v.ID })
		msr.Add(results...)
		results = msr.AsSlice()
		// post-processing
		ps := new(slices.WeightedSlice[*model.MovieSearchResult, float64])
		for _, result := range results {
			if !result.IsValid() /* validation check */ {
				continue
			}
			if _, err := e.GetMovieProviderByName(result.Provider); err != nil {
				e.logger.Printf("ignore provider %s as not found", result.Provider)
				continue
			}
			priority := comparer.Compare(keyword, result.Number) *
				e.MustGetMovieProviderByName(result.Provider).Priority()
			ps.Append(result, priority)
		}
		// sort by priority.
		results = ps.SortFunc(sort.Stable).Slice()
	}()

	if fallback /* query database for missing results  */ {
		defer func() {
			if innerResults, innerErr := e.searchMovieFromDB(keyword, nil, true);
			// ignore DB query error.
			innerErr == nil && len(innerResults) > 0 {
				// overwrite error.
				err = nil
				// append results.
				results = append(results, innerResults...)
			}
		}()
	}

	results, err = e.searchMovieAll(keyword)
	return
}

func (e *Engine) getMovieInfoFromDB(provider mt.MovieProvider, id string) (*model.MovieInfo, error) {
	info := &model.MovieInfo{}
	err := e.db. // Exact match here.
			Where("provider = ?", provider.Name()).
			Where("id = ? COLLATE NOCASE", id).
			First(info).Error
	return info, err
}

func (e *Engine) getMovieInfoWithCallback(provider mt.MovieProvider, id string, lazy bool, callback func() (*model.MovieInfo, error)) (info *model.MovieInfo, err error) {
	defer func() {
		// Every provider reports "the source has no such record" in one of two
		// shapes that the HTTP layer could not classify, so both surfaced as a
		// 500 — "the server is broken" — and a client library retried the same
		// missing file on every scan:
		//
		//   - colly turns any non-2xx response into errors.New(StatusText(code))
		//     with no type to assert on, and providers return it verbatim;
		//   - a removed record answers 200 with a page that parses into a
		//     structurally valid but title-less MovieInfo.
		//
		// A 404 for both is the truthful answer. A record that has a title but
		// is missing other fields stays ErrIncompleteMetadata: that one really
		// is a parser or upstream problem, and it should stay visible as a 500.
		switch {
		case err != nil:
			err = classifyProviderError(err)
		case info == nil:
			// The provider ran to completion and returned nothing at all, which
			// is how the catalogue-backed providers spell "no such record".
			err = mt.ErrMovieNotFound
		case !info.IsValid():
			// A record that parsed but is missing its title or artwork is a
			// parser or upstream problem. Reporting it as 404 would turn a site
			// rewrite into a catalogue that silently lost every title.
			err = mt.ErrIncompleteMetadata
		}
	}()
	// Query DB first (by id).
	if lazy {
		if info, err = e.getMovieInfoFromDB(provider, id); err == nil && info.IsValid() {
			return // ignore DB query error.
		}
	}
	// delayed info auto-save.
	defer func() {
		// info is nil whenever the provider answered "no such record" with a nil
		// error, which is how the API-backed sources spell a miss. Dereferencing
		// it here panicked inside the request goroutine.
		if err == nil && info != nil && info.IsValid() {
			e.db.Clauses(clause.OnConflict{
				UpdateAll: true,
			}).Create(info) // ignore error
		}
	}()
	return callback()
}

// classifyProviderError maps the status-text error colly produces onto the
// HTTP-aware sentinel the API layer understands.
//
// colly constructs errors.New(http.StatusText(code)) for every non-2xx response
// when ParseHTTPErrorResponse is off, which is the default, and providers return
// that value verbatim. It carries no type, so nothing downstream could tell a
// missing record from a broken server. Only 404 is remapped: 403 and 429 mean
// "blocked" and "slow down", and a client must not cache either of those as
// "this title does not exist".
func classifyProviderError(err error) error {
	if err != nil && err.Error() == http.StatusText(http.StatusNotFound) {
		return mt.ErrMovieNotFound
	}
	return err
}

func (e *Engine) getMovieInfoByProviderID(provider mt.MovieProvider, id string, lazy bool) (*model.MovieInfo, error) {
	if id = provider.NormalizeMovieID(id); id == "" {
		return nil, mt.ErrInvalidID
	}
	return e.getMovieInfoWithCallback(provider, id, lazy, func() (*model.MovieInfo, error) {
		return provider.GetMovieInfoByID(id)
	})
}

func (e *Engine) GetMovieInfoByProviderID(pid providerid.ProviderID, lazy bool) (*model.MovieInfo, error) {
	provider, err := e.GetMovieProviderByName(pid.Provider)
	if err != nil {
		return nil, err
	}
	return e.getMovieInfoByProviderID(provider, pid.ID, lazy)
}

func (e *Engine) getMovieInfoByProviderURL(provider mt.MovieProvider, rawURL string, lazy bool) (*model.MovieInfo, error) {
	id, err := provider.ParseMovieIDFromURL(rawURL)
	switch {
	case err != nil:
		return nil, err
	case id == "":
		return nil, mt.ErrInvalidURL
	}
	return e.getMovieInfoWithCallback(provider, id, lazy, func() (*model.MovieInfo, error) {
		return provider.GetMovieInfoByURL(rawURL)
	})
}

func (e *Engine) GetMovieInfoByURL(rawURL string, lazy bool) (*model.MovieInfo, error) {
	provider, err := e.GetMovieProviderByURL(rawURL)
	if err != nil {
		return nil, err
	}
	return e.getMovieInfoByProviderURL(provider, rawURL, lazy)
}
