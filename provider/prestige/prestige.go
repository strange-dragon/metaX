// Copyright 2026 metaX contributors.
//
// Feature-level comparison reference: https://github.com/sqzw-x/amane.
// Native implementation for MetaTube's Provider API; no Amane source or
// fixtures are vendored. See ../../NOTICE.md for attribution.
package prestige

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lib/pq"
	"golang.org/x/text/language"
	"golang.org/x/text/unicode/norm"

	"github.com/metatube-community/metatube-sdk-go/common/parser"
	"github.com/metatube-community/metatube-sdk-go/model"
	mt "github.com/metatube-community/metatube-sdk-go/provider"
	"github.com/strange-dragon/metaX/provider/httpretry"
)

const (
	Name           = "Prestige"
	Priority       = 1000 - 31
	defaultURL     = "https://www.prestige-av.com"
	defaultTimeout = 20 * time.Second
	maxPageBytes   = 8 << 20
)

var (
	ErrMovieNotFound                         = errors.New("prestige: movie not found")
	skuRE                                    = regexp.MustCompile(`^[A-Z0-9]+(?:-[A-Z0-9]+)*$`)
	uuidRE                                   = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	_                mt.MovieProvider        = (*Prestige)(nil)
	_                mt.MovieSearcher        = (*Prestige)(nil)
	_                mt.Fetcher              = (*Prestige)(nil)
	_                mt.ProxySetter          = (*Prestige)(nil)
	_                mt.RequestTimeoutSetter = (*Prestige)(nil)
)

type Prestige struct {
	baseURL *url.URL

	clientMu sync.RWMutex
	client   *http.Client
	timeout  time.Duration
	proxyURL string

	priorityMu sync.RWMutex
	priority   float64
}

type nameValue struct {
	Name string `json:"name"`
}

type imageRef struct {
	Path string `json:"path"`
}

type skuPayload struct {
	ParentProduct *struct {
		UUID string `json:"uuid"`
	} `json:"parentProduct"`
}

type productPayload struct {
	UUID         string          `json:"uuid"`
	Title        string          `json:"title"`
	MgsStartAt   string          `json:"mgsStartAt"`
	CreatedAt    string          `json:"createdAt"`
	PlayTime     json.RawMessage `json:"playTime"`
	Actress      []nameValue     `json:"actress"`
	Directors    []nameValue     `json:"directors"`
	Genre        []nameValue     `json:"genre"`
	Maker        json.RawMessage `json:"maker"`
	Label        json.RawMessage `json:"label"`
	Series       json.RawMessage `json:"series"`
	Body         string          `json:"body"`
	Thumbnail    *imageRef       `json:"thumbnail"`
	PackageImage *imageRef       `json:"packageImage"`
	Media        []imageRef      `json:"media"`
	SKU          []struct {
		DeliveryItemID string `json:"deliveryItemId"`
	} `json:"sku"`
}

func New() *Prestige {
	u, _ := url.Parse(defaultURL)
	p := &Prestige{baseURL: u, timeout: defaultTimeout, priority: Priority}
	p.rebuildClientLocked()
	return p
}

func newForTest(base string, client *http.Client) *Prestige {
	u, _ := url.Parse(base)
	p := &Prestige{baseURL: u, timeout: defaultTimeout, priority: Priority}
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

func (p *Prestige) Name() string { return Name }
func (p *Prestige) Priority() float64 {
	p.priorityMu.RLock()
	defer p.priorityMu.RUnlock()
	return p.priority
}
func (p *Prestige) SetPriority(v float64) {
	p.priorityMu.Lock()
	defer p.priorityMu.Unlock()
	p.priority = v
}
func (p *Prestige) Language() language.Tag { return language.Japanese }
func (p *Prestige) URL() *url.URL          { u := *p.baseURL; return &u }

func (p *Prestige) NormalizeMovieID(id string) string {
	return canonicalSKU(id)
}
func (p *Prestige) NormalizeMovieKeyword(keyword string) string {
	return canonicalSKU(keyword)
}

func canonicalSKU(value string) string {
	value = strings.ToUpper(norm.NFKC.String(strings.TrimSpace(value)))
	value = strings.NewReplacer("_", "-", " ", "", "　", "").Replace(value)
	return value
}

func compactSKU(value string) string {
	return strings.ReplaceAll(canonicalSKU(value), "-", "")
}

func normalizeSKU(value string) string {
	return compactSKU(value)
}

func skuCandidates(value string) ([]string, error) {
	canonical := canonicalSKU(value)
	if canonical == "" || !skuRE.MatchString(canonical) {
		return nil, fmt.Errorf("prestige: invalid catalog number %q", value)
	}
	compact := compactSKU(canonical)
	candidates := []string{canonical, compact, "GOOE" + canonical, "GOOE" + compact}
	seen := make(map[string]struct{}, len(candidates))
	out := candidates[:0]
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if _, exists := seen[candidate]; exists {
			continue
		}
		seen[candidate] = struct{}{}
		out = append(out, candidate)
	}
	return out, nil
}

