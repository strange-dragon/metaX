// Copyright 2026 metaX contributors.
//
// Feature-level comparison reference: https://github.com/sqzw-x/amane.
// Native implementation for MetaTube's Provider API; no Amane source or
// fixtures are vendored. See NOTICE.md for attribution.
package avsox

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/lib/pq"
	"golang.org/x/text/language"
	"golang.org/x/text/unicode/norm"

	"github.com/metatube-community/metatube-sdk-go/common/parser"
	"github.com/metatube-community/metatube-sdk-go/model"
	mt "github.com/metatube-community/metatube-sdk-go/provider"
)

const (
	Name           = "AVSOX"
	Priority       = 1000 - 21
	defaultURL     = "https://avsox.click"
	defaultTimeout = 20 * time.Second
	maxPageBytes   = 8 << 20
)

var ErrMovieNotFound = errors.New("avsox: movie not found")

var (
	_ mt.MovieProvider        = (*AVSOX)(nil)
	_ mt.MovieSearcher        = (*AVSOX)(nil)
	_ mt.Fetcher              = (*AVSOX)(nil)
	_ mt.ProxySetter          = (*AVSOX)(nil)
	_ mt.RequestTimeoutSetter = (*AVSOX)(nil)
)

type AVSOX struct {
	baseURL    *url.URL
	clientMu   sync.RWMutex
	client     *http.Client
	timeout    time.Duration
	proxyURL   string
	priorityMu sync.RWMutex
	priority   float64
}

func New() *AVSOX {
	u, _ := url.Parse(defaultURL)
	p := &AVSOX{baseURL: u, timeout: defaultTimeout, priority: Priority}
	p.rebuildClientLocked()
	return p
}

func newForTest(base string, client *http.Client) *AVSOX {
	u, _ := url.Parse(base)
	p := &AVSOX{baseURL: u, timeout: defaultTimeout, priority: Priority}
	if client == nil {
		p.rebuildClientLocked()
		return p
	}
	c := *client
	c.CheckRedirect = p.checkRedirect
	if c.Timeout <= 0 {
		c.Timeout = defaultTimeout
	}
	p.client = &c
	return p
}

func (p *AVSOX) Name() string { return Name }
func (p *AVSOX) Priority() float64 {
	p.priorityMu.RLock()
	defer p.priorityMu.RUnlock()
	return p.priority
}
func (p *AVSOX) SetPriority(v float64) {
	p.priorityMu.Lock()
	defer p.priorityMu.Unlock()
	p.priority = v
}
func (p *AVSOX) Language() language.Tag                { return language.SimplifiedChinese }
func (p *AVSOX) URL() *url.URL                         { u := *p.baseURL; return &u }
func (p *AVSOX) NormalizeMovieID(id string) string     { return strings.TrimSpace(id) }
func (p *AVSOX) NormalizeMovieKeyword(s string) string { return strings.TrimSpace(norm.NFKC.String(s)) }

func validLanguage(s string) bool {
	switch s {
	case "cn", "tw", "ja", "en":
		return true
	}
	return false
}

func (p *AVSOX) ParseMovieIDFromURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("avsox: parse URL: %w", err)
	}
	if !p.validOrigin(u) {
		return "", fmt.Errorf("avsox: unexpected detail origin %q", raw)
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	var id string
	switch {
	case len(parts) == 2 && (parts[0] == "movies" || parts[0] == "movie"):
		id = parts[1]
	case len(parts) == 3 && validLanguage(parts[0]) && (parts[1] == "movies" || parts[1] == "movie"):
		id = parts[2]
	default:
		return "", fmt.Errorf("avsox: invalid detail path %q", u.Path)
	}
	if id == "" {
		return "", fmt.Errorf("avsox: missing movie ID")
	}
	for _, r := range id {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return "", fmt.Errorf("avsox: invalid movie ID %q", id)
		}
	}
	return id, nil
}

func effectivePort(u *url.URL) string {
	if v := u.Port(); v != "" {
		return v
	}
	switch strings.ToLower(u.Scheme) {
	case "http":
		return "80"
	case "https":
		return "443"
	}
	return ""
}
func (p *AVSOX) validOrigin(u *url.URL) bool {
	if u == nil || u.User != nil || u.Host == "" || !strings.EqualFold(u.Scheme, p.baseURL.Scheme) {
		return false
	}
	if !strings.EqualFold(strings.TrimSuffix(u.Hostname(), "."), strings.TrimSuffix(p.baseURL.Hostname(), ".")) {
		return false
	}
	return effectivePort(u) == effectivePort(p.baseURL)
}
func (p *AVSOX) checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return fmt.Errorf("avsox: stopped after 10 redirects")
	}
	if !p.validOrigin(req.URL) {
		return fmt.Errorf("avsox: refusing redirect to unexpected origin %q", req.URL)
	}
	return nil
}

