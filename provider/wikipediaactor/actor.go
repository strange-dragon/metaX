// Copyright 2026 metaX contributors.
//
// Feature-level comparison reference: https://github.com/sqzw-x/amane.
// Native Go actor provider; no Amane source or fixtures are vendored.
package wikipediaactor

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/lib/pq"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"golang.org/x/text/unicode/norm"

	"github.com/metatube-community/metatube-sdk-go/common/parser"
	"github.com/metatube-community/metatube-sdk-go/model"
	mt "github.com/metatube-community/metatube-sdk-go/provider"
	"github.com/strange-dragon/metaX/provider/httpretry"
)

const (
	// ActorProviderName is the provider name exposed by the MetaTube API.
	ActorProviderName = "WikipediaActor"
	// Priority places the biography source after the dedicated profile sites.
	Priority             = 1000 - 40
	defaultWikidataURL   = "https://www.wikidata.org"
	defaultTimeout       = 20 * time.Second
	maxPageBytes         = 8 << 20
	searchCandidateLimit = 5
	overviewMaxChars     = 1500
)

const (
	searchLanguages = "ja,zh,en"
	wikiLanguageJA  = "ja"
	wikiLanguageZH  = "zh"
	wikiLanguageEN  = "en"
)

var ErrActorNotFound = errors.New("wikipedia actor: actor not found")

var (
	_ mt.ActorProvider        = (*Actor)(nil)
	_ mt.ActorSearcher        = (*Actor)(nil)
	_ mt.ProxySetter          = (*Actor)(nil)
	_ mt.RequestTimeoutSetter = (*Actor)(nil)
)

// Actor resolves Wikidata entities and uses Wikipedia for the biography text.
// Wikidata is the identity source: wbsearchentities is only a fuzzy index, so
// every candidate must still match an exact label/alias and carry an AV
// occupation or description before it can be returned.
type Actor struct {
	wikidataURL *url.URL
	wikiURLs    map[string]*url.URL

	clientMu sync.RWMutex
	client   *http.Client
	timeout  time.Duration
	proxyURL string

	priorityMu sync.RWMutex
	priority   float64
}

func New() *Actor {
	wikidataURL, _ := url.Parse(defaultWikidataURL)
	wikiURLs := map[string]*url.URL{
		wikiLanguageJA: mustParse("https://ja.wikipedia.org"),
		wikiLanguageZH: mustParse("https://zh.wikipedia.org"),
		wikiLanguageEN: mustParse("https://en.wikipedia.org"),
	}
	p := &Actor{
		wikidataURL: wikidataURL,
		wikiURLs:    wikiURLs,
		timeout:     defaultTimeout,
		priority:    Priority,
	}
	p.rebuildClientLocked()
	return p
}

func mustParse(raw string) *url.URL {
	u, _ := url.Parse(raw)
	return u
}

func newForTest(wikidata string, wikis map[string]string, client *http.Client) *Actor {
	p := &Actor{
		wikidataURL: mustParse(wikidata),
		wikiURLs:    make(map[string]*url.URL, len(wikis)),
		timeout:     defaultTimeout,
		priority:    Priority,
	}
	for lang, base := range wikis {
		p.wikiURLs[lang] = mustParse(base)
	}
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

func (p *Actor) Name() string { return ActorProviderName }

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
	u := *p.wikidataURL
	return &u
}

func (p *Actor) NormalizeActorID(id string) string {
	id = strings.TrimSpace(norm.NFKC.String(id))
	upper := strings.ToUpper(id)
	if len(upper) > 1 && upper[0] == 'Q' {
		allDigits := true
		for _, r := range upper[1:] {
			if r < '0' || r > '9' {
				allDigits = false
				break
			}
		}
		if allDigits {
			return upper
		}
	}
	return id
}

func (p *Actor) ParseActorIDFromURL(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("wikipedia actor: parse URL: %w", err)
	}
	if !p.validOrigin(u, p.wikidataURL) || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("wikipedia actor: expected canonical Wikidata entity URL")
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) == 0 || (parts[0] != "wiki" && parts[0] != "entity") {
		return "", fmt.Errorf("wikipedia actor: invalid entity path %q", u.Path)
	}
	id := p.NormalizeActorID(parts[len(parts)-1])
	if !strings.HasPrefix(id, "Q") || len(id) < 2 {
		return "", fmt.Errorf("wikipedia actor: invalid entity ID %q", parts[len(parts)-1])
	}
	return id, nil
}

