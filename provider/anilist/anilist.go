// Package anilist provides title-based anime/hentai metadata from AniList.
//
// AniList indexes English, Japanese, and romaji titles. Chinese titles are
// resolved through Bangumi's public search endpoint first, then looked up on
// AniList. This keeps title searches language-agnostic without requiring a
// catalog number.
package anilist

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/strange-dragon/metaX/model"
	"github.com/strange-dragon/metaX/provider"
	"github.com/strange-dragon/metaX/provider/httpretry"
	"golang.org/x/text/language"
	"gorm.io/datatypes"
)

var (
	_ provider.MovieProvider = (*AniList)(nil)
	_ provider.MovieSearcher = (*AniList)(nil)
)

const (
	Name     = "AniList"
	Priority = 1000 - 2
)

const (
	baseURL         = "https://anilist.co/"
	graphqlURL      = "https://graphql.anilist.co"
	bangumiURL      = "https://api.bgm.tv/search/subject/"
	defaultTimeout  = 20 * time.Second
	animePagePrefix = "ANILIST-"
)

var (
	animeURLRE  = regexp.MustCompile(`^/anime/([0-9]+)(?:/.*)?$`)
	numberRE    = regexp.MustCompile(`(?i)^(?:ANILIST|ANILIST-|ANILIST_)\s*([0-9]+)$`)
	htmlTagRE   = regexp.MustCompile(`<[^>]+>`)
	bangumiList = 5
)

type AniList struct {
	baseURL    *url.URL
	graphqlURL string
	bangumiURL string
	client     *http.Client
	priority   float64
}

func New() *AniList {
	base, _ := url.Parse(baseURL)
	return newWithClient(base, graphqlURL, bangumiURL, &http.Client{
		Timeout:   defaultTimeout,
		Transport: httpretry.WrapTransport(nil),
	})
}

func newWithClient(base *url.URL, graphql, bangumi string, client *http.Client) *AniList {
	if client == nil {
		client = &http.Client{Timeout: defaultTimeout}
	}
	return &AniList{
		baseURL:    base,
		graphqlURL: graphql,
		bangumiURL: bangumi,
		client:     client,
		priority:   Priority,
	}
}

func (a *AniList) Name() string                          { return Name }
func (a *AniList) Priority() float64                     { return a.priority }
func (a *AniList) SetPriority(v float64)                 { a.priority = v }
func (a *AniList) Language() language.Tag                { return language.English }
func (a *AniList) URL() *url.URL                         { u := *a.baseURL; return &u }
func (a *AniList) NormalizeMovieKeyword(v string) string { return strings.TrimSpace(v) }

func (a *AniList) NormalizeMovieID(id string) string {
	id = strings.TrimSpace(id)
	if match := numberRE.FindStringSubmatch(id); len(match) == 2 {
		return match[1]
	}
	if _, err := strconv.Atoi(id); err == nil {
		return id
	}
	return ""
}

func (a *AniList) ParseMovieIDFromURL(rawURL string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", fmt.Errorf("anilist: parse URL: %w", err)
	}
	if !a.validOrigin(u) {
		return "", fmt.Errorf("anilist: unexpected detail origin %q", rawURL)
	}
	match := animeURLRE.FindStringSubmatch(u.Path)
	if len(match) != 2 {
		return "", fmt.Errorf("anilist: invalid detail path %q", u.Path)
	}
	return match[1], nil
}