func (p *AVSOX) SetRequestTimeout(d time.Duration) {
	p.clientMu.Lock()
	defer p.clientMu.Unlock()
	if d <= 0 {
		d = defaultTimeout
	}
	p.timeout = d
	p.rebuildClientLocked()
}
func (p *AVSOX) SetProxy(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw != "" {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("avsox: invalid proxy URL %q", raw)
		}
		switch strings.ToLower(u.Scheme) {
		case "http", "https", "socks5":
		default:
			return fmt.Errorf("avsox: unsupported proxy scheme %q", u.Scheme)
		}
	}
	p.clientMu.Lock()
	defer p.clientMu.Unlock()
	p.proxyURL = raw
	p.rebuildClientLocked()
	return nil
}
func (p *AVSOX) rebuildClientLocked() {
	var tr *http.Transport
	if base, ok := http.DefaultTransport.(*http.Transport); ok {
		tr = base.Clone()
	} else {
		tr = &http.Transport{Proxy: http.ProxyFromEnvironment}
	}
	tr.Proxy = http.ProxyFromEnvironment
	if p.proxyURL != "" {
		if u, err := url.Parse(p.proxyURL); err == nil {
			tr.Proxy = http.ProxyURL(u)
		}
	}
	timeout := p.timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	p.client = &http.Client{Transport: tr, Timeout: timeout, CheckRedirect: p.checkRedirect}
}
func (p *AVSOX) httpClient() *http.Client {
	p.clientMu.RLock()
	defer p.clientMu.RUnlock()
	return p.client
}
func (p *AVSOX) setHeaders(req *http.Request) {
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:128.0) Gecko/20100101 Firefox/128.0")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.7")
}
func (p *AVSOX) Fetch(raw string) (*http.Response, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("avsox: parse request URL: %w", err)
	}
	if !p.validOrigin(u) {
		return nil, fmt.Errorf("avsox: refusing request to unexpected origin %q", raw)
	}
	req, err := http.NewRequest(http.MethodGet, raw, nil)
	if err != nil {
		return nil, fmt.Errorf("avsox: create request: %w", err)
	}
	p.setHeaders(req)
	resp, err := p.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("avsox: GET %s: %w", raw, err)
	}
	if resp.StatusCode == http.StatusNotFound {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("%w: %s", ErrMovieNotFound, raw)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("avsox: GET %s: unexpected HTTP status %s", raw, resp.Status)
	}
	return resp, nil
}
func (p *AVSOX) readBody(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxPageBytes+1))
	if err != nil {
		return nil, fmt.Errorf("avsox: read response: %w", err)
	}
	if len(b) > maxPageBytes {
		return nil, fmt.Errorf("avsox: response exceeds %d bytes", maxPageBytes)
	}
	return b, nil
}
func (p *AVSOX) csrfToken() (string, error) {
	u := *p.baseURL
	u.Path = path.Join(u.Path, "cn") + "/"
	resp, err := p.Fetch(u.String())
	if err != nil {
		return "", err
	}
	body, err := p.readBody(resp)
	if err != nil {
		return "", err
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("avsox: parse homepage: %w", err)
	}
	token, _ := doc.Find("meta[name='csrf-token']").First().Attr("content")
	return strings.TrimSpace(token), nil
}
func (p *AVSOX) apiCall(method string, payload any, referer string) (json.RawMessage, error) {
	token, err := p.csrfToken()
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("avsox: encode %s payload: %w", method, err)
	}
	endpoint := *p.baseURL
	endpoint.Path = path.Join(endpoint.Path, "javu", "data", "api", method)
	req, err := http.NewRequest(http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("avsox: create %s request: %w", method, err)
	}
	p.setHeaders(req)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", p.baseURL.String())
	req.Header.Set("Referer", referer)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	if token != "" {
		req.Header.Set("X-CSRF-Token", token)
	}
	resp, err := p.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("avsox: POST %s: %w", method, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("avsox: POST %s: unexpected HTTP status %s", method, resp.Status)
	}
	responseBody, err := p.readBody(resp)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(responseBody, &envelope); err != nil {
		return nil, fmt.Errorf("avsox: decode %s response: %w", method, err)
	}
	if envelope.Code != http.StatusOK || len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return nil, fmt.Errorf("%w: API %s returned code %d", ErrMovieNotFound, method, envelope.Code)
	}
	return envelope.Data, nil
}