func (p *Actor) actorURL(id string) string {
	u := *p.wikidataURL
	u.Path = strings.TrimRight(u.Path, "/") + "/wiki/" + id
	u.RawQuery, u.Fragment = "", ""
	return u.String()
}

func (p *Actor) GetActorInfoByID(id string) (*model.ActorInfo, error) {
	id = p.NormalizeActorID(id)
	if !strings.HasPrefix(id, "Q") || !allDigits(id[1:]) {
		return nil, fmt.Errorf("wikipedia actor: invalid actor ID %q", id)
	}
	entity, err := p.entityData(id)
	if err != nil {
		return nil, err
	}
	if entity == nil || !isAVEntity(entity) {
		return nil, fmt.Errorf("%w: %s", ErrActorNotFound, id)
	}
	return p.buildActorInfo(id, entity)
}

func (p *Actor) GetActorInfoByURL(rawURL string) (*model.ActorInfo, error) {
	id, err := p.ParseActorIDFromURL(rawURL)
	if err != nil {
		return nil, err
	}
	return p.GetActorInfoByID(id)
}

func (p *Actor) SearchActor(keyword string) ([]*model.ActorSearchResult, error) {
	keyword = strings.TrimSpace(norm.NFKC.String(keyword))
	if keyword == "" {
		return nil, nil
	}
	candidates, err := p.searchEntities(keyword)
	if err != nil {
		return nil, err
	}
	for _, candidate := range candidates {
		entity, err := p.entityData(candidate.id)
		if err != nil || entity == nil {
			continue
		}
		if !isAVEntity(entity) || !nameMatches(entity, keyword) {
			continue
		}
		info, err := p.buildActorInfo(candidate.id, entity)
		if err != nil {
			continue
		}
		return []*model.ActorSearchResult{info.ToSearchResult()}, nil
	}
	return nil, nil
}

type searchCandidate struct {
	id          string
	description string
}

func (p *Actor) searchEntities(keyword string) ([]searchCandidate, error) {
	seen := make(map[string]struct{})
	var out []searchCandidate
	var lastErr error
	for _, lang := range []string{"ja", "zh", "en"} {
		u := *p.wikidataURL
		u.Path = strings.TrimRight(u.Path, "/") + "/w/api.php"
		query := u.Query()
		query.Set("action", "wbsearchentities")
		query.Set("search", keyword)
		query.Set("language", lang)
		query.Set("uselang", lang)
		query.Set("limit", fmt.Sprintf("%d", searchCandidateLimit))
		query.Set("format", "json")
		u.RawQuery = query.Encode()

		body, err := p.get(u.String(), "application/json")
		if err != nil {
			lastErr = err
			continue
		}
		var response struct {
			Search []struct {
				ID          string `json:"id"`
				Description string `json:"description"`
			} `json:"search"`
		}
		if err := json.Unmarshal(body, &response); err != nil {
			lastErr = fmt.Errorf("wikipedia actor: decode search response: %w", err)
			continue
		}
		for _, item := range response.Search {
			if !strings.HasPrefix(item.ID, "Q") || !allDigits(item.ID[1:]) {
				continue
			}
			if _, ok := seen[item.ID]; ok {
				continue
			}
			if !matchAVKeyword(item.Description) {
				continue
			}
			seen[item.ID] = struct{}{}
			out = append(out, searchCandidate{id: item.ID, description: item.Description})
		}
	}
	if len(out) == 0 && lastErr != nil {
		return nil, lastErr
	}
	return out, nil
}

type entityTerm struct {
	Language string `json:"language"`
	Value    string `json:"value"`
}

type entityClaim struct {
	MainSnak struct {
		DataValue struct {
			Value json.RawMessage `json:"value"`
		} `json:"datavalue"`
	} `json:"mainsnak"`
}

type entity struct {
	Labels       map[string]entityTerm    `json:"labels"`
	Aliases      map[string][]entityTerm  `json:"aliases"`
	Descriptions map[string]entityTerm    `json:"descriptions"`
	Claims       map[string][]entityClaim `json:"claims"`
	Sitelinks    map[string]struct {
		Title string `json:"title"`
	} `json:"sitelinks"`
}

