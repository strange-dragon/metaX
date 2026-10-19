// Copyright 2026 metaX contributors.
//
// Feature-level comparison reference: https://github.com/sqzw-x/amane.
// Native Go actor provider; no Amane source or fixtures are vendored.
package avbaseactor

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/PuerkitoBio/goquery"
	"github.com/lib/pq"
	"golang.org/x/text/language"
	"golang.org/x/text/unicode/norm"

	"github.com/metatube-community/metatube-sdk-go/common/parser"
	"github.com/metatube-community/metatube-sdk-go/model"
	mt "github.com/metatube-community/metatube-sdk-go/provider"
)

const (
	Name           = "AVBaseActor"
	Priority       = 1000 - 25
	defaultURL     = "https://www.avbase.net"
	defaultTimeout = 20 * time.Second
	maxPageBytes   = 8 << 20
)

var ErrActorNotFound = errors.New("avbase actor: profile not found")

var (
	_ mt.ActorProvider        = (*Actor)(nil)
	_ mt.ActorSearcher        = (*Actor)(nil)
	_ mt.Fetcher              = (*Actor)(nil)
	_ mt.ProxySetter          = (*Actor)(nil)
	_ mt.RequestTimeoutSetter = (*Actor)(nil)
)

// Actor resolves AVBase talent pages directly by name. AVBase has no usable
// actor search endpoint; /talents?q= returns 404, while aliases can resolve
// through the canonical /talents/{name} route.
type Actor struct {
	baseURL *url.URL

	clientMu sync.RWMutex
	client   *http.Client
	timeout  time.Duration
	proxyURL string

	priorityMu sync.RWMutex
	priority   float64
}

func New() *Actor {
	u, _ := url.Parse(defaultURL)
	p := &Actor{baseURL: u, timeout: defaultTimeout, priority: Priority}
	p.rebuildClientLocked()
	return p
}

func newForTest(base string, client *http.Client) *Actor {
	u, _ := url.Parse(base)
	p := &Actor{baseURL: u, timeout: defaultTimeout, priority: Priority}
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

func (p *Actor) Name() string { return Name }

func (p *Actor) Priority() float64 {
	p.priorityMu.RLock()
	defer p.priorityMu.RUnlock()
	return p.priority
}

func (p *Actor) SetPriority(v float64) {
	p.priorityMu.Lock()
	defer p.priorityMu.Unlock()
	p.priority = v
}

func (p *Actor) Language() language.Tag { return language.Japanese }

func (p *Actor) URL() *url.URL {
	u := *p.baseURL
	return &u
}

func (p *Actor) NormalizeActorID(id string) string {
	return strings.TrimSpace(norm.NFKC.String(id))
}

func (p *Actor) ParseActorIDFromURL(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("avbase actor: parse URL: %w", err)
	}
	if !p.validOrigin(u) || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("avbase actor: expected canonical AVBase URL")
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 || parts[0] != "talents" || !validActorID(parts[1]) {
		return "", fmt.Errorf("avbase actor: invalid talent path %q", u.Path)
	}
	return parts[1], nil
}