func (p *Prestige) validOrigin(u *url.URL) bool {
	if u == nil || u.User != nil || u.Host == "" || !strings.EqualFold(u.Scheme, p.baseURL.Scheme) {
		return false
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	base := strings.TrimSuffix(strings.ToLower(p.baseURL.Hostname()), ".")
	if host != base && !(base == "prestige-av.com" && host == "www.prestige-av.com") &&
		!(base == "www.prestige-av.com" && host == "prestige-av.com") {
		return false
	}
	return effectivePort(u) == effectivePort(p.baseURL)
}

func effectivePort(u *url.URL) string {
	if port := u.Port(); port != "" {
		return port
	}
	if strings.EqualFold(u.Scheme, "https") {
		return "443"
	}
	if strings.EqualFold(u.Scheme, "http") {
		return "80"
	}
	return ""
}

func (p *Prestige) productURL(uuid string) string {
	u := *p.baseURL
	u.Path = "/api/product/" + url.PathEscape(uuid)
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

func (p *Prestige) skuURL(sku string) string {
	u := *p.baseURL
	u.Path = "/api/sku/item/" + url.PathEscape(sku)
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

func (p *Prestige) parseProductUUID(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("prestige: parse product URL: %w", err)
	}
	if !p.validOrigin(u) {
		return "", fmt.Errorf("prestige: unexpected product origin %q", raw)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("prestige: product URL must not contain query or fragment")
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 3 || parts[0] != "api" || parts[1] != "product" || !uuidRE.MatchString(parts[2]) {
		return "", fmt.Errorf("prestige: invalid product path %q", u.Path)
	}
	return parts[2], nil
}

func (p *Prestige) ParseMovieIDFromURL(raw string) (string, error) {
	return p.parseProductUUID(raw)
}

func (p *Prestige) checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return fmt.Errorf("prestige: stopped after 10 redirects")
	}
	if !p.validOrigin(req.URL) {
		return fmt.Errorf("prestige: refusing cross-origin redirect to %q", req.URL)
	}
	return nil
}

func (p *Prestige) SetRequestTimeout(v time.Duration) {
	p.clientMu.Lock()
	defer p.clientMu.Unlock()
	if v <= 0 {
		v = defaultTimeout
	}
	p.timeout = v
	p.rebuildClientLocked()
}

func (p *Prestige) SetProxy(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw != "" {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("prestige: invalid proxy URL")
		}
		switch strings.ToLower(u.Scheme) {
		case "http", "https", "socks5":
		default:
			return fmt.Errorf("prestige: unsupported proxy scheme %q", u.Scheme)
		}
	}
	p.clientMu.Lock()
	defer p.clientMu.Unlock()
	p.proxyURL = raw
	p.rebuildClientLocked()
	return nil
}

func (p *Prestige) rebuildClientLocked() {
	tr := http.DefaultTransport.(*http.Transport).Clone()
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
	p.client = &http.Client{
		Transport:     httpretry.WrapTransport(tr),
		Timeout:       timeout,
		CheckRedirect: p.checkRedirect,
	}
}

func (p *Prestige) httpClient() *http.Client {
	p.clientMu.RLock()
	defer p.clientMu.RUnlock()
	return p.client
}

func (p *Prestige) Fetch(raw string) (*http.Response, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("prestige: parse request URL: %w", err)
	}
	if !p.validOrigin(u) {
		return nil, fmt.Errorf("prestige: refusing request to unexpected origin %q", raw)
	}
	req, err := http.NewRequest(http.MethodGet, raw, nil)
	if err != nil {
		return nil, fmt.Errorf("prestige: create request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:128.0) Gecko/20100101 Firefox/128.0")
	req.Header.Set("Accept", "application/json,text/plain,*/*")
	req.Header.Set("Accept-Language", "ja-JP,ja;q=0.9,en;q=0.7")
	req.Header.Set("Referer", p.baseURL.String()+"/")
	resp, err := p.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("prestige: GET %s: %w", raw, err)
	}
	if resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		return nil, fmt.Errorf("%w: %s", ErrMovieNotFound, raw)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resp.Body.Close()
		return nil, fmt.Errorf("prestige: unexpected HTTP status %s", resp.Status)
	}
	return resp, nil
}

