package avbase

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/gocolly/colly/v2"
	"golang.org/x/text/language"

	"github.com/strange-dragon/metaX/common/fetch"
	"github.com/strange-dragon/metaX/common/number"
	"github.com/strange-dragon/metaX/common/parser"
	"github.com/strange-dragon/metaX/common/singledo"
	"github.com/strange-dragon/metaX/model"
	"github.com/strange-dragon/metaX/provider"
	"github.com/strange-dragon/metaX/provider/duga"
	"github.com/strange-dragon/metaX/provider/fanza"
	"github.com/strange-dragon/metaX/provider/getchu"
	"github.com/strange-dragon/metaX/provider/internal/scraper"
	"github.com/strange-dragon/metaX/provider/mgstage"
	"github.com/strange-dragon/metaX/provider/pcolle"
)

var (
	_ provider.MovieProvider = (*AVBase)(nil)
	_ provider.MovieSearcher = (*AVBase)(nil)
	_ provider.Fetcher       = (*AVBase)(nil)
)

const (
	Name     = "AVBASE"
	Priority = 1000 - 4
)

const (
	baseURL      = "https://www.avbase.net/"
	movieURL     = "https://www.avbase.net/works/%s"
	movieAPIURL  = "https://www.avbase.net/_next/data/%s/works/%s.json?id=%s"
	searchAPIURL = "https://www.avbase.net/_next/data/%s/works.json?q=%s"
)

type AVBase struct {
	*fetch.Fetcher
	*scraper.Scraper
	single    *singledo.Single
	providers map[string]provider.MovieProvider
}

func New() *AVBase {
	return &AVBase{
		Fetcher: fetch.Default(&fetch.Config{SkipVerify: true}),
		Scraper: scraper.NewDefaultScraper(
			Name, baseURL, Priority, language.Japanese,
			scraper.WithHeaders(map[string]string{
				"Referer": baseURL,
			})),
		single: singledo.NewSingle(2 * time.Hour),
		providers: map[string]provider.MovieProvider{
			"duga":    duga.New(),
			"fanza":   fanza.New(),
			"getchu":  getchu.New(),
			"mgstage": mgstage.New(),
			"pcolle":  pcolle.New(),
		},
	}
}

func (ab *AVBase) NormalizeMovieID(id string) string {
	if !strings.Contains(id, ":") {
		return strings.ToUpper(id)
	}
	ss := strings.SplitN(id, ":", 2)
	prefix, workID := ss[0], ss[1]
	return ab.JoinPrefixID(prefix, workID)
}

func (ab *AVBase) GetMovieInfoByID(id string) (info *model.MovieInfo, err error) {
	return ab.GetMovieInfoByURL(fmt.Sprintf(movieURL, id))
}

func (ab *AVBase) ParseMovieIDFromURL(rawURL string) (string, error) {
	homepage, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	return ab.NormalizeMovieID(path.Base(homepage.Path)), nil
}

func (ab *AVBase) GetMovieInfoByURL(rawURL string) (info *model.MovieInfo, err error) {
	id, err := ab.ParseMovieIDFromURL(rawURL)
	if err != nil {
		return
	}

	buildID, err := ab.GetBuildID()
	if err != nil {
		return
	}

	c := ab.ClonedCollector()

	c.OnResponse(func(r *colly.Response) {
		data := struct {
			PageProps struct {
				Work workResponse `json:"work"`
			} `json:"pageProps"`
		}{}
		if err = json.Unmarshal(r.Body, &data); err == nil {
			workInfo, _ := ab.getMovieInfoFromWork(data.PageProps.Work)
			srcInfo, srcErr := ab.getMovieInfoFromSource(data.PageProps.Work)
			if srcErr != nil {
				info = workInfo /* ignore error and fallback to work info */
				return
			}
			// use source info.
			info = srcInfo
			// supplement info fields.
			if info.Maker == "" {
				info.Maker = workInfo.Maker
			}
			if info.Label == "" {
				info.Label = workInfo.Label
			}
			if info.Series == "" {
				info.Series = workInfo.Series
			}
			if info.Summary == "" {
				info.Summary = workInfo.Summary
			}
			if len(info.Genres) == 0 {
				info.Genres = workInfo.Genres
			}
			// replace actor names.
			if len(workInfo.Actors) > 0 {
				info.Actors = workInfo.Actors
			}
			// prefer workID number.
			if workInfo.Number != "" {
				info.Number = workInfo.Number
			}
			// choose right ID for info.
			if len(workInfo.ID) > len(id) && strings.Contains(workInfo.ID, ":") {
				id = workInfo.ID
			}
		}
	})

	c.OnScraped(func(_ *colly.Response) {
		if info != nil {
			// As a provider wrapper.
			info.ID = id
			info.Provider = ab.Name()
			info.Homepage = rawURL
		}
	})

	if vErr := c.Visit(fmt.Sprintf(movieAPIURL, buildID, id, url.QueryEscape(id))); vErr != nil {
		err = vErr
	}
	return
}