func validActorID(id string) bool {
	if strings.TrimSpace(id) == "" || id == "." || id == ".." {
		return false
	}
	for _, r := range id {
		if r == '/' || r == '\\' || r == '?' || r == '#' || unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func (p *Actor) actorURL(id string) string {
	u := *p.baseURL
	u.Path = strings.TrimRight(u.Path, "/") + "/talents/" + id
	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

func effectivePort(u *url.URL) string {
	if port := u.Port(); port != "" {
		return port
	}
	switch strings.ToLower(u.Scheme) {
	case "http":
		return "80"
	case "https":
		return "443"
	default:
		return ""
	}
}

func (p *Actor) validOrigin(u *url.URL) bool {
	if u == nil || u.User != nil || u.Host == "" || !strings.EqualFold(u.Scheme, p.baseURL.Scheme) {
		return false
	}
	if !strings.EqualFold(strings.TrimSuffix(u.Hostname(), "."), strings.TrimSuffix(p.baseURL.Hostname(), ".")) {
		return false
	}
	return effectivePort(u) == effectivePort(p.baseURL)
}

func (p *Actor) checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return fmt.Errorf("avbase actor: stopped after 10 redirects")
	}
	if !p.validOrigin(req.URL) {
		return fmt.Errorf("avbase actor: refusing cross-origin redirect to %q", req.URL)
	}
	return nil
}

func (p *Actor) SetRequestTimeout(timeout time.Duration) {
	p.clientMu.Lock()
	defer p.clientMu.Unlock()
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	p.timeout = timeout
	p.rebuildClientLocked()
}

func (p *Actor) SetProxy(proxyURL string) error {
	proxyURL = strings.TrimSpace(proxyURL)
	if proxyURL != "" {
		u, err := url.Parse(proxyURL)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("avbase actor: invalid proxy URL %q", proxyURL)
		}
		switch strings.ToLower(u.Scheme) {
		case "http", "https", "socks5":
		default:
			return fmt.Errorf("avbase actor: unsupported proxy scheme %q", u.Scheme)
		}
	}
	p.clientMu.Lock()
	defer p.clientMu.Unlock()
	p.proxyURL = proxyURL
	p.rebuildClientLocked()
	return nil
}

func (p *Actor) rebuildClientLocked() {
	transport, ok := http.DefaultTransport.(*http.Transport)
	if ok {
		transport = transport.Clone()
	} else {
		transport = &http.Transport{Proxy: http.ProxyFromEnvironment}
	}
	transport.Proxy = http.ProxyFromEnvironment
	if p.proxyURL != "" {
		if proxyURL, err := url.Parse(p.proxyURL); err == nil {
			transport.Proxy = http.ProxyURL(proxyURL)
		}
	}
	timeout := p.timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	p.client = &http.Client{Transport: transport, Timeout: timeout, CheckRedirect: p.checkRedirect}
}

func (p *Actor) httpClient() *http.Client {
	p.clientMu.RLock()
	defer p.clientMu.RUnlock()
	return p.client
}

func (p *Actor) Fetch(rawURL string) (*http.Response, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("avbase actor: parse request URL: %w", err)
	}
	if !p.validOrigin(u) {
		return nil, fmt.Errorf("avbase actor: refusing request to unexpected origin %q", rawURL)
	}
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("avbase actor: create request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:128.0) Gecko/20100101 Firefox/128.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "ja,en;q=0.8,zh-CN;q=0.6")

	resp, err := p.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("avbase actor: GET %s: %w", rawURL, err)
	}
	if resp.StatusCode == http.StatusNotFound {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("%w: %s", ErrActorNotFound, rawURL)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("avbase actor: GET %s: unexpected HTTP status %s", rawURL, resp.Status)
	}
	return resp, nil
}

func (p *Actor) getDocument(rawURL string) (*goquery.Document, string, error) {
	resp, err := p.Fetch(rawURL)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	finalURL := rawURL
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPageBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("avbase actor: read response: %w", err)
	}
	if len(body) > maxPageBytes {
		return nil, "", fmt.Errorf("avbase actor: response exceeds %d bytes", maxPageBytes)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, "", fmt.Errorf("avbase actor: parse HTML: %w", err)
	}
	return doc, finalURL, nil
}

func (p *Actor) GetActorInfoByID(id string) (*model.ActorInfo, error) {
	id = p.NormalizeActorID(id)
	if !validActorID(id) {
		return nil, fmt.Errorf("avbase actor: invalid actor ID %q", id)
	}
	return p.GetActorInfoByURL(p.actorURL(id))
}