func (p *Prestige) getJSON(raw string, dst any) (string, error) {
	resp, err := p.Fetch(raw)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	finalURL := raw
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPageBytes+1))
	if err != nil {
		return "", fmt.Errorf("prestige: read response: %w", err)
	}
	if len(body) > maxPageBytes {
		return "", fmt.Errorf("prestige: response exceeds %d bytes", maxPageBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(dst); err != nil {
		return "", fmt.Errorf("prestige: parse JSON: %w", err)
	}
	return finalURL, nil
}

func (p *Prestige) searchProductBySKU(sku string) (*productPayload, string, error) {
	var payload skuPayload
	skuFinalURL, err := p.getJSON(p.skuURL(sku), &payload)
	if err != nil {
		return nil, "", err
	}
	if payload.ParentProduct == nil || !uuidRE.MatchString(payload.ParentProduct.UUID) {
		return nil, skuFinalURL, nil
	}
	product, productFinalURL, err := p.getProduct(payload.ParentProduct.UUID)
	if err != nil {
		return nil, skuFinalURL, err
	}
	if product.UUID == "" {
		product.UUID = payload.ParentProduct.UUID
	}
	return product, productFinalURL, nil
}

func (p *Prestige) getProduct(uuid string) (*productPayload, string, error) {
	if !uuidRE.MatchString(uuid) {
		return nil, "", fmt.Errorf("prestige: invalid product UUID %q", uuid)
	}
	var payload productPayload
	finalURL, err := p.getJSON(p.productURL(uuid), &payload)
	if err != nil {
		return nil, "", err
	}
	finalUUID, err := p.parseProductUUID(finalURL)
	if err != nil || !strings.EqualFold(finalUUID, uuid) {
		return nil, finalURL, fmt.Errorf("%w: product identity changed", ErrMovieNotFound)
	}
	return &payload, finalURL, nil
}

func (p *Prestige) SearchMovie(keyword string) ([]*model.MovieSearchResult, error) {
	keyword = p.NormalizeMovieKeyword(keyword)
	candidates, err := skuCandidates(keyword)
	if err != nil {
		return nil, err
	}

	var lastErr error
	for _, sku := range candidates {
		product, productURL, err := p.searchProductBySKU(sku)
		if err != nil {
			if errors.Is(err, ErrMovieNotFound) {
				continue
			}
			lastErr = err
			continue
		}
		if product == nil {
			continue
		}
		info, err := p.productInfo(product, productURL)
		if err != nil {
			if errors.Is(err, ErrMovieNotFound) {
				continue
			}
			lastErr = err
			continue
		}
		if !sameCatalogNumber(info.Number, keyword) {
			continue
		}
		return []*model.MovieSearchResult{info.ToSearchResult()}, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, nil
}

func sameCatalogNumber(left, right string) bool {
	left = normalizeSKU(left)
	right = normalizeSKU(right)
	if left == right {
		return true
	}
	return left == strings.TrimPrefix(right, "GOOE")
}

func (p *Prestige) GetMovieInfoByID(id string) (*model.MovieInfo, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("prestige: empty movie ID")
	}
	if u, err := url.Parse(id); err == nil && u.IsAbs() {
		return p.GetMovieInfoByURL(id)
	}
	if uuidRE.MatchString(id) {
		product, finalURL, err := p.getProduct(id)
		if err != nil {
			return nil, err
		}
		return p.productInfo(product, finalURL)
	}
	if !skuRE.MatchString(canonicalSKU(id)) {
		return nil, fmt.Errorf("prestige: invalid movie ID %q", id)
	}
	results, err := p.SearchMovie(id)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, ErrMovieNotFound
	}
	return p.GetMovieInfoByURL(results[0].Homepage)
}

func (p *Prestige) GetMovieInfoByURL(raw string) (*model.MovieInfo, error) {
	uuid, err := p.parseProductUUID(raw)
	if err != nil {
		return nil, err
	}
	product, finalURL, err := p.getProduct(uuid)
	if err != nil {
		return nil, err
	}
	return p.productInfo(product, finalURL)
}