func (p *AVSOX) detailURL(id, lang string) string {
	if !validLanguage(lang) {
		lang = "cn"
	}
	u := *p.baseURL
	u.Path = path.Join(u.Path, lang, "movies", id)
	return u.String()
}
func (p *AVSOX) searchURL(number, lang string) string {
	if !validLanguage(lang) {
		lang = "cn"
	}
	u := *p.baseURL
	u.Path = path.Join(u.Path, lang, "search", url.PathEscape(number))
	return u.String()
}
func foldNumber(s string) string {
	s = strings.ToLower(strings.TrimSpace(norm.NFKC.String(s)))
	return strings.NewReplacer("-", "", " ", "").Replace(s)
}
func firstString(values ...string) string {
	for _, s := range values {
		if s = strings.TrimSpace(s); s != "" {
			return s
		}
	}
	return ""
}

func (p *AVSOX) absoluteAssetURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	ref, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return p.baseURL.ResolveReference(ref).String()
}
func anyString(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case json.Number:
		return v.String()
	case float64:
		return strconv.FormatInt(int64(v), 10)
	default:
		return ""
	}
}
func namedValue(value any, prefix string) string {
	if list, ok := value.([]any); ok {
		if len(list) == 0 {
			return ""
		}
		value = list[0]
	}
	object, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	for _, suffix := range []string{"", "_ja", "_en", "_cn", "_tw"} {
		if s := anyString(object[prefix+"Name"+suffix]); s != "" {
			return s
		}
	}
	return ""
}

type moviePayload struct {
	MovieID       string           `json:"movieId"`
	Number        string           `json:"movieFanHao"`
	Title         string           `json:"title"`
	TitleJA       string           `json:"title_ja"`
	TitleEN       string           `json:"title_en"`
	TitleCN       string           `json:"title_cn"`
	TitleTW       string           `json:"title_tw"`
	ReleaseDate   string           `json:"releaseDate"`
	Length        json.RawMessage  `json:"length"`
	Description   string           `json:"description"`
	DescriptionJA string           `json:"description_ja"`
	DescriptionEN string           `json:"description_en"`
	DescriptionCN string           `json:"description_cn"`
	DescriptionTW string           `json:"description_tw"`
	PosterSmall   string           `json:"posterSmall"`
	PosterLarge   string           `json:"posterLarge"`
	SampleLarge   []string         `json:"sampleLarge"`
	Studio        any              `json:"studio"`
	Star          []map[string]any `json:"star"`
	Genre         []map[string]any `json:"genre"`
	Director      any              `json:"director"`
	Label         any              `json:"label"`
	Series        any              `json:"series"`
}

func parseLength(raw json.RawMessage) int {
	if len(raw) == 0 || string(raw) == "null" {
		return 0
	}
	var n int
	if json.Unmarshal(raw, &n) == nil {
		return n
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		n, _ = strconv.Atoi(strings.TrimSpace(s))
		return n
	}
	return 0
}
func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
func nonEmpty(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = appendUnique(result, value)
		}
	}
	return result
}

func (p *AVSOX) absoluteAssets(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if resolved := p.absoluteAssetURL(value); resolved != "" {
			result = appendUnique(result, resolved)
		}
	}
	return result
}