func (a *AniList) validOrigin(u *url.URL) bool {
	if u == nil || u.User != nil || u.Host == "" || !strings.EqualFold(u.Scheme, "https") {
		return false
	}
	if port := u.Port(); port != "" && port != "443" {
		return false
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	return host == "anilist.co" || host == "www.anilist.co"
}

func (a *AniList) GetMovieInfoByURL(rawURL string) (*model.MovieInfo, error) {
	id, err := a.ParseMovieIDFromURL(rawURL)
	if err != nil {
		return nil, err
	}
	return a.GetMovieInfoByID(id)
}

func (a *AniList) GetMovieInfoByID(id string) (*model.MovieInfo, error) {
	id = a.NormalizeMovieID(id)
	if id == "" {
		return nil, provider.ErrInvalidID
	}
	media, err := a.fetchMedia(id)
	if err != nil {
		return nil, err
	}
	if media == nil {
		return nil, provider.ErrInfoNotFound
	}
	info := a.mediaInfo(*media, "")
	if !info.IsValid() {
		return nil, provider.ErrIncompleteMetadata
	}
	return info, nil
}

func (a *AniList) SearchMovie(keyword string) ([]*model.MovieSearchResult, error) {
	keyword = a.NormalizeMovieKeyword(keyword)
	if keyword == "" {
		return nil, nil
	}

	// Aggregated detail requests use the stable ANILIST-<id> number.
	if id := a.NormalizeMovieID(keyword); id != "" && strings.HasPrefix(strings.ToUpper(keyword), "ANILIST") {
		info, err := a.GetMovieInfoByID(id)
		if err != nil {
			if err == provider.ErrInfoNotFound {
				return nil, nil
			}
			return nil, err
		}
		return []*model.MovieSearchResult{info.ToSearchResult()}, nil
	}

	media, err := a.searchPage(keyword)
	if err != nil {
		return nil, err
	}
	if len(media) == 0 && containsHan(keyword) {
		aliases, aliasErr := a.bangumiAliases(keyword)
		if aliasErr == nil {
			seen := map[int]struct{}{}
			for _, alias := range aliases {
				rows, searchErr := a.searchPage(alias)
				if searchErr != nil {
					continue
				}
				for _, row := range rows {
					if _, ok := seen[row.ID]; ok {
						continue
					}
					seen[row.ID] = struct{}{}
					media = append(media, row)
				}
				if len(media) >= 10 {
					break
				}
			}
		}
	}

	results := make([]*model.MovieSearchResult, 0, len(media))
	for _, row := range media {
		results = append(results, a.mediaSearchResult(row, keyword))
	}
	return results, nil
}

func (a *AniList) searchPage(keyword string) ([]animeMedia, error) {
	cacheKey := a.graphqlURL + "\x00search\x00" + strings.ToLower(keyword)
	if media, ok := cachedSearch(cacheKey); ok {
		return media, nil
	}
	var response struct {
		Page struct {
			Media []animeMedia `json:"media"`
		} `json:"Page"`
	}
	variables := map[string]any{
		"search":  keyword,
		"page":    1,
		"perPage": 10,
	}
	if err := a.postGraphQL(searchQuery, variables, &response); err != nil {
		return nil, err
	}
	storeSearch(cacheKey, response.Page.Media)
	return response.Page.Media, nil
}

func (a *AniList) fetchMedia(id string) (*animeMedia, error) {
	cacheKey := a.graphqlURL + "\x00detail\x00" + id
	if media, ok := cachedDetail(cacheKey); ok {
		return media, nil
	}
	var response struct {
		Media *animeMedia `json:"Media"`
	}
	if err := a.postGraphQL(detailQuery, map[string]any{"id": id}, &response); err != nil {
		return nil, err
	}
	storeDetail(cacheKey, response.Media)
	return response.Media, nil
}

func (a *AniList) postGraphQL(query string, variables map[string]any, out any) error {
	body, err := json.Marshal(map[string]any{"query": query, "variables": variables})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, a.graphqlURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "metaX/1.0 (https://github.com/strange-dragon/metaX)")
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return provider.ErrInfoNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("anilist: graphql status %d", resp.StatusCode)
	}
	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return fmt.Errorf("anilist: decode response: %w", err)
	}
	if len(envelope.Errors) > 0 {
		return fmt.Errorf("anilist: %s", envelope.Errors[0].Message)
	}
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return provider.ErrInfoNotFound
	}
	return json.Unmarshal(envelope.Data, out)
}