func (p *Prestige) productInfo(product *productPayload, homepage string) (*model.MovieInfo, error) {
	if product == nil {
		return nil, ErrMovieNotFound
	}
	number := ""
	fallbackNumber := ""
	for _, sku := range product.SKU {
		id := strings.TrimSpace(sku.DeliveryItemID)
		if id == "" {
			continue
		}
		if fallbackNumber == "" {
			fallbackNumber = id
		}
		upper := strings.ToUpper(id)
		if !strings.HasPrefix(upper, "GOOE") && !strings.HasPrefix(upper, "GOO") {
			number = id
			break
		}
	}
	if number == "" {
		number = fallbackNumber
	}
	if number == "" {
		return nil, ErrMovieNotFound
	}

	title := strings.Join(strings.Fields(strings.TrimSpace(product.Title)), " ")
	cover := p.imageURL(product.Thumbnail)
	thumb := p.imageURL(product.PackageImage)
	if cover == "" {
		cover = thumb
	}
	if thumb == "" {
		thumb = cover
	}
	if title == "" || cover == "" {
		return nil, ErrMovieNotFound
	}

	actors := nameValues(product.Actress)
	directors := nameValues(product.Directors)
	genres := nameValues(product.Genre)

	studio := "プレステージ"
	label := firstNonEmpty(nameFromRaw(product.Label), nameFromRaw(product.Maker))
	series := nameFromRaw(product.Series)

	release := strings.TrimSpace(product.MgsStartAt)
	if release == "" {
		release = strings.TrimSpace(product.CreatedAt)
	}
	if index := strings.IndexByte(release, 'T'); index >= 0 {
		release = release[:index]
	}

	preview := make([]string, 0, len(product.Media))
	for _, image := range product.Media {
		if raw := p.imageURL(&image); raw != "" {
			preview = append(preview, raw)
		}
	}

	info := &model.MovieInfo{
		ID:            number,
		Number:        number,
		Title:         title,
		Summary:       strings.TrimSpace(product.Body),
		Provider:      p.Name(),
		Homepage:      homepage,
		Director:      strings.Join(directors, ", "),
		Actors:        pq.StringArray(actors),
		ThumbURL:      thumb,
		BigThumbURL:   thumb,
		CoverURL:      cover,
		BigCoverURL:   cover,
		PreviewImages: pq.StringArray(preview),
		Maker:         studio,
		Label:         label,
		Series:        series,
		Genres:        pq.StringArray(genres),
		Runtime:       parseRuntime(product.PlayTime),
	}
	if release != "" {
		info.ReleaseDate = parser.ParseDate(release)
	}
	if !info.IsValid() {
		return nil, ErrMovieNotFound
	}
	return info, nil
}

func nameValues(values []nameValue) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		name := strings.Join(strings.Fields(strings.TrimSpace(value.Name)), " ")
		if name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

func nameFromRaw(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	}
	var object struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(raw, &object) == nil && object.Name != "" {
		return strings.Join(strings.Fields(strings.TrimSpace(object.Name)), " ")
	}
	var list []json.RawMessage
	if json.Unmarshal(raw, &list) == nil {
		for _, item := range list {
			if name := nameFromRaw(item); name != "" {
				return name
			}
		}
	}
	return ""
}

func parseRuntime(raw json.RawMessage) int {
	if len(raw) == 0 {
		return 0
	}
	var number json.Number
	if json.Unmarshal(raw, &number) == nil {
		if value, err := number.Int64(); err == nil {
			return int(value)
		}
		if value, err := strconv.ParseFloat(number.String(), 64); err == nil {
			return int(value)
		}
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		text = strings.TrimSpace(text)
		if value, err := strconv.Atoi(text); err == nil {
			return value
		}
		if match := regexp.MustCompile(`\d+`).FindString(text); match != "" {
			value, _ := strconv.Atoi(match)
			return value
		}
	}
	return 0
}

func (p *Prestige) imageURL(image *imageRef) string {
	if image == nil {
		return ""
	}
	raw := strings.TrimSpace(image.Path)
	if raw == "" {
		return ""
	}
	if u, err := url.Parse(raw); err == nil && u.IsAbs() {
		if !p.validOrigin(u) {
			return ""
		}
		return u.String()
	}
	raw = strings.TrimPrefix(raw, "/")
	if raw == "" || strings.Contains(raw, "..") {
		return ""
	}
	base := *p.baseURL
	base.Path = ""
	base.RawQuery = ""
	base.Fragment = ""
	if strings.HasPrefix(raw, "api/media/") {
		return strings.TrimSuffix(base.String(), "/") + "/" + raw
	}
	return strings.TrimSuffix(base.String(), "/") + "/api/media/" + raw
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func init() { mt.Register(Name, New) }
