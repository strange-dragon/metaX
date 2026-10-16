// Copyright 2026 metaX contributors.
//
// Native Go actor provider based on independently implemented site behavior.
// Amane is used only as a feature-level comparison reference; no Amane source
// or fixtures are copied into this package.
package minnano

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/PuerkitoBio/goquery"
	"github.com/lib/pq"
	"golang.org/x/text/language"

	"github.com/metatube-community/metatube-sdk-go/common/parser"
	"github.com/metatube-community/metatube-sdk-go/model"
	mt "github.com/metatube-community/metatube-sdk-go/provider"
)

const (
	ActorProviderName = "MinnanoActor"
	Priority          = 1000 - 30
	defaultURL        = "https://www.minnano-av.com"
	maxPageBytes      = 8 << 20
)

var (
	_             mt.ActorProvider = (*Actor)(nil)
	_             mt.ActorSearcher = (*Actor)(nil)
	actorIDRE                      = regexp.MustCompile(`^\d+$`)
	actressHrefRE                  = regexp.MustCompile(`(?i)^/actress(\d+)\.html$`)
	figureRE                       = regexp.MustCompile(`(?i)T\s*\d+\s*/\s*B\s*(\d+)\s*(?:\(\s*([A-Z]+)(?:カップ)?\s*\))?\s*/\s*W\s*(\d+)\s*/\s*H\s*(\d+)`)
	heightRE                       = regexp.MustCompile(`(\d{2,3})\s*(?:cm|センチ)?`)
	cupRE                          = regexp.MustCompile(`(?i)([A-Z]+)\s*カップ`)
	ageGateCookie                  = &http.Cookie{Name: "age_verified", Value: "1", Path: "/"}
)

// Actor implements Minnano's actor search and profile pages.
type Actor struct {
	baseURL *url.URL

	clientMu sync.RWMutex
	client   *http.Client
	priority float64
}

func NewActor() *Actor {
	u, _ := url.Parse(defaultURL)
	return &Actor{baseURL: u, priority: Priority, client: newHTTPClient()}
}

func newHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("minnano actor: stopped after 10 redirects")
			}
			if !sameOrigin(req.URL, mustParseURL(defaultURL)) {
				return fmt.Errorf("minnano actor: refusing cross-origin redirect to %q", req.URL)
			}
			return nil
		},
	}
}

func mustParseURL(raw string) *url.URL {
	u, _ := url.Parse(raw)
	return u
}

func (p *Actor) Name() string           { return ActorProviderName }
func (p *Actor) Priority() float64      { return p.priority }
func (p *Actor) SetPriority(v float64)  { p.priority = v }
func (p *Actor) Language() language.Tag { return language.Japanese }
func (p *Actor) URL() *url.URL          { u := *p.baseURL; return &u }

func (p *Actor) NormalizeActorID(id string) string {
	id = strings.TrimSpace(id)
	if u, err := url.Parse(id); err == nil && u.IsAbs() {
		if parsed, err := p.ParseActorIDFromURL(id); err == nil {
			return parsed
		}
	}
	return id
}

func (p *Actor) ParseActorIDFromURL(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("minnano actor: parse URL: %w", err)
	}
	if u.Scheme == "" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("minnano actor: expected a canonical absolute actor URL")
	}
	if !sameOrigin(u, p.baseURL) {
		return "", fmt.Errorf("minnano actor: unexpected actor origin %q", rawURL)
	}
	m := actressHrefRE.FindStringSubmatch(u.EscapedPath())
	if len(m) != 2 {
		return "", fmt.Errorf("minnano actor: invalid actor path %q", u.Path)
	}
	return m[1], nil
}

func (p *Actor) actorURL(id string) string {
	u := *p.baseURL
	u.Path = strings.TrimRight(u.Path, "/") + "/actress" + id + ".html"
	u.RawQuery, u.Fragment = "", ""
	return u.String()
}

func (p *Actor) GetActorInfoByID(id string) (*model.ActorInfo, error) {
	id = strings.TrimSpace(id)
	if !actorIDRE.MatchString(id) {
		return nil, fmt.Errorf("minnano actor: invalid actor ID %q", id)
	}
	return p.GetActorInfoByURL(p.actorURL(id))
}