func (a *AniList) mediaInfo(media animeMedia, keyword string) *model.MovieInfo {
	info := &model.MovieInfo{
		ID:            strconv.Itoa(media.ID),
		Number:        animePagePrefix + strconv.Itoa(media.ID),
		Title:         pickTitle(media.Title, keyword),
		Summary:       cleanDescription(media.Description),
		Provider:      Name,
		Homepage:      fmt.Sprintf("https://anilist.co/anime/%d", media.ID),
		Actors:        []string{},
		PreviewImages: []string{},
		Genres:        append([]string{}, media.Genres...),
	}
	if media.CoverImage.ExtraLarge != "" {
		info.CoverURL = media.CoverImage.ExtraLarge
	} else {
		info.CoverURL = media.CoverImage.Large
	}
	info.ThumbURL = media.CoverImage.Large
	if info.ThumbURL == "" {
		info.ThumbURL = info.CoverURL
	}
	info.BigCoverURL = info.CoverURL
	if media.StartDate.Year > 0 {
		info.ReleaseDate = dateValue(media.StartDate)
	}
	if media.Duration > 0 {
		info.Runtime = media.Duration
	}
	if media.AverageScore > 0 {
		info.Score = float64(media.AverageScore) / 20
	}
	for _, studio := range media.Studios.Edges {
		if info.Maker == "" {
			info.Maker = strings.TrimSpace(studio.Node.Name)
		}
	}
	for _, staff := range media.Staff.Edges {
		if strings.Contains(strings.ToLower(staff.Role), "director") {
			info.Director = strings.TrimSpace(staff.Node.Name.Full)
			break
		}
	}
	seenActors := map[string]struct{}{}
	for _, character := range media.Characters.Edges {
		for _, actor := range character.VoiceActors {
			name := strings.TrimSpace(actor.Name.Full)
			if name == "" {
				name = strings.TrimSpace(actor.Name.Native)
			}
			if name == "" {
				continue
			}
			if _, ok := seenActors[name]; ok {
				continue
			}
			seenActors[name] = struct{}{}
			info.Actors = append(info.Actors, name)
		}
	}
	return info
}

func (a *AniList) mediaSearchResult(media animeMedia, keyword string) *model.MovieSearchResult {
	info := a.mediaInfo(media, keyword)
	return info.ToSearchResult()
}

func (a *AniList) bangumiAliases(keyword string) ([]string, error) {
	cacheKey := a.bangumiURL + "\x00" + strings.ToLower(keyword)
	if aliases, ok := cachedAliases(cacheKey); ok {
		return aliases, nil
	}
	u, err := url.Parse(a.bangumiURL)
	if err != nil {
		return nil, err
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/" + keyword
	q := u.Query()
	q.Set("type", "2")
	q.Set("responseGroup", "large")
	q.Set("max_results", strconv.Itoa(bangumiList))
	u.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "metaX/1.0 (https://github.com/strange-dragon/metaX)")
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil
	}
	var payload struct {
		List []struct {
			Name   string `json:"name"`
			NameCN string `json:"name_cn"`
		} `json:"list"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return nil, nil // alias resolution is best-effort
	}
	aliases := make([]string, 0, len(payload.List)*2)
	seen := map[string]struct{}{}
	for _, item := range payload.List {
		for _, alias := range []string{item.Name, item.NameCN} {
			alias = strings.TrimSpace(alias)
			if alias == "" {
				continue
			}
			key := strings.ToLower(alias)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			aliases = append(aliases, alias)
		}
	}
	storeAliases(cacheKey, aliases)
	return aliases, nil
}

func pickTitle(titles animeTitles, keyword string) string {
	if strings.TrimSpace(keyword) == "" {
		if value := strings.TrimSpace(titles.English); value != "" {
			return value
		}
		if value := strings.TrimSpace(titles.Romaji); value != "" {
			return value
		}
		return strings.TrimSpace(titles.Native)
	}
	if containsHan(keyword) && strings.TrimSpace(titles.Native) != "" {
		return strings.TrimSpace(titles.Native)
	}
	if containsASCII(keyword) {
		if value := strings.TrimSpace(titles.English); value != "" {
			return value
		}
		if value := strings.TrimSpace(titles.Romaji); value != "" {
			return value
		}
	}
	if value := strings.TrimSpace(titles.Native); value != "" {
		return value
	}
	if value := strings.TrimSpace(titles.English); value != "" {
		return value
	}
	return strings.TrimSpace(titles.Romaji)
}

func containsHan(value string) bool {
	for _, r := range value {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

func containsASCII(value string) bool {
	for _, r := range value {
		if r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			return true
		}
	}
	return false
}

func cleanDescription(value string) string {
	value = htmlTagRE.ReplaceAllString(value, "\n")
	value = html.UnescapeString(value)
	lines := strings.Split(value, "\n")
	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		if line = strings.TrimSpace(line); line != "" {
			cleaned = append(cleaned, line)
		}
	}
	return strings.Join(cleaned, "\n")
}

func dateValue(date animeDate) datatypes.Date {
	month := time.Month(date.Month)
	if month < time.January || month > time.December {
		month = time.January
	}
	day := date.Day
	if day < 1 || day > 31 {
		day = 1
	}
	return datatypes.Date(time.Date(date.Year, month, day, 0, 0, 0, 0, time.UTC))
}