func (ab *AVBase) getMovieInfoFromWork(work workResponse) (info *model.MovieInfo, err error) {
	info = &model.MovieInfo{
		ID:            ab.JoinPrefixID(work.Prefix, work.WorkID),
		Number:        work.WorkID,
		Actors:        []string{},
		PreviewImages: []string{},
		Genres:        []string{},
	}
	sort.SliceStable(work.Products, func(i, j int) bool {
		// we want mgs > fanza > duga, etc.
		return work.Products[i].Source > work.Products[j].Source
	})
	for _, product := range work.Products {
		if info.Title == "" {
			info.Title = product.Title
		}
		if info.CoverURL == "" {
			info.CoverURL = product.ImageURL
		}
		if info.ThumbURL == "" {
			info.ThumbURL = product.ThumbnailURL
		}
		if info.Maker == "" {
			info.Maker = product.Maker.Name
		}
		if info.Label == "" {
			info.Label = product.Label.Name
		}
		if info.Series == "" {
			info.Series = product.Series.Name
		}
		if info.Summary == "" {
			info.Summary = product.ItemInfo.Description
		}
		if time.Time(info.ReleaseDate).IsZero() {
			info.ReleaseDate = parser.ParseDate(product.Date)
		}
		if len(info.PreviewImages) == 0 {
			for _, sample := range product.SampleImageURLS {
				if sample.L == "" {
					continue
				}
				info.PreviewImages = append(info.PreviewImages, sample.L)
			}
		}
	}
	for _, genre := range work.Genres {
		info.Genres = append(info.Genres, genre.Name)
	}
	for _, cast := range work.Casts {
		info.Actors = append(info.Actors, cast.Actor.Name)
	}
	return
}

func (ab *AVBase) getMovieInfoFromSource(work workResponse) (info *model.MovieInfo, err error) {
	for _, product := range work.Products {
		movieProvider, ok := ab.providers[product.Source]
		if !ok {
			continue
		}
		info, err = movieProvider.GetMovieInfoByID(product.ProductID)
		if err != nil || info == nil || !info.IsValid() {
			continue
		}
		break
	}
	if info == nil || !info.IsValid() {
		if err == nil {
			err = provider.ErrInfoNotFound
		}
	}
	return
}

func (ab *AVBase) NormalizeMovieKeyword(keyword string) string {
	if number.IsUncensored(keyword) || number.IsFC2(keyword) {
		return "" // no uncensored support.
	}
	return strings.ToUpper(keyword)
}

func (ab *AVBase) searchWorks(keyword string) (works []workResponse, err error) {
	buildID, err := ab.GetBuildID()
	if err != nil {
		return
	}

	c := ab.ClonedCollector()
	c.OnResponse(func(r *colly.Response) {
		data := struct {
			PageProps struct {
				Works []workResponse `json:"works"`
			} `json:"pageProps"`
		}{}
		if json.Unmarshal(r.Body, &data) == nil {
			works = append(works, data.PageProps.Works...)
		}
	})
	err = c.Visit(fmt.Sprintf(searchAPIURL, buildID, url.QueryEscape(keyword)))
	return
}

var (
	avbaseSeparators = regexp.MustCompile(`[^A-Z0-9]+`)
	avbaseDigitRuns  = regexp.MustCompile(`0+([0-9])`)
	avbaseDashable   = regexp.MustCompile(`^(.+[A-Z])(\d+)$`)
)

func normalizeAVBaseNumber(value string) string {
	value = avbaseSeparators.ReplaceAllString(strings.ToUpper(value), "")
	return avbaseDigitRuns.ReplaceAllString(value, "$1")
}

func avbaseQueryForms(keyword string) []string {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil
	}
	forms := []string{keyword}
	compact := strings.NewReplacer("-", "", " ", "").Replace(keyword)
	if compact != "" && compact != keyword {
		forms = append(forms, compact)
	}
	if !strings.Contains(keyword, "-") {
		if match := avbaseDashable.FindStringSubmatch(compact); len(match) == 3 {
			dashed := match[1] + "-" + match[2]
			found := false
			for _, form := range forms {
				if form == dashed {
					found = true
					break
				}
			}
			if !found {
				forms = append(forms, dashed)
			}
		}
	}
	return forms
}