func (p *Actor) GetActorInfoByURL(rawURL string) (*model.ActorInfo, error) {
	id, err := p.ParseActorIDFromURL(rawURL)
	if err != nil {
		return nil, err
	}
	doc, err := p.getDocument(rawURL)
	if err != nil {
		return nil, err
	}
	profile := doc.Find("div.act-profile").First()
	if profile.Length() == 0 {
		return nil, fmt.Errorf("minnano actor: profile %q not found", rawURL)
	}
	rawName := cleanText(profile.Find("h2").First().Text())
	if rawName == "" {
		return nil, fmt.Errorf("minnano actor: profile %q has no name", rawURL)
	}
	name, aliases := parseNameLine(rawName)
	for _, row := range profile.Find("tr").EachIter() {
		key := cleanText(row.Find("td span").First().Text())
		if key != "別名" {
			continue
		}
		aliasName, readings := parseNameLine(cleanText(row.Find("td p").First().Text()))
		if aliasName != "" && aliasName != name {
			aliases = appendUnique(aliases, aliasName)
		}
		for _, reading := range readings {
			if reading != name {
				aliases = appendUnique(aliases, reading)
			}
		}
	}

	fields := profileFields(profile)
	height, bust, waist, hip, cup := parseMeasurements(fields["サイズ"])
	if height == 0 {
		if m := heightRE.FindStringSubmatch(fields["身長"]); len(m) == 2 {
			height, _ = strconv.Atoi(m[1])
		}
	}
	birthday := parser.ParseDate(fields["生年月日"])
	image := cleanText(doc.Find(`meta[property="og:image"]`).First().AttrOr("content", ""))
	if image == "" {
		image = cleanText(profile.Find(`div.thumb img[src*="p_actress"]`).First().AttrOr("src", ""))
	}
	if image != "" {
		image = p.absoluteURL(image)
	}
	homepage := p.actorURL(id)
	if canonical := cleanText(doc.Find(`link[rel="canonical"]`).First().AttrOr("href", "")); canonical != "" {
		if canonicalURL := p.absoluteURL(canonical); canonicalURL != "" {
			if canonicalID, parseErr := p.ParseActorIDFromURL(canonicalURL); parseErr == nil && canonicalID == id {
				homepage = canonicalURL
			}
		}
	}
	info := &model.ActorInfo{
		ID: id, Name: name, Provider: p.Name(), Homepage: homepage,
		Aliases: pq.StringArray(aliases),
	}
	if !time.Time(birthday).IsZero() {
		info.Birthday = birthday
	}
	if height > 0 {
		info.Height = height
	}
	if bust > 0 && waist > 0 && hip > 0 {
		info.Measurements = fmt.Sprintf("B:%d / W:%d / H:%d", bust, waist, hip)
	}
	if cup != "" {
		info.CupSize = cup
	}
	if image != "" {
		info.Images = pq.StringArray{image}
	}
	return info, nil
}

func (p *Actor) SearchActor(keyword string) ([]*model.ActorSearchResult, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, nil
	}
	u := *p.baseURL
	u.Path = strings.TrimRight(u.Path, "/") + "/search_result.php"
	q := u.Query()
	q.Set("search_scope", "actress")
	q.Set("search_word", keyword)
	q.Set("search", "Go")
	u.RawQuery = q.Encode()
	doc, err := p.getDocument(u.String())
	if err != nil {
		return nil, err
	}
	if doc.Find("div.act-profile").Length() > 0 {
		canonical := cleanText(doc.Find(`link[rel="canonical"]`).First().AttrOr("href", ""))
		if canonical == "" {
			canonical = cleanText(doc.Find(`meta[property="og:url"]`).First().AttrOr("content", ""))
		}
		if canonical != "" {
			canonical = p.absoluteURL(canonical)
			if id, parseErr := p.ParseActorIDFromURL(canonical); parseErr == nil {
				info, detailErr := p.GetActorInfoByURL(canonical)
				if detailErr == nil && info != nil && sameName(info.Name, keyword, []string(info.Aliases)) {
					return []*model.ActorSearchResult{info.ToSearchResult()}, nil
				}
				_ = id
			}
		}
	}
	results := make([]*model.ActorSearchResult, 0)
	seen := make(map[string]struct{})
	doc.Find("table.tbllist.actress tr td h2.ttl a").Each(func(_ int, a *goquery.Selection) {
		name := cleanText(a.Text())
		href, _ := a.Attr("href")
		if name == "" || !sameName(stripAnnotations(name), keyword, nil) {
			return
		}
		u, parseErr := url.Parse(strings.Split(href, "?")[0])
		if parseErr != nil || !u.IsAbs() && !strings.HasPrefix(u.Path, "/") {
			return
		}
		homepage := p.absoluteURL(u.String())
		id, parseErr := p.ParseActorIDFromURL(homepage)
		if parseErr != nil {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		image := cleanText(a.Closest("tr").Find(`img[src*="p_actress"]`).First().AttrOr("src", ""))
		images := pq.StringArray{}
		if image != "" {
			images = append(images, p.absoluteURL(image))
		}
		result := &model.ActorSearchResult{ID: id, Name: stripAnnotations(name), Provider: p.Name(), Homepage: homepage, Images: images}
		if result.IsValid() {
			results = append(results, result)
		}
	})
	return results, nil
}