func (p *Actor) GetActorInfoByURL(rawURL string) (*model.ActorInfo, error) {
	if _, err := p.ParseActorIDFromURL(rawURL); err != nil {
		return nil, err
	}
	doc, finalURL, err := p.getDocument(rawURL)
	if err != nil {
		return nil, err
	}
	finalID, err := p.ParseActorIDFromURL(finalURL)
	if err != nil {
		return nil, fmt.Errorf("avbase actor: invalid final profile URL: %w", err)
	}

	var root map[string]any
	rawData := strings.TrimSpace(doc.Find("script#__NEXT_DATA__").First().Text())
	if rawData == "" {
		return nil, fmt.Errorf("%w: missing Next.js page data", ErrActorNotFound)
	}
	if err := json.Unmarshal([]byte(rawData), &root); err != nil {
		return nil, fmt.Errorf("avbase actor: decode Next.js page data: %w", err)
	}
	props := object(root["props"])
	pageProps := object(props["pageProps"])
	talent := object(pageProps["talent"])
	primary := object(talent["primary"])
	name := text(primary["name"])
	if name == "" {
		return nil, fmt.Errorf("%w: missing primary talent name", ErrActorNotFound)
	}

	aliases := make([]string, 0)
	if actors, ok := talent["actors"].([]any); ok {
		for _, item := range actors {
			alias := text(object(item)["name"])
			if alias != "" && !strings.EqualFold(alias, name) {
				aliases = appendUnique(aliases, alias)
			}
		}
	}
	meta := object(talent["meta"])
	basic := object(meta["basic_info"])
	height := integer(basic["height"])
	bust, waist, hip := integer(basic["bust"]), integer(basic["waist"]), integer(basic["hip"])
	var measurements []string
	for _, part := range []struct {
		name  string
		value int
	}{{"B", bust}, {"W", waist}, {"H", hip}} {
		if part.value > 0 {
			measurements = append(measurements, fmt.Sprintf("%s:%d", part.name, part.value))
		}
	}
	imageURL := absoluteURL(p.baseURL, text(primary["image_url"]))
	info := &model.ActorInfo{
		ID:       finalID,
		Name:     name,
		Provider: p.Name(),
		Homepage: finalURL,
		Aliases:  pq.StringArray(aliases),
	}
	if birthday := parser.ParseDate(text(basic["birthday"])); !time.Time(birthday).IsZero() {
		info.Birthday = birthday
	}
	if height > 0 {
		info.Height = height
	}
	if len(measurements) > 0 {
		info.Measurements = strings.Join(measurements, " / ")
	}
	if cup := text(basic["cup"]); cup != "" {
		info.CupSize = cup
	}
	if imageURL != "" {
		info.Images = pq.StringArray{imageURL}
	}
	if !info.IsValid() {
		return nil, mt.ErrIncompleteMetadata
	}
	return info, nil
}

func (p *Actor) SearchActor(keyword string) ([]*model.ActorSearchResult, error) {
	keyword = strings.TrimSpace(norm.NFKC.String(keyword))
	if keyword == "" {
		return nil, nil
	}
	info, err := p.GetActorInfoByID(keyword)
	if errors.Is(err, ErrActorNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !sameName(keyword, info.Name, []string(info.Aliases)) {
		return nil, nil
	}
	return []*model.ActorSearchResult{info.ToSearchResult()}, nil
}

func object(value any) map[string]any {
	if result, ok := value.(map[string]any); ok {
		return result
	}
	return map[string]any{}
}

func text(value any) string {
	switch value := value.(type) {
	case string:
		return strings.TrimSpace(value)
	case json.Number:
		return value.String()
	case float64:
		return strconv.FormatInt(int64(value), 10)
	default:
		return ""
	}
}

func integer(value any) int {
	raw := text(value)
	if raw == "" {
		return 0
	}
	valueInt, err := strconv.Atoi(raw)
	if err != nil || valueInt < 0 {
		return 0
	}
	return valueInt
}

func absoluteURL(base *url.URL, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	ref, err := url.Parse(raw)
	if err != nil || (ref.Scheme != "" && ref.Scheme != "http" && ref.Scheme != "https") {
		return ""
	}
	return base.ResolveReference(ref).String()
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if strings.EqualFold(existing, value) {
			return values
		}
	}
	return append(values, value)
}

func sameName(query, canonical string, aliases []string) bool {
	normalize := func(value string) string {
		return strings.ToLower(strings.Join(strings.Fields(norm.NFKC.String(value)), " "))
	}
	key := normalize(query)
	if key == "" {
		return false
	}
	if key == normalize(canonical) {
		return true
	}
	for _, alias := range aliases {
		if key == normalize(alias) {
			return true
		}
	}
	return false
}

func init() {
	mt.Register(Name, New)
}