func (p *Actor) entityData(id string) (*entity, error) {
	u := *p.wikidataURL
	u.Path = strings.TrimRight(u.Path, "/") + "/wiki/Special:EntityData/" + id + ".json"
	u.RawQuery, u.Fragment = "", ""
	body, err := p.get(u.String(), "application/json")
	if err != nil {
		return nil, err
	}
	var response struct {
		Entities map[string]*entity `json:"entities"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("wikipedia actor: decode entity %s: %w", id, err)
	}
	return response.Entities[id], nil
}

func (p *Actor) buildActorInfo(id string, e *entity) (*model.ActorInfo, error) {
	name := preferLabel(e.Labels)
	if name == "" {
		return nil, fmt.Errorf("%w: %s has no label", ErrActorNotFound, id)
	}
	aliases := make([]string, 0)
	for _, lang := range []string{"ja", "zh", "zh-cn", "zh-tw", "en"} {
		if label := e.Labels[lang].Value; label != "" && label != name {
			aliases = appendUnique(aliases, label)
		}
	}
	info := &model.ActorInfo{
		ID:       id,
		Name:     name,
		Provider: p.Name(),
		Homepage: p.actorURL(id),
		Aliases:  pq.StringArray(aliases),
	}
	if birthday := claimTime(e, "P569"); birthday != "" {
		info.Birthday = parser.ParseDate(birthday)
	}
	if image := claimCommonsImage(e); image != "" {
		info.Images = pq.StringArray{image}
	}
	if page, err := p.wikipediaPage(e); err == nil && page != nil {
		info.Summary = page.overview
		if page.birthday != "" && time.Time(info.Birthday).IsZero() {
			info.Birthday = parser.ParseDate(page.birthday)
		}
	}
	return info, nil
}

type wikiPage struct {
	overview string
	birthday string
}

func (p *Actor) wikipediaPage(e *entity) (*wikiPage, error) {
	var lastErr error
	for _, lang := range []string{wikiLanguageZH, wikiLanguageJA, wikiLanguageEN} {
		base := p.wikiURLs[lang]
		link := e.Sitelinks[lang+"wiki"]
		if base == nil || link.Title == "" {
			continue
		}
		u := *base
		prefix := "wiki"
		if lang == wikiLanguageZH {
			prefix = "zh-cn"
		}
		u.Path = strings.TrimRight(u.Path, "/") + "/" + prefix + "/" +
			escapeWikiTitle(link.Title)
		u.RawQuery, u.Fragment = "", ""
		body, err := p.get(u.String(), "text/html")
		if err != nil {
			lastErr = err
			continue
		}
		page, err := parseWikiPage(body)
		if err != nil {
			lastErr = err
			continue
		}
		if page.overview != "" {
			return page, nil
		}
	}
	return nil, lastErr
}

func (p *Actor) get(rawURL, accept string) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("wikipedia actor: parse request URL: %w", err)
	}
	if !p.allowedOrigin(u) {
		return nil, fmt.Errorf("wikipedia actor: refusing request to unexpected origin %q", rawURL)
	}
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("wikipedia actor: create request: %w", err)
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("User-Agent", "metaX/1.0 (MetaTube native provider; actor biography)")
	resp, err := p.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("wikipedia actor: GET %s: %w", rawURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%w: %s", ErrActorNotFound, rawURL)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("wikipedia actor: GET %s: unexpected HTTP status %s", rawURL, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPageBytes+1))
	if err != nil {
		return nil, fmt.Errorf("wikipedia actor: read response: %w", err)
	}
	if len(body) > maxPageBytes {
		return nil, fmt.Errorf("wikipedia actor: response exceeds %d bytes", maxPageBytes)
	}
	return body, nil
}

func (p *Actor) allowedOrigin(u *url.URL) bool {
	if p.validOrigin(u, p.wikidataURL) {
		return true
	}
	for _, base := range p.wikiURLs {
		if p.validOrigin(u, base) {
			return true
		}
	}
	return false
}

func (p *Actor) validOrigin(u, base *url.URL) bool {
	if u == nil || base == nil || u.User != nil || u.Host == "" {
		return false
	}
	if !strings.EqualFold(u.Scheme, base.Scheme) {
		return false
	}
	if !strings.EqualFold(strings.TrimSuffix(u.Hostname(), "."), strings.TrimSuffix(base.Hostname(), ".")) {
		return false
	}
	return effectivePort(u) == effectivePort(base)
}

func (p *Actor) checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return fmt.Errorf("wikipedia actor: stopped after 10 redirects")
	}
	if !p.allowedOrigin(req.URL) {
		return fmt.Errorf("wikipedia actor: refusing cross-origin redirect to %q", req.URL)
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
			return fmt.Errorf("wikipedia actor: invalid proxy URL %q", proxyURL)
		}
		switch strings.ToLower(u.Scheme) {
		case "http", "https", "socks5":
		default:
			return fmt.Errorf("wikipedia actor: unsupported proxy scheme %q", u.Scheme)
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
	p.client = &http.Client{Transport: httpretry.WrapTransport(transport), Timeout: timeout, CheckRedirect: p.checkRedirect}
}

func (p *Actor) httpClient() *http.Client {
	p.clientMu.RLock()
	defer p.clientMu.RUnlock()
	return p.client
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

var (
	avKeywords = []string{
		"av idol", "av actress", "av actor", "adult actress", "adult actor",
		"adult model", "porn actress", "porn actor", "pornographic",
		"japanese idol", "gravure", "av女優", "av女优", "av男優", "av男优",
		"avアイドル", "av監督", "アダルト", "成人映画", "成人影片",
	}
	avIndustryRE = regexp.MustCompile(`(?i)\b(?:av|adult|porn|xxx)\b`)
	avRoleRE     = regexp.MustCompile(`(?i)\b(?:actress|actor|idol|model|star)\b`)
)

func matchAVKeyword(description string) bool {
	lower := strings.ToLower(description)
	for _, keyword := range avKeywords {
		if strings.Contains(lower, strings.ToLower(keyword)) {
			return true
		}
	}
	return avIndustryRE.MatchString(description) && avRoleRE.MatchString(description)
}

var avOccupations = map[string]struct{}{
	"Q1079215": {},
	"Q8380347": {},
	"Q488111":  {},
}

func isAVEntity(e *entity) bool {
	for _, description := range e.Descriptions {
		if matchAVKeyword(description.Value) {
			return true
		}
	}
	for _, claim := range e.Claims["P106"] {
		var value struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(claim.MainSnak.DataValue.Value, &value) != nil {
			continue
		}
		if _, ok := avOccupations[value.ID]; ok {
			return true
		}
	}
	return false
}

func nameMatches(e *entity, name string) bool {
	key := foldName(name)
	if key == "" {
		return false
	}
	for _, label := range e.Labels {
		if foldName(label.Value) == key {
			return true
		}
	}
	for _, aliases := range e.Aliases {
		for _, alias := range aliases {
			if foldName(alias.Value) == key {
				return true
			}
		}
	}
	return false
}

func foldName(value string) string {
	value = strings.Join(strings.Fields(norm.NFKC.String(value)), " ")
	if value == "" {
		return ""
	}
	return cases.Fold().String(value)
}

func preferLabel(labels map[string]entityTerm) string {
	for _, lang := range []string{"ja", "zh", "zh-cn", "zh-tw", "en"} {
		if label := labels[lang].Value; label != "" {
			return label
		}
	}
	for _, label := range labels {
		if label.Value != "" {
			return label.Value
		}
	}
	return ""
}

func claimTime(e *entity, property string) string {
	for _, claim := range e.Claims[property] {
		var value struct {
			Time string `json:"time"`
		}
		if json.Unmarshal(claim.MainSnak.DataValue.Value, &value) != nil {
			continue
		}
		if len(value.Time) >= 11 && value.Time[0] != '-' {
			date := value.Time[1:11]
			if _, err := time.Parse("2006-01-02", date); err == nil {
				return date
			}
		}
	}
	return ""
}

func claimCommonsImage(e *entity) string {
	for _, claim := range e.Claims["P18"] {
		var filename string
		if json.Unmarshal(claim.MainSnak.DataValue.Value, &filename) != nil {
			continue
		}
		filename = strings.TrimSpace(filename)
		if filename != "" {
			return "https://commons.wikimedia.org/wiki/Special:FilePath/" + url.PathEscape(filename)
		}
	}
	return ""
}

func allDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func escapeWikiTitle(title string) string {
	return strings.ReplaceAll(url.PathEscape(strings.ReplaceAll(title, " ", "_")), "%2F", "/")
}

var (
	citationRE   = regexp.MustCompile(`[\[［]\s*(?:\d+|注\s*\d*|注釈\s*\d*|要出典|要ページ番号|編集|編輯|edit)\s*[\]］]`)
	archiveRE    = regexp.MustCompile(`(?i)\s*(?:archived?\s+(?:from|at)\s+the\s+original|ウェイバックマシン|存(?:于|於)(?:互联网档案馆|互聯網檔案館|網際網路檔案館))[^。．.！!？?]*[。．.]?`)
	accessDateRE = regexp.MustCompile(`[（(〔]\s*[\d年月日./\-－—\s]{2,24}(?:閲覧|閱覽|阅读|閱讀)\s*[）)〕]`)
	spaceRE      = regexp.MustCompile(`\s+`)
)

func parseWikiPage(body []byte) (*wikiPage, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("wikipedia actor: parse HTML: %w", err)
	}
	root := doc.Find(".mw-parser-output").First()
	if root.Length() == 0 {
		return &wikiPage{}, nil
	}
	var blocks []string
	headings := make([]headingLevel, 0, 4)
	selected := true
	root.Find("h1,h2,h3,h4,h5,h6,p,ul,ol").Each(func(_ int, node *goquery.Selection) {
		tag := goquery.NodeName(node)
		if strings.HasPrefix(tag, "h") {
			title := cleanWikiText(node.Text())
			if title == "" {
				return
			}
			level := int(tag[1] - '0')
			for len(headings) > 0 && headings[len(headings)-1].level >= level {
				headings = headings[:len(headings)-1]
			}
			headings = append(headings, headingLevel{level: level, title: title})
			selected = false
			for _, heading := range headings {
				if isBiographySection(heading.title) {
					selected = true
					break
				}
			}
			return
		}
		if !selected || hasAncestor(node, "table", "figure") {
			return
		}
		if tag == "p" {
			if hasAncestor(node, "ul", "ol") {
				return
			}
			if text := cleanWikiText(node.Text()); text != "" {
				blocks = append(blocks, text)
			}
			return
		}
		if hasAncestor(node, "ul", "ol") {
			return
		}
		var items []string
		node.ChildrenFiltered("li").Each(func(_ int, item *goquery.Selection) {
			if text := cleanWikiText(item.Text()); text != "" {
				items = append(items, text)
			}
		})
		if len(items) > 0 {
			blocks = append(blocks, strings.Join(items, "\n"))
		}
	})
	return &wikiPage{overview: joinOverview(blocks), birthday: infoboxBirthday(root)}, nil
}

type headingLevel struct {
	level int
	title string
}

func hasAncestor(node *goquery.Selection, tags ...string) bool {
	for parent := node.Parent(); parent.Length() > 0; parent = parent.Parent() {
		tag := goquery.NodeName(parent)
		if tag == "body" {
			return false
		}
		for _, candidate := range tags {
			if tag == candidate {
				return true
			}
		}
	}
	return false
}

func isBiographySection(title string) bool {
	folded := strings.ToLower(cleanWikiText(title))
	for _, keyword := range []string{
		"人物", "简历", "簡歷", "简介", "簡介", "经历", "經歷", "履历", "履歷",
		"略历", "略歷", "来历", "來歷", "生平", "生涯", "背景", "私生活",
		"个人生活", "個人生活", "プロフィール", "生い立ち", "early life",
		"career", "biography", "personal life", "background",
	} {
		if strings.Contains(folded, keyword) {
			return true
		}
	}
	return false
}

func cleanWikiText(text string) string {
	text = citationRE.ReplaceAllString(text, "")
	text = accessDateRE.ReplaceAllString(text, "")
	text = archiveRE.ReplaceAllString(text, "")
	text = strings.ReplaceAll(text, "[編集]", "")
	text = strings.ReplaceAll(text, "[edit]", "")
	return strings.TrimSpace(spaceRE.ReplaceAllString(text, " "))
}

func joinOverview(blocks []string) string {
	var b strings.Builder
	for _, block := range blocks {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		if b.Len()+len(block) > overviewMaxChars {
			remaining := overviewMaxChars - b.Len()
			if remaining < 30 {
				break
			}
			block = clipSentence(block, remaining)
		}
		b.WriteString(block)
		if b.Len() >= overviewMaxChars {
			break
		}
	}
	return b.String()
}

func clipSentence(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	cut := string(runes[:limit])
	if i := strings.LastIndexAny(cut, "。．.！!？?…"); i >= 0 {
		return cut[:i+len("。")]
	}
	return strings.TrimSpace(cut)
}

var infoboxBirthdayRE = regexp.MustCompile(`(\d{4})年(\d{1,2})月(\d{1,2})日`)

func infoboxBirthday(root *goquery.Selection) string {
	var result string
	root.Find("table.infobox tr").EachWithBreak(func(_ int, row *goquery.Selection) bool {
		header := cleanWikiText(row.Find("th").First().Text())
		if header != "生年月日" && !strings.EqualFold(header, "Born") &&
			header != "出生" && header != "出生日期" {
			return true
		}
		match := infoboxBirthdayRE.FindStringSubmatch(row.Find("td").First().Text())
		if len(match) == 4 {
			result = fmt.Sprintf("%s-%02s-%02s", match[1], match[2], match[3])
			return false
		}
		return true
	})
	return result
}

func init() { mt.Register(ActorProviderName, New) }
