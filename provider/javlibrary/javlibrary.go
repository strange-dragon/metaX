// Copyright 2026 metaX contributors.
//
// Feature-level comparison reference: https://github.com/sqzw-x/amane.
// Native Go implementation for the metaX Provider API; no Amane source
// files or test fixtures are vendored. See ../../NOTICE.md for attribution.
package javlibrary

import (
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

	"github.com/strange-dragon/metaX/internal/antibot"

	"github.com/PuerkitoBio/goquery"
	fhttp "github.com/bogdanfinn/fhttp"
	fhttpcookiejar "github.com/bogdanfinn/fhttp/cookiejar"
	tlsclient "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
	"github.com/lib/pq"
	"golang.org/x/net/publicsuffix"
	"golang.org/x/text/language"
	"golang.org/x/text/unicode/norm"

	"github.com/strange-dragon/metaX/common/parser"
	"github.com/strange-dragon/metaX/model"
	mt "github.com/strange-dragon/metaX/provider"
	"github.com/strange-dragon/metaX/provider/httpretry"
)

const (
	Name           = "JavLibrary"
	Priority       = 1000 - 25
	defaultURL     = "https://www.f101w.com"
	defaultTimeout = 20 * time.Second
	maxPageBytes   = 8 << 20
)

var (
	ErrMovieNotFound                          = errors.New("javlibrary: movie not found")
	numberTokenRE                             = regexp.MustCompile(`(?i)^[a-z0-9]+(?:[-_.][a-z0-9]+)*$`)
	runtimeRE                                 = regexp.MustCompile(`\d+`)
	scoreRE                                   = regexp.MustCompile(`\(([\d.]+)\)`)
	detailSlugRE                              = regexp.MustCompile(`^/(?:cn|en|ja)/([A-Za-z0-9_-]+)\.html$`)
	opaqueSlugRE                              = regexp.MustCompile(`[a-z]`)
	opaqueSlugDigitRE                         = regexp.MustCompile(`[0-9]`)
	_                 mt.MovieProvider        = (*JavLibrary)(nil)
	_                 mt.MovieSearcher        = (*JavLibrary)(nil)
	_                 mt.Fetcher              = (*JavLibrary)(nil)
	_                 mt.ConfigSetter         = (*JavLibrary)(nil)
	_                 mt.ProxySetter          = (*JavLibrary)(nil)
	_                 mt.RequestTimeoutSetter = (*JavLibrary)(nil)
)

type JavLibrary struct {
	baseURL    *url.URL
	cookieJar  fhttp.CookieJar
	clientMu   sync.RWMutex
	client     *http.Client
	timeout    time.Duration
	proxyURL   string
	cookies    string
	browserMu  sync.Mutex
	fetcher    *antibot.Fetcher
	priorityMu sync.RWMutex
	priority   float64
}

func New() *JavLibrary {
	u, _ := url.Parse(defaultURL)
	p := &JavLibrary{baseURL: u, timeout: defaultTimeout, priority: Priority, cookieJar: newFingerprintCookieJar()}
	p.rebuildClientLocked()
	return p
}

func newForTest(base string, client *http.Client) *JavLibrary {
	u, _ := url.Parse(base)
	p := &JavLibrary{baseURL: u, timeout: defaultTimeout, priority: Priority, cookieJar: newFingerprintCookieJar()}
	if client != nil {
		c := *client
		c.CheckRedirect = p.checkRedirect
		if c.Timeout <= 0 {
			c.Timeout = defaultTimeout
		}
		p.client = &c
	} else {
		p.rebuildClientLocked()
	}
	return p
}

func newFingerprintCookieJar() fhttp.CookieJar {
	jar, err := fhttpcookiejar.New(&fhttpcookiejar.Options{PublicSuffixList: publicsuffix.List})
	if err != nil {
		return nil
	}
	return jar
}

