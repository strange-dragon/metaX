package theporndb

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/gocolly/colly/v2"
	"golang.org/x/text/language"

	"github.com/strange-dragon/metaX/common/number"
	"github.com/strange-dragon/metaX/model"
	"github.com/strange-dragon/metaX/provider"
	"github.com/strange-dragon/metaX/provider/internal/scraper"
)

var (
	_ provider.MovieProvider = (*ThePornDBVideo)(nil)
	_ provider.MovieSearcher = (*ThePornDBVideo)(nil)
	_ provider.ConfigSetter  = (*ThePornDBVideo)(nil)
)

const (
	SceneProviderName = "ThePornDBScene"
	MovieProviderName = "ThePornDBMovie"

	movieBaseURL = "https://theporndb.net/movies/"
	sceneBaseURL = "https://theporndb.net/scenes/"

	moviePageURL = "https://theporndb.net/movies/%s"
	scenePageURL = "https://theporndb.net/scenes/%s"

	apiGetMovieURL  = "https://api.theporndb.net/movies/%s"
	apiGetScenesURL = "https://api.theporndb.net/scenes/%s"

	apiSearchMovieURL = "https://api.theporndb.net/movies?q=%s"
	apiSearchSceneURL = "https://api.theporndb.net/scenes?q=%s"
)

var thePornDBIDRE = regexp.MustCompile(`^[A-Za-z0-9]+(?:-[A-Za-z0-9]+)*$`)

// parseThePornDBDetailID validates a public detail URL for the expected collection.
func parseThePornDBDetailID(rawURL, collection string) (string, error) {
	homepage, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	if !homepage.IsAbs() || homepage.Host == "" || !strings.EqualFold(homepage.Scheme, "https") {
		return "", fmt.Errorf("theporndb: expected an absolute HTTPS detail URL")
	}
	if homepage.User != nil {
		return "", fmt.Errorf("theporndb: userinfo is not allowed in detail URLs")
	}
	if host := strings.TrimSuffix(strings.ToLower(homepage.Hostname()), "."); host != "theporndb.net" {
		return "", fmt.Errorf("theporndb: unexpected detail host %q", homepage.Hostname())
	}
	if port := homepage.Port(); port != "" && port != "443" {
		return "", fmt.Errorf("theporndb: unexpected detail port %q", port)
	}
	parts := strings.Split(strings.Trim(homepage.Path, "/"), "/")
	if len(parts) != 2 || parts[0] != collection || !thePornDBIDRE.MatchString(parts[1]) {
		return "", fmt.Errorf("theporndb: URL is not a supported %s detail path", collection)
	}
	return parts[1], nil
}

// ThePornDB have different API endpoints for scenes and movies, but response
// JSON schema is the same. So we need to register 2 video providers.

type ThePornDBVideo struct {
	*scraper.Scraper

	pageURL      string
	apiGetURL    string
	apiSearchURL string

	accessToken string
}

func new(name, baseURL, pageURL, apiGetURL, apiSearchURL string) *ThePornDBVideo {
	return &ThePornDBVideo{
		Scraper:      scraper.NewDefaultScraper(name, baseURL, Priority, language.English),
		pageURL:      pageURL,
		apiGetURL:    apiGetURL,
		apiSearchURL: apiSearchURL,
		accessToken:  "",
	}
}

func NewThePornDBScene() *ThePornDBVideo {
	return new(SceneProviderName, sceneBaseURL, scenePageURL, apiGetScenesURL, apiSearchSceneURL)
}

func NewThePornDBMovie() *ThePornDBVideo {
	return new(MovieProviderName, movieBaseURL, moviePageURL, apiGetMovieURL, apiSearchMovieURL)
}

func (s *ThePornDBVideo) SetConfig(config provider.Config) error {
	if err := s.Scraper.SetConfig(config); err != nil {
		return fmt.Errorf("theporndb: configure movie transport: %w", err)
	}
	token, err := accessTokenFromConfig(config)
	if err != nil {
		return fmt.Errorf("theporndb: read movie access token: %w", err)
	}
	if token != "" {
		s.accessToken = token
	}
	return nil
}