func avbaseProductHasNumber(work workResponse, key string) bool {
	for _, product := range work.Products {
		if normalizeAVBaseNumber(product.ProductID) == key {
			return true
		}
	}
	return false
}

func pickAVBaseWork(works []workResponse, key string) (workResponse, bool) {
	candidates := make([]workResponse, 0, len(works))
	seen := make(map[string]struct{}, len(works))
	for _, work := range works {
		if key == "" || normalizeAVBaseNumber(work.WorkID) != key {
			continue
		}
		id := strings.ToUpper(strings.TrimSpace(work.Prefix)) + ":" + strings.ToUpper(strings.TrimSpace(work.WorkID))
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		candidates = append(candidates, work)
	}
	if len(candidates) == 0 {
		return workResponse{}, false
	}
	if len(candidates) == 1 {
		return candidates[0], true
	}
	matchedProducts := make([]workResponse, 0, len(candidates))
	for _, work := range candidates {
		if avbaseProductHasNumber(work, key) {
			matchedProducts = append(matchedProducts, work)
		}
	}
	if len(matchedProducts) == 1 {
		return matchedProducts[0], true
	}
	if len(matchedProducts) > 1 {
		candidates = matchedProducts
	}
	var unprefixed []workResponse
	for _, work := range candidates {
		if strings.TrimSpace(work.Prefix) == "" {
			unprefixed = append(unprefixed, work)
		}
	}
	if len(unprefixed) == 1 {
		return unprefixed[0], true
	}
	return workResponse{}, false
}

func (ab *AVBase) SearchMovie(keyword string) (results []*model.MovieSearchResult, err error) {
	key := normalizeAVBaseNumber(keyword)
	if key == "" {
		return nil, nil
	}
	for _, form := range avbaseQueryForms(keyword) {
		works, searchErr := ab.searchWorks(form)
		if searchErr != nil {
			err = searchErr
			continue
		}
		work, ok := pickAVBaseWork(works, key)
		if !ok {
			continue
		}
		sort.SliceStable(work.Products, func(i, j int) bool {
			return work.Products[i].Source > work.Products[j].Source
		})
		index := -1
		for i, product := range work.Products {
			if _, ok := ab.providers[product.Source]; ok {
				index = i
				break
			}
		}
		if index < 0 {
			continue
		}
		result := &model.MovieSearchResult{
			ID:          ab.JoinPrefixID(work.Prefix, work.WorkID),
			Number:      work.WorkID,
			Title:       work.Title,
			Provider:    ab.Name(),
			Homepage:    ab.movieURLForWork(work.Prefix, work.WorkID),
			ThumbURL:    work.Products[index].ThumbnailURL,
			CoverURL:    work.Products[index].ImageURL,
			ReleaseDate: parser.ParseDate(work.MinDate),
		}
		for _, actor := range work.Actors {
			result.Actors = append(result.Actors, actor.Name)
		}
		return []*model.MovieSearchResult{result}, nil
	}
	return nil, err
}

func (ab *AVBase) JoinPrefixID(prefix, workID string) string {
	if strings.TrimSpace(prefix) == "" {
		return workID
	}
	return fmt.Sprintf("%s:%s", prefix, workID)
}

func (ab *AVBase) movieURLForWork(prefix, workID string) string {
	return fmt.Sprintf(movieURL, ab.JoinPrefixID(prefix, workID))
}

func (ab *AVBase) GetBuildID() (string, error) {
	v, err, _ := ab.single.Do(func() (any, error) {
		return ab.getBuildID()
	})
	if err != nil {
		return "", err
	}
	return v.(string), nil
}

func (ab *AVBase) getBuildID() (buildID string, err error) {
	defer func() {
		if err == nil && buildID == "" {
			err = errors.New("empty build id")
		}
	}()

	c := ab.ClonedCollector()

	c.OnXML(`//*[@id="__NEXT_DATA__"]`, func(e *colly.XMLElement) {
		data := struct {
			BuildId string `json:"buildId"`
		}{}
		if err = json.NewDecoder(strings.NewReader(e.Text)).Decode(&data); err == nil {
			buildID = data.BuildId
		}
	})

	if vErr := c.Visit(baseURL); vErr != nil {
		err = vErr
	}
	return
}

func init() {
	// The stability of this provider is still unknown.
	provider.Register(Name, New)
}