func (p *JavLibrary) Name() string { return Name }
func (p *JavLibrary) Priority() float64 {
	p.priorityMu.RLock()
	defer p.priorityMu.RUnlock()
	return p.priority
}
func (p *JavLibrary) SetPriority(v float64) {
	p.priorityMu.Lock()
	defer p.priorityMu.Unlock()
	p.priority = v
}
func (p *JavLibrary) Language() language.Tag { return language.English }
func (p *JavLibrary) URL() *url.URL          { u := *p.baseURL; return &u }

// SetConfig allows deployments to select the current JavLibrary language-site
// origin when the canonical www.javlibrary.com hostname is blocked or moved.
// Do not use javlib.com here: it is an address portal, not the scraper site.
func (p *JavLibrary) SetConfig(config mt.Config) error {
	if config == nil {
		return nil
	}
	if config.Has("base_url") {
		raw, err := config.GetString("base_url")
		if err != nil {
			return fmt.Errorf("javlibrary: read base_url config: %w", err)
		}
		u, err := url.Parse(strings.TrimSpace(raw))
		if err != nil || !strings.EqualFold(u.Scheme, "https") || u.Host == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("javlibrary: base_url must be an HTTPS origin without path, query, or userinfo")
		}
		u.Path = ""
		u.RawPath = ""
		p.clientMu.Lock()
		p.baseURL = u
		p.clientMu.Unlock()
	}
	var cookieKey string
	switch {
	case config.Has("cookies"):
		cookieKey = "cookies"
	case config.Has("cookie"):
		cookieKey = "cookie"
	}
	if cookieKey != "" {
		raw, err := config.GetString(cookieKey)
		if err != nil {
			return fmt.Errorf("javlibrary: read cookies config: %w", err)
		}
		raw = strings.TrimSpace(raw)
		if strings.ContainsAny(raw, "\r\n") {
			return errors.New("javlibrary: cookies must not contain CR or LF")
		}
		request, _ := http.NewRequest(http.MethodGet, p.baseURL.String(), nil)
		request.Header.Set("Cookie", raw)
		parsed := request.Cookies()
		if raw != "" && len(parsed) == 0 {
			return errors.New("javlibrary: invalid cookies header")
		}
		cookies := make([]*fhttp.Cookie, 0, len(parsed))
		for _, cookie := range parsed {
			cookies = append(cookies, &fhttp.Cookie{Name: cookie.Name, Value: cookie.Value, Path: "/"})
		}
		p.clientMu.Lock()
		p.cookieJar.SetCookies(p.baseURL, cookies)
		p.cookies = raw
		p.clientMu.Unlock()
	}
	return nil
}

func (p *JavLibrary) NormalizeMovieID(id string) string {
	return strings.TrimSpace(norm.NFKC.String(id))
}
func (p *JavLibrary) NormalizeMovieKeyword(keyword string) string {
	return strings.ToUpper(strings.TrimSpace(norm.NFKC.String(keyword)))
}