func (p *AVSOX) SearchMovie(keyword string) ([]*model.MovieSearchResult, error) {
	number := p.NormalizeMovieKeyword(keyword)
	if number == "" {
		return nil, nil
	}
	lang := "cn"
	payload := []any{map[string]string{"search": number, "lang": lang}, 60, 1}
	data, err := p.apiCall("search", payload, p.searchURL(number, lang))
	if err != nil {
		if errors.Is(err, ErrMovieNotFound) {
			return nil, nil
		}
		return nil, err
	}
	var movies []map[string]any
	if err := json.Unmarshal(data, &movies); err != nil {
		return nil, fmt.Errorf("avsox: decode search results: %w", err)
	}
	query, folded := strings.ToLower(number), foldNumber(number)
	var loose map[string]any
	for _, movie := range movies {
		fanhao, id := anyString(movie["movieFanHao"]), anyString(movie["movieId"])
		if fanhao == "" || id == "" {
			continue
		}
		if strings.ToLower(fanhao) == query {
			return []*model.MovieSearchResult{p.searchResult(movie, lang)}, nil
		}
		if loose == nil && foldNumber(fanhao) == folded {
			loose = movie
		}
	}
	if loose == nil {
		return nil, nil
	}
	return []*model.MovieSearchResult{p.searchResult(loose, lang)}, nil
}
func (p *AVSOX) searchResult(movie map[string]any, lang string) *model.MovieSearchResult {
	id, number := anyString(movie["movieId"]), anyString(movie["movieFanHao"])
	title := firstString(anyString(movie["title"]), anyString(movie["title_ja"]), anyString(movie["title_en"]), anyString(movie["title_cn"]), anyString(movie["title_tw"]))
	return &model.MovieSearchResult{
		ID: id, Number: number, Title: title, Provider: p.Name(),
		Homepage: p.detailURL(id, lang), CoverURL: p.absoluteAssetURL(anyString(movie["posterSmall"])),
		ThumbURL: p.absoluteAssetURL(anyString(movie["posterLarge"])),
	}
}
func (p *AVSOX) GetMovieInfoByID(id string) (*model.MovieInfo, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("avsox: empty movie ID")
	}
	if u, err := url.Parse(id); err == nil && u.IsAbs() {
		return p.GetMovieInfoByURL(id)
	}
	results, err := p.SearchMovie(id)
	if err != nil {
		return nil, err
	}
	for _, result := range results {
		if foldNumber(result.Number) == foldNumber(id) {
			return p.GetMovieInfoByURL(result.Homepage)
		}
	}
	if strings.ContainsAny(id, "/\\?#") {
		return nil, fmt.Errorf("avsox: invalid movie ID %q", id)
	}
	return p.GetMovieInfoByURL(p.detailURL(id, "cn"))
}
func (p *AVSOX) GetMovieInfoByURL(rawURL string) (*model.MovieInfo, error) {
	id, err := p.ParseMovieIDFromURL(rawURL)
	if err != nil {
		return nil, err
	}
	u, _ := url.Parse(rawURL)
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	lang := "cn"
	if len(parts) == 3 {
		lang = parts[0]
	}
	data, err := p.apiCall("getMovie", []any{id, lang}, rawURL)
	if err != nil {
		return nil, err
	}
	var movie moviePayload
	if err := json.Unmarshal(data, &movie); err != nil {
		return nil, fmt.Errorf("avsox: decode movie details: %w", err)
	}
	if movie.MovieID != "" && movie.MovieID != id {
		return nil, fmt.Errorf("%w: requested ID %q but API returned %q", ErrMovieNotFound, id, movie.MovieID)
	}
	if strings.TrimSpace(movie.Number) == "" {
		return nil, ErrMovieNotFound
	}
	title := firstString(movie.Title, movie.TitleJA, movie.TitleEN, movie.TitleCN, movie.TitleTW)
	if title == "" {
		return nil, ErrMovieNotFound
	}
	cover := p.absoluteAssetURL(firstString(movie.PosterSmall, movie.PosterLarge))
	if cover == "" {
		return nil, ErrMovieNotFound
	}
	var actors, genres []string
	for _, star := range movie.Star {
		if name := namedValue(star, "star"); name != "" {
			actors = appendUnique(actors, name)
		}
	}
	for _, genre := range movie.Genre {
		if name := namedValue(genre, "genre"); name != "" {
			genres = appendUnique(genres, name)
		}
	}
	info := &model.MovieInfo{
		ID: id, Number: movie.Number, Title: title,
		Summary:  firstString(movie.Description, movie.DescriptionCN, movie.DescriptionJA, movie.DescriptionEN, movie.DescriptionTW),
		Provider: p.Name(), Homepage: rawURL, Director: namedValue(movie.Director, "director"),
		Actors: pq.StringArray(actors), CoverURL: cover,
		ThumbURL:      firstString(movie.PosterLarge, movie.PosterSmall),
		PreviewImages: pq.StringArray(nonEmpty(movie.SampleLarge)),
		Maker:         namedValue(movie.Studio, "studio"), Label: namedValue(movie.Label, "label"),
		Series: namedValue(movie.Series, "series"), Genres: pq.StringArray(genres),
		Runtime: parseLength(movie.Length),
	}
	if movie.ReleaseDate != "" {
		info.ReleaseDate = parser.ParseDate(movie.ReleaseDate)
	}
	if !info.IsValid() {
		return nil, ErrMovieNotFound
	}
	return info, nil
}

func init() { mt.Register(Name, New) }