// GetMovieInfoByID impls MovieProvider.GetMovieInfoByID.
func (s *ThePornDBVideo) GetMovieInfoByID(id string) (info *model.MovieInfo, err error) {
	id = strings.TrimSpace(id)
	if !thePornDBIDRE.MatchString(id) {
		return nil, fmt.Errorf("theporndb: invalid movie ID")
	}
	if s.accessToken == "" {
		return nil, nil
	}

	info = &model.MovieInfo{
		Provider:      s.Name(),
		Actors:        []string{},
		PreviewImages: []string{},
		Genres:        []string{},
	}

	c := s.ClonedCollector()

	c.OnResponse(func(r *colly.Response) {
		resp := &getVideoResponse{}
		if err = json.Unmarshal(r.Body, resp); err != nil {
			return
		}

		info.ID = resp.Data.Slug
		info.Number = resp.Data.Slug
		info.Homepage = fmt.Sprintf(s.pageURL, info.ID)
		info.Title = resp.Data.Title
		info.Summary = resp.Data.Description
		info.ThumbURL = resp.Data.Poster
		info.CoverURL = resp.Data.Image
		info.Score = float64(resp.Data.Rating)
		info.PreviewVideoURL = resp.Data.Trailer
		info.Maker = resp.Data.Site.Name
		info.Runtime = resp.Data.Duration

		if releaseDate, err := resp.Data.ReleaseDate(); err == nil {
			info.ReleaseDate = releaseDate
		}

		for _, tag := range resp.Data.Tags {
			info.Genres = append(info.Genres, tag.Name)
		}

		for _, actor := range resp.Data.Performers {
			info.Actors = append(info.Actors, actor.Name)
		}

		if len(resp.Data.Directors) > 0 {
			info.Director = resp.Data.Directors[0].Name
		}
	})

	headers := http.Header{}
	headers.Set("Authorization", fmt.Sprintf("Bearer %s", s.accessToken))
	err = c.Request(http.MethodGet, fmt.Sprintf(s.apiGetURL, id), nil, nil, headers)
	return
}

// ParseMovieIDFromURL impls MovieProvider.ParseMovieIDFromURL.
func (s *ThePornDBVideo) ParseMovieIDFromURL(rawURL string) (string, error) {
	collection := "movies"
	if s.Name() == SceneProviderName {
		collection = "scenes"
	}
	return parseThePornDBDetailID(rawURL, collection)
}

// GetMovieInfoByURL impls MovieProvider.GetMovieInfoByURL.
func (s *ThePornDBVideo) GetMovieInfoByURL(rawURL string) (info *model.MovieInfo, err error) {
	id, err := s.ParseMovieIDFromURL(rawURL)
	if err != nil {
		return
	}

	return s.GetMovieInfoByID(id)
}

// NormalizeMovieKeyword impls MovieSearcher.NormalizeMovieKeyword.
func (s *ThePornDBVideo) NormalizeMovieKeyword(keyword string) string {
	if number.IsSpecial(keyword) {
		return ""
	}
	return strings.ToUpper(keyword)
}

// SearchMovie impls MovieSearcher.SearchMovie.
func (s *ThePornDBVideo) SearchMovie(keyword string) (results []*model.MovieSearchResult, err error) {
	if s.accessToken == "" {
		return nil, nil
	}

	c := s.ClonedCollector()

	c.OnResponse(func(r *colly.Response) {
		resp := &searchVideosResponse{}
		if err = json.Unmarshal(r.Body, resp); err != nil {
			return
		}
		for _, video := range resp.Data {
			releaseDate, _ := video.ReleaseDate()
			results = append(results, &model.MovieSearchResult{
				ID:          video.Slug,
				Number:      video.Slug,
				Title:       video.Title,
				Provider:    s.Name(),
				Homepage:    fmt.Sprintf(s.apiGetURL, video.Slug),
				ThumbURL:    video.Poster,
				CoverURL:    video.Image,
				ReleaseDate: releaseDate,
			})
		}
	})

	headers := http.Header{}
	headers.Set("Authorization", fmt.Sprintf("Bearer %s", s.accessToken))
	err = c.Request(http.MethodGet, fmt.Sprintf(s.apiSearchURL, url.QueryEscape(keyword)), nil, nil, headers)
	return
}