func (p *JavLibrary) validOrigin(u *url.URL) bool {
	if u == nil || u.User != nil || u.Host == "" || !strings.EqualFold(u.Scheme, p.baseURL.Scheme) {
		return false
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	base := strings.TrimSuffix(strings.ToLower(p.baseURL.Hostname()), ".")
	if host != base && !(base == "www.javlibrary.com" && host == "javlibrary.com") {
		return false
	}
	return effectivePort(u) == effectivePort(p.baseURL)
}
func effectivePort(u *url.URL) string {
	if v := u.Port(); v != "" {
		return v
	}
	if strings.EqualFold(u.Scheme, "https") {
		return "443"
	}
	if strings.EqualFold(u.Scheme, "http") {
		return "80"
	}
	return ""
}
func (p *JavLibrary) checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return fmt.Errorf("javlibrary: stopped after 10 redirects")
	}
	if !p.validOrigin(req.URL) {
		return fmt.Errorf("javlibrary: refusing cross-origin redirect to %q", req.URL)
	}
	return nil
}
func (p *JavLibrary) SetRequestTimeout(v time.Duration) {
	p.clientMu.Lock()
	defer p.clientMu.Unlock()
	if v <= 0 {
		v = defaultTimeout
	}
	p.timeout = v
	p.rebuildClientLocked()
}
func (p *JavLibrary) SetProxy(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw != "" {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("javlibrary: invalid proxy URL %q", raw)
		}
		switch strings.ToLower(u.Scheme) {
		case "http", "https", "socks5":
		default:
			return fmt.Errorf("javlibrary: unsupported proxy scheme %q", u.Scheme)
		}
	}
	p.clientMu.Lock()
	defer p.clientMu.Unlock()
	p.proxyURL = raw
	p.rebuildClientLocked()
	return nil
}
func (p *JavLibrary) rebuildClientLocked() {
	var tr *http.Transport
	if base, ok := http.DefaultTransport.(*http.Transport); ok {
		tr = base.Clone()
	} else {
		tr = &http.Transport{Proxy: http.ProxyFromEnvironment}
	}
	tr.Proxy = http.ProxyFromEnvironment
	if p.proxyURL != "" {
		if u, e := url.Parse(p.proxyURL); e == nil {
			tr.Proxy = http.ProxyURL(u)
		}
	}
	timeout := p.timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	p.client = &http.Client{Transport: httpretry.WrapTransport(tr), Timeout: timeout, CheckRedirect: p.checkRedirect}
}
func (p *JavLibrary) httpClient() *http.Client {
	p.clientMu.RLock()
	defer p.clientMu.RUnlock()
	return p.client
}
func (p *JavLibrary) Fetch(raw string) (*http.Response, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("javlibrary: parse URL: %w", err)
	}
	if !p.validOrigin(u) {
		return nil, fmt.Errorf("javlibrary: refusing request to unexpected origin %q", raw)
	}
	req, err := http.NewRequest(http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:128.0) Gecko/20100101 Firefox/128.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9,zh-CN;q=0.7")
	req.Header.Set("Referer", p.baseURL.String()+"/")
	// JavLibrary rejects generic Go TLS fingerprints. Use a browser-profiled
	// client for the first request, then rotate profiles only when the origin
	// returns 403/406, matching Amane's request strategy.
	deadline := time.Now().Add(p.totalFetchBudget())
	resp, err := p.fetchWithFingerprintRotation(req, deadline)
	if err != nil {
		if isCloudflareChallenge(err) {
			resp, browserErr := p.fetchWithChromium(req, deadline)
			if browserErr == nil {
				return resp, nil
			}
			return nil, fmt.Errorf("javlibrary: GET %s blocked by Cloudflare; Chromium fallback failed: %w (initial Go request: %v)", raw, browserErr, err)
		}
		return nil, fmt.Errorf("javlibrary: GET %s with browser fingerprints: %w", raw, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound {
			return nil, ErrMovieNotFound
		}
		return nil, fmt.Errorf("javlibrary: unexpected HTTP status %s", resp.Status)
	}
	return resp, nil
}

func isCloudflareChallenge(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "just a moment") || strings.Contains(text, "请稍候") || strings.Contains(text, "cf-chl-")
}

func isCloudflareChallengeOrigin(u *url.URL) bool {
	return u != nil && u.Scheme == "https" && u.Port() == "" && u.User == nil && strings.EqualFold(u.Hostname(), "challenges.cloudflare.com")
}