func (p *Actor) getDocument(rawURL string) (*goquery.Document, error) {
	u, err := url.Parse(rawURL)
	if err != nil || !sameOrigin(u, p.baseURL) {
		return nil, fmt.Errorf("minnano actor: refusing URL %q", rawURL)
	}
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept-Language", "ja,en-US;q=0.8,en;q=0.6")
	req.AddCookie(ageGateCookie)
	p.clientMu.RLock()
	client := p.client
	p.clientMu.RUnlock()
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("minnano actor: unexpected HTTP status %d for %q", resp.StatusCode, rawURL)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPageBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxPageBytes {
		return nil, fmt.Errorf("minnano actor: response exceeds %d bytes", maxPageBytes)
	}
	return goquery.NewDocumentFromReader(strings.NewReader(string(body)))
}

func (p *Actor) absoluteURL(raw string) string {
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

func sameOrigin(a, b *url.URL) bool {
	return a != nil && b != nil &&
		strings.EqualFold(a.Scheme, b.Scheme) &&
		strings.EqualFold(a.Hostname(), b.Hostname()) &&
		effectivePort(a) == effectivePort(b) &&
		a.User == nil
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

func profileFields(profile *goquery.Selection) map[string]string {
	fields := make(map[string]string)
	profile.Find("tr").Each(func(_ int, row *goquery.Selection) {
		key := cleanText(row.Find("td span").First().Text())
		if key == "" {
			return
		}
		value := cleanText(row.Find("td p").First().Text())
		if value == "" {
			value = cleanText(row.Find("td").First().Text())
			value = strings.TrimSpace(strings.TrimPrefix(value, key))
		}
		if value != "" {
			fields[key] = value
		}
	})
	return fields
}

func parseNameLine(raw string) (string, []string) {
	raw = cleanText(raw)
	openToken, closeToken := "（", "）"
	open := strings.LastIndex(raw, openToken)
	close := strings.LastIndex(raw, closeToken)
	if open < 0 || close <= open {
		openToken, closeToken = "(", ")"
		open = strings.LastIndex(raw, openToken)
		close = strings.LastIndex(raw, closeToken)
	}
	if open <= 0 || close <= open {
		return stripAnnotations(raw), nil
	}
	inside := raw[open+len(openToken) : close]
	parts := strings.SplitN(inside, "/", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return stripAnnotations(raw), nil
	}
	name := strings.TrimSpace(raw[:open])
	return name, []string{strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])}
}

func stripAnnotations(name string) string {
	name = cleanText(name)
	if i := strings.LastIndexAny(name, "（("); i > 0 && strings.HasSuffix(name, ")") || strings.HasSuffix(name, "）") {
		if i > 0 {
			return strings.TrimSpace(name[:i])
		}
	}
	return name
}

func parseMeasurements(raw string) (height, bust, waist, hip int, cup string) {
	raw = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return r
		}
		if r >= '０' && r <= '９' {
			return rune('0' + (r - '０'))
		}
		return r
	}, raw)
	if m := figureRE.FindStringSubmatch(raw); len(m) == 5 {
		bust, _ = strconv.Atoi(m[1])
		cup = strings.ToUpper(m[2])
		waist, _ = strconv.Atoi(m[3])
		hip, _ = strconv.Atoi(m[4])
	}
	if m := regexp.MustCompile(`(?i)(\d{2,3})\s*cm`).FindStringSubmatch(raw); len(m) == 2 {
		height, _ = strconv.Atoi(m[1])
	}
	if cup == "" {
		if m := cupRE.FindStringSubmatch(raw); len(m) == 2 {
			cup = strings.ToUpper(m[1])
		}
	}
	return
}

func cleanText(s string) string { return strings.Join(strings.Fields(s), " ") }

func sameName(canonical, query string, aliases []string) bool {
	normalize := func(s string) string {
		return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(s)), " "))
	}
	query = normalize(query)
	if query == "" {
		return false
	}
	if normalize(canonical) == query {
		return true
	}
	for _, alias := range aliases {
		if normalize(alias) == query {
			return true
		}
	}
	return false
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	if value == "" {
		return values
	}
	return append(values, value)
}

func init() { mt.Register(ActorProviderName, NewActor) }