func parseCookieHeader(raw string) []*http.Cookie {
	if strings.TrimSpace(raw) == "" || strings.ContainsAny(raw, "\r\n") {
		return nil
	}
	req, err := http.NewRequest(http.MethodGet, "https://localhost/", nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Cookie", raw)
	return req.Cookies()
}

// Close releases the Chromium fallback. A long-lived server would otherwise
// keep a headless browser and its temporary profile alive after the last
// scrape, which on a developer machine looks like a stray Chrome process.
func (p *JavLibrary) Close() error {
	p.browserMu.Lock()
	defer p.browserMu.Unlock()
	if p.fetcher != nil {
		p.fetcher.Close()
		p.fetcher = nil
	}
	return nil
}

// fingerprintProfile pairs a TLS ClientHello profile with a matching browser User-Agent.
// Browser profiles are tried in order. A 403/406 triggers a one-second pause and the
// next profile, while a shared cookie jar preserves upstream challenge cookies.
type fingerprintProfile struct {
	name      string
	profile   profiles.ClientProfile
	userAgent string
}

// Keep the newest available Chrome profiles first: the live JavLibrary browser
// session on 2026-10-27 negotiated with Chromium 152, while older profiles were
// all challenged from this environment.
var javLibraryFingerprintProfiles = []fingerprintProfile{
	{name: "Chrome 146", profile: profiles.Chrome_146, userAgent: "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36"},
	{name: "Chrome 131", profile: profiles.Chrome_131, userAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"},
	{name: "Chrome 133", profile: profiles.Chrome_133, userAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36"},
	{name: "Chrome 144", profile: profiles.Chrome_144, userAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/144.0.0.0 Safari/537.36"},
	{name: "Firefox 135", profile: profiles.Firefox_135, userAgent: "Mozilla/5.0 (X11; Linux x86_64; rv:135.0) Gecko/20100101 Firefox/135.0"},
}

// fetchWithChromium renders the request in the shared headless browser.
//
// The provider keeps its own TLS fingerprint rotation for the cheap path, but
// the browser fallback is the shared implementation. A private copy used to live
// here and drifted out of date: it opened a visible window and overrode the UA
// through CDP, both of which make Cloudflare fail the challenge forever.
// totalFetchBudget is the wall clock one page fetch may take. The provider
// timeout bounds a single attempt, but a fetch may legitimately need several:
// four TLS profiles and then a Chromium navigation. Unbounded, a handshake that
// hangs instead of failing turns one request into a multi-minute hang — on a
// 0.1 vCPU Koyeb instance four profiles at the 20s timeout had not even reached
// the browser after 90 seconds.
func (p *JavLibrary) totalFetchBudget() time.Duration {
	p.clientMu.RLock()
	per := p.timeout
	p.clientMu.RUnlock()
	if per <= 0 {
		per = defaultTimeout
	}
	return max(2*per, 30*time.Second)
}

// remainingSleep sleeps until at most d has passed, but never past the
// deadline. It reports whether the deadline is still ahead.
func remainingSleep(deadline time.Time, d time.Duration) bool {
	left := time.Until(deadline)
	if left <= 0 {
		return false
	}
	time.Sleep(min(d, left))
	return time.Until(deadline) > 0
}

func (p *JavLibrary) fetchWithChromium(original *http.Request, deadline time.Time) (*http.Response, error) {
	p.clientMu.RLock()
	proxyURL, cookies := p.proxyURL, p.cookies
	p.clientMu.RUnlock()
	// Chromium is the last strategy and the only one that can clear a
	// challenge, so it keeps whatever is left of the budget. A five second
	// floor covers the process start on a slow instance.
	timeout := max(time.Until(deadline), 5*time.Second)
	p.browserMu.Lock()
	defer p.browserMu.Unlock()
	if p.fetcher == nil {
		p.fetcher = antibot.New(antibot.Options{
			Timeout:  timeout,
			ProxyURL: proxyURL,
			Cookies:  cookies,
			AllowURL: p.validOrigin,
		})
	} else {
		p.fetcher.SetOptions(func(opts *antibot.Options) {
			opts.Timeout = timeout
			opts.ProxyURL = proxyURL
			opts.Cookies = cookies
		})
	}
	req := original.Clone(original.Context())
	req.Header.Set("Accept-Language", antibot.BrowserLanguage())
	return p.fetcher.Do(req)
}

func (p *JavLibrary) fetchWithFingerprintRotation(original *http.Request, deadline time.Time) (*http.Response, error) {
	p.clientMu.RLock()
	timeout, proxyURL := p.timeout, p.proxyURL
	p.clientMu.RUnlock()
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	if proxyURL == "" {
		if envProxy, err := http.ProxyFromEnvironment(original); err == nil && envProxy != nil {
			proxyURL = envProxy.String()
		}
	}

	var lastError error
	var failures []string
	profiles := javLibraryFingerprintProfiles
	for i, fingerprint := range profiles {
		if i > 0 && !remainingSleep(deadline, time.Second) {
			failures = append(failures, "deadline reached before the remaining TLS profiles were tried")
			break
		}
		// Split the fingerprint stage's share of the remaining budget across
		// the profiles that are left, so one hanging handshake cannot consume
		// the time the Chromium fallback needs.
		attemptTimeout := timeout
		if left := time.Until(deadline); left > 0 {
			attemptTimeout = min(attemptTimeout, time.Duration(float64(left)*0.5/float64(len(profiles)-i)))
		}
		if attemptTimeout < time.Second {
			failures = append(failures, "deadline reached before the remaining TLS profiles were tried")
			break
		}
		options := []tlsclient.HttpClientOption{
			tlsclient.WithTimeoutSeconds(max(1, int(attemptTimeout.Seconds()))),
			tlsclient.WithClientProfile(fingerprint.profile),
			tlsclient.WithNotFollowRedirects(),
		}
		if proxyURL != "" {
			options = append(options, tlsclient.WithProxyUrl(proxyURL))
		}
		if p.cookieJar != nil {
			options = append(options, tlsclient.WithCookieJar(p.cookieJar))
		}
		client, err := tlsclient.NewHttpClient(tlsclient.NewNoopLogger(), options...)
		if err != nil {
			lastError = err
			failures = append(failures, fingerprint.name+": "+err.Error())
			continue
		}
		resp, err := p.doFingerprintRequest(client, original, fingerprint.userAgent)
		client.CloseIdleConnections()
		if err != nil {
			lastError = err
			failures = append(failures, fingerprint.name+": "+err.Error())
			continue
		}
		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotAcceptable {
			detail := fingerprintFailureDetail(fingerprint.name, resp)
			failures = append(failures, detail)
			_ = resp.Body.Close()
			// Every remaining profile answers a JavaScript challenge with the
			// same interstitial, so rotating past it only eats the budget the
			// Chromium fallback needs.
			if isChallengeDetail(detail) {
				break
			}
			continue
		}
		return resp, nil
	}
	if len(failures) > 0 {
		detail := strings.Join(failures, "; ")
		if isChallengeDetail(detail) {
			return nil, fmt.Errorf("Cloudflare JavaScript challenge rejected every browser TLS profile; Go does not execute the challenge page. Configure a fresh COOKIES value containing cf_clearance from the same egress IP, or use an egress network that passes the challenge (%s)", detail)
		}
		return nil, fmt.Errorf("browser fingerprint attempts exhausted: %s", detail)
	}
	if lastError == nil {
		lastError = errors.New("all browser fingerprint attempts failed")
	}
	return nil, lastError
}

// isChallengeDetail reports whether a recorded failure is a JavaScript
// challenge page rather than a real rejection. Only the former is immune to TLS
// profile rotation, and only the former is worth handing to the browser.
func isChallengeDetail(detail string) bool {
	lower := strings.ToLower(detail)
	for _, marker := range []string{"just a moment", "cf-chl-", "challenge-platform", "checking your browser"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func fingerprintFailureDetail(name string, resp *http.Response) string {
	body, err := io.ReadAll(io.LimitReader(resp.Body, 512))
	bodyText := strings.Join(strings.Fields(string(body)), " ")
	if len(bodyText) > 240 {
		bodyText = bodyText[:240] + "…"
	}
	if err != nil {
		bodyText = fmt.Sprintf("<body read failed: %v>", err)
	}
	return fmt.Sprintf("%s=%s server=%q content-type=%q body=%q", name, resp.Status, resp.Header.Get("Server"), resp.Header.Get("Content-Type"), bodyText)
}

func (p *JavLibrary) doFingerprintRequest(client tlsclient.HttpClient, original *http.Request, userAgent string) (*http.Response, error) {
	current := *original.URL
	previous := ""
	for redirects := 0; ; redirects++ {
		req, err := fhttp.NewRequestWithContext(original.Context(), original.Method, current.String(), nil)
		if err != nil {
			return nil, err
		}
		for key, values := range original.Header {
			req.Header[key] = append([]string(nil), values...)
		}
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("Upgrade-Insecure-Requests", "1")
		if previous != "" {
			req.Header.Set("Referer", previous)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		isRedirect := resp.StatusCode == http.StatusMovedPermanently ||
			resp.StatusCode == http.StatusFound ||
			resp.StatusCode == http.StatusSeeOther ||
			resp.StatusCode == http.StatusTemporaryRedirect ||
			resp.StatusCode == http.StatusPermanentRedirect
		location := resp.Header.Get("Location")
		if !isRedirect || location == "" {
			return toStandardResponse(resp, original), nil
		}
		if redirects >= 10 {
			_ = resp.Body.Close()
			return nil, errors.New("stopped after 10 redirects")
		}
		reference, err := url.Parse(location)
		if err != nil {
			_ = resp.Body.Close()
			return nil, fmt.Errorf("parse redirect location: %w", err)
		}
		next := current.ResolveReference(reference)
		if !p.validOrigin(next) {
			_ = resp.Body.Close()
			return nil, fmt.Errorf("refusing cross-origin redirect to %q", next)
		}
		_ = resp.Body.Close()
		previous = current.String()
		current = *next
	}
}

func toStandardResponse(resp *fhttp.Response, request *http.Request) *http.Response {
	header := make(http.Header, len(resp.Header))
	for key, values := range resp.Header {
		header[key] = append([]string(nil), values...)
	}
	return &http.Response{
		Status: resp.Status, StatusCode: resp.StatusCode, Header: header, Body: resp.Body,
		ContentLength: resp.ContentLength, Request: request,
	}
}
func (p *JavLibrary) getHTML(raw string) (*goquery.Document, error) {
	resp, err := p.Fetch(raw)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPageBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxPageBytes {
		return nil, fmt.Errorf("javlibrary: response exceeds %d bytes", maxPageBytes)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("javlibrary: parse HTML: %w", err)
	}
	return doc, nil
}
func (p *JavLibrary) detailURL(id string) string {
	u := *p.baseURL
	if strings.HasSuffix(strings.ToLower(id), ".html") {
		u.Path = "/cn/" + strings.TrimPrefix(id, "/")
		u.RawQuery = ""
	} else if isOpaqueSlug(id) {
		u.Path = "/cn/" + id + ".html"
		u.RawQuery = ""
	} else {
		u.Path = "/cn/"
		q := url.Values{}
		q.Set("v", id)
		u.RawQuery = q.Encode()
	}
	u.Fragment = ""
	return u.String()
}
func (p *JavLibrary) ParseMovieIDFromURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("javlibrary: parse URL: %w", err)
	}
	if !p.validOrigin(u) {
		return "", fmt.Errorf("javlibrary: unexpected detail origin %q", raw)
	}
	id := ""
	switch {
	case u.Path == "/cn/" || u.Path == "/en/" || u.Path == "/ja/" || u.Path == "/":
		id = strings.TrimSpace(u.Query().Get("v"))
	case detailSlugRE.MatchString(u.Path):
		id = detailSlugRE.FindStringSubmatch(u.Path)[1]
	default:
		return "", fmt.Errorf("javlibrary: unsupported detail path %q", u.Path)
	}
	if id == "" || !numberTokenRE.MatchString(id) {
		return "", fmt.Errorf("javlibrary: missing or invalid v parameter")
	}
	return id, nil
}

func isOpaqueSlug(id string) bool {
	if id == "" || strings.ContainsAny(id, "/\\?#") || !numberTokenRE.MatchString(id) {
		return false
	}
	return id == strings.ToLower(id) && opaqueSlugRE.MatchString(id) && opaqueSlugDigitRE.MatchString(id)
}

func (p *JavLibrary) SearchMovie(keyword string) ([]*model.MovieSearchResult, error) {
	keyword = p.NormalizeMovieKeyword(keyword)
	if keyword == "" {
		return nil, nil
	}
	u := *p.baseURL
	u.Path = "/cn/vl_searchbyid.php"
	q := url.Values{}
	q.Set("keyword", keyword)
	u.RawQuery = q.Encode()
	doc, err := p.getHTML(u.String())
	if err != nil {
		return nil, err
	}
	var out []*model.MovieSearchResult
	seen := map[string]bool{}
	doc.Find("div.video").Each(func(_ int, card *goquery.Selection) {
		a := card.Find("a").First()
		href, ok := a.Attr("href")
		if !ok {
			return
		}
		ref, e := url.Parse(strings.TrimSpace(href))
		if e != nil {
			return
		}
		target := u.ResolveReference(ref)
		id, e := p.ParseMovieIDFromURL(target.String())
		if e != nil || seen[id] {
			return
		}
		title, _ := a.Attr("title")
		title = cleanText(title)
		number := findNumber(title, keyword)
		if number == "" {
			number = findNumber(cleanText(card.Find(".id").First().Text()), keyword)
		}
		if number == "" {
			return
		}
		thumb := absoluteURL(p.baseURL, firstAttr(card.Find("img").First(), "src", "data-src"))
		display := strings.TrimSpace(strings.Replace(title, number, "", 1))
		display = strings.Trim(display, " -—:：")
		item := &model.MovieSearchResult{ID: id, Number: number, Title: display, Provider: p.Name(), Homepage: target.String(), ThumbURL: thumb}
		if item.IsValid() {
			seen[id] = true
			out = append(out, item)
		}
	})
	return out, nil
}
func findNumber(title, query string) string {
	for _, field := range strings.Fields(strings.TrimSpace(title)) {
		token := strings.Trim(field, "()[]{}，。:：")
		if normalizeNumber(token) == normalizeNumber(query) {
			return token
		}
	}
	return ""
}
func normalizeNumber(s string) string {
	s = strings.ToUpper(norm.NFKC.String(strings.TrimSpace(s)))
	return strings.NewReplacer("-", "", "_", "", " ", "", ".", "", "\t", "").Replace(s)
}
func (p *JavLibrary) GetMovieInfoByID(id string) (*model.MovieInfo, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("javlibrary: empty movie ID")
	}
	if u, e := url.Parse(id); e == nil && u.IsAbs() {
		return p.GetMovieInfoByURL(id)
	}
	if numberTokenRE.MatchString(id) && !strings.ContainsAny(id, "/ ?#") && !isOpaqueSlug(id) {
		results, err := p.SearchMovie(id)
		if err != nil {
			return nil, err
		}
		for _, r := range results {
			if normalizeNumber(r.Number) == normalizeNumber(id) {
				return p.GetMovieInfoByURL(r.Homepage)
			}
		}
	}
	return p.GetMovieInfoByURL(p.detailURL(id))
}
func (p *JavLibrary) GetMovieInfoByURL(raw string) (*model.MovieInfo, error) {
	id, err := p.ParseMovieIDFromURL(raw)
	if err != nil {
		return nil, err
	}
	doc, err := p.getHTML(raw)
	if err != nil {
		return nil, err
	}
	number := cleanText(doc.Find("#video_id td.text").First().Text())
	if number == "" {
		return nil, ErrMovieNotFound
	}
	title := cleanText(doc.Find("h3.post-title a").First().Text())
	if title == "" {
		return nil, fmt.Errorf("javlibrary: detail page %s has no title", raw)
	}
	release := cleanText(doc.Find("#video_date td.text").First().Text())
	runtimeText := cleanText(doc.Find("#video_length td.text").First().Text())
	runtime := 0
	if m := runtimeRE.FindString(runtimeText); m != "" {
		runtime, _ = strconv.Atoi(m)
	}
	director := cleanText(doc.Find("#video_director td.text a").First().Text())
	studio := cleanText(doc.Find("#video_maker td.text a").First().Text())
	label := cleanText(doc.Find("#video_label td.text a").First().Text())
	var actors, genres []string
	doc.Find("span.cast span.star a").Each(func(_ int, s *goquery.Selection) {
		if v := cleanText(s.Text()); v != "" {
			actors = appendUnique(actors, v)
		}
	})
	doc.Find("#video_genres td.text span.genre a").Each(func(_ int, s *goquery.Selection) {
		if v := cleanText(s.Text()); v != "" {
			genres = appendUnique(genres, v)
		}
	})
	cover := absoluteURL(p.baseURL, firstAttr(doc.Find("#video_jacket_img").First(), "src", "data-src"))
	if cover == "" {
		cover = absoluteURL(p.baseURL, attrText(doc.Find("meta[property='og:image']").First(), "content"))
	}
	if cover == "" {
		return nil, fmt.Errorf("javlibrary: detail page %s has no cover image", raw)
	}
	thumb := cover
	if strings.HasSuffix(strings.ToLower(cover), "pl.jpg") {
		thumb = cover[:len(cover)-len("pl.jpg")] + "ps.jpg"
	}
	var previewImages []string
	doc.Find(".previewthumbs a").Each(func(_ int, a *goquery.Selection) {
		image := absoluteURL(p.baseURL, firstAttr(a, "href"))
		if image != "" && (strings.HasPrefix(image, "https://") || strings.HasPrefix(image, "http://")) {
			previewImages = appendUnique(previewImages, image)
		}
	})
	scoreText := cleanText(doc.Find("span.score").First().Text())
	score := 0.0
	if m := scoreRE.FindStringSubmatch(scoreText); len(m) > 1 {
		score, _ = strconv.ParseFloat(m[1], 64)
	}
	info := &model.MovieInfo{ID: id, Number: number, Title: title, Provider: p.Name(), Homepage: raw, Maker: studio, Label: label, Director: director, Actors: pq.StringArray(actors), Genres: pq.StringArray(genres), CoverURL: cover, BigCoverURL: cover, ThumbURL: thumb, BigThumbURL: cover, PreviewImages: pq.StringArray(previewImages), Score: score, Runtime: runtime}
	if release != "" {
		info.ReleaseDate = parser.ParseDate(release)
	}
	return info, nil
}
func init() { mt.Register(Name, New) }
func cleanText(s string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(norm.NFKC.String(s))), " ")
}
func attrText(s *goquery.Selection, key string) string {
	v, _ := s.Attr(key)
	return strings.TrimSpace(v)
}
func firstAttr(s *goquery.Selection, keys ...string) string {
	for _, k := range keys {
		if v := attrText(s, k); v != "" {
			return v
		}
	}
	return ""
}
func absoluteURL(base *url.URL, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	ref, e := url.Parse(raw)
	if e != nil {
		return ""
	}
	return base.ResolveReference(ref).String()
}
func appendUnique(values []string, v string) []string {
	for _, old := range values {
		if old == v {
			return values
		}
	}
	return append(values, v)
}
