package engine

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/text/language"

	"github.com/strange-dragon/metaX/collection/maps"
	"github.com/strange-dragon/metaX/model"
	mt "github.com/strange-dragon/metaX/provider"
)

const (
	requestCookieMovieName        = "MetaXRequestCookieMovie"
	requestCookieUnsupportedName  = "MetaXRequestCookieUnsupported"
	requestCookieActorName        = "MetaXRequestCookieActor"
	requestCookieMovieBaseURL     = "https://request-cookie.invalid"
	requestCookieActorBaseURL     = "https://request-cookie-actor.invalid"
	requestCookieMovieDefaultInfo = "default"
)

type requestCookieMovieBase struct {
	name    string
	baseURL *url.URL
	cookies string
}

func (p *requestCookieMovieBase) Name() string         { return p.name }
func (*requestCookieMovieBase) Priority() float64      { return 1 }
func (*requestCookieMovieBase) SetPriority(float64)    {}
func (*requestCookieMovieBase) Language() language.Tag { return language.English }
func (p *requestCookieMovieBase) URL() *url.URL        { return p.baseURL }
func (*requestCookieMovieBase) NormalizeMovieID(id string) string {
	return strings.TrimSpace(id)
}
func (p *requestCookieMovieBase) ParseMovieIDFromURL(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	return strings.Trim(parsed.Path, "/"), nil
}
func (p *requestCookieMovieBase) GetMovieInfoByID(id string) (*model.MovieInfo, error) {
	title := p.cookies
	if title == "" {
		title = requestCookieMovieDefaultInfo
	}
	return &model.MovieInfo{
		ID:       id,
		Number:   "ABC-123",
		Title:    title,
		Provider: p.Name(),
		Homepage: requestCookieMovieBaseURL + "/v/" + id,
		CoverURL: requestCookieMovieBaseURL + "/cover.jpg",
	}, nil
}
func (p *requestCookieMovieBase) GetMovieInfoByURL(rawURL string) (*model.MovieInfo, error) {
	id, err := p.ParseMovieIDFromURL(rawURL)
	if err != nil {
		return nil, err
	}
	return p.GetMovieInfoByID(id)
}
func (*requestCookieMovieBase) NormalizeMovieKeyword(keyword string) string {
	return strings.TrimSpace(keyword)
}
func (p *requestCookieMovieBase) SearchMovie(keyword string) ([]*model.MovieSearchResult, error) {
	info, err := p.GetMovieInfoByID(keyword)
	if err != nil {
		return nil, err
	}
	return []*model.MovieSearchResult{info.ToSearchResult()}, nil
}

type requestCookieMovie struct {
	requestCookieMovieBase
}

func (p *requestCookieMovie) SetConfig(config mt.Config) error {
	if config == nil || !config.Has("cookies") {
		return nil
	}
	cookies, err := config.GetString("cookies")
	if err != nil {
		return err
	}
	p.cookies = cookies
	return nil
}

type requestCookieUnsupportedMovie struct {
	requestCookieMovieBase
}

func newRequestCookieTestEngine(providers ...mt.MovieProvider) *Engine {
	e := &Engine{
		movieProviders:       maps.NewCaseInsensitiveMap[mt.MovieProvider](),
		actorProviders:       maps.NewCaseInsensitiveMap[mt.ActorProvider](),
		movieProviderConfigs: maps.NewCaseInsensitiveMap[mt.Config](),
		actorProviderConfigs: maps.NewCaseInsensitiveMap[mt.Config](),
		movieHostProviders:   maps.NewCaseInsensitiveMap[[]mt.MovieProvider](),
		actorHostProviders:   maps.NewCaseInsensitiveMap[[]mt.ActorProvider](),
	}
	for _, provider := range providers {
		e.movieProviders.Set(provider.Name(), provider)
		host := provider.URL().Hostname()
		e.movieHostProviders.Set(host, append(e.movieHostProviders.GetOrDefault(host, nil), provider))
	}
	return e
}

func newRequestCookieMovie() *requestCookieMovie {
	baseURL, _ := url.Parse(requestCookieMovieBaseURL)
	return &requestCookieMovie{requestCookieMovieBase{
		name: requestCookieMovieName, baseURL: baseURL,
	}}
}

func newRequestCookieUnsupportedMovie() *requestCookieUnsupportedMovie {
	baseURL, _ := url.Parse(requestCookieMovieBaseURL)
	return &requestCookieUnsupportedMovie{requestCookieMovieBase{
		name: requestCookieUnsupportedName, baseURL: baseURL,
	}}
}

type requestCookieActor struct {
	baseURL *url.URL
	cookies string
}

func (*requestCookieActor) Name() string           { return requestCookieActorName }
func (*requestCookieActor) Priority() float64      { return 1 }
func (*requestCookieActor) SetPriority(float64)    {}
func (*requestCookieActor) Language() language.Tag { return language.English }
func (p *requestCookieActor) URL() *url.URL        { return p.baseURL }
func (*requestCookieActor) NormalizeActorID(id string) string {
	return strings.TrimSpace(id)
}
func (p *requestCookieActor) ParseActorIDFromURL(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	return strings.Trim(parsed.Path, "/"), nil
}
func (p *requestCookieActor) GetActorInfoByID(id string) (*model.ActorInfo, error) {
	name := p.cookies
	if name == "" {
		name = "default"
	}
	return &model.ActorInfo{
		ID:       id,
		Name:     name,
		Provider: p.Name(),
		Homepage: requestCookieActorBaseURL + "/actor/" + id,
	}, nil
}
func (p *requestCookieActor) GetActorInfoByURL(rawURL string) (*model.ActorInfo, error) {
	id, err := p.ParseActorIDFromURL(rawURL)
	if err != nil {
		return nil, err
	}
	return p.GetActorInfoByID(id)
}
func (p *requestCookieActor) SetConfig(config mt.Config) error {
	if config == nil || !config.Has("cookies") {
		return nil
	}
	cookies, err := config.GetString("cookies")
	if err != nil {
		return err
	}
	p.cookies = cookies
	return nil
}

func newRequestCookieActor() *requestCookieActor {
	baseURL, _ := url.Parse(requestCookieActorBaseURL)
	return &requestCookieActor{baseURL: baseURL}
}

func init() {
	mt.Register(requestCookieMovieName, newRequestCookieMovie)
	mt.Register(requestCookieUnsupportedName, newRequestCookieUnsupportedMovie)
	mt.Register(requestCookieActorName, newRequestCookieActor)
}

func TestWithRequestCookiesIsolatesEachRequest(t *testing.T) {
	original := newRequestCookieMovie()
	e := newRequestCookieTestEngine(original)

	first, err := e.WithRequestCookies(map[string]string{
		requestCookieMovieName: "session=first",
	})
	if err != nil {
		t.Fatalf("WithRequestCookies(first): %v", err)
	}
	second, err := e.WithRequestCookies(map[string]string{
		strings.ToLower(requestCookieMovieName): "session=second",
	})
	if err != nil {
		t.Fatalf("WithRequestCookies(second): %v", err)
	}

	firstInfo, err := first.MustGetMovieProviderByName(requestCookieMovieName).GetMovieInfoByID("one")
	if err != nil {
		t.Fatalf("first provider info: %v", err)
	}
	secondInfo, err := second.MustGetMovieProviderByName(requestCookieMovieName).GetMovieInfoByID("two")
	if err != nil {
		t.Fatalf("second provider info: %v", err)
	}
	if firstInfo.Title != "session=first" || secondInfo.Title != "session=second" {
		t.Fatalf("request cookies leaked: first=%q second=%q", firstInfo.Title, secondInfo.Title)
	}
	if original.cookies != "" {
		t.Fatalf("base provider was mutated: %q", original.cookies)
	}
	if info, _ := original.GetMovieInfoByID("base"); info.Title != requestCookieMovieDefaultInfo {
		t.Fatalf("base provider no longer uses default config: %q", info.Title)
	}
}

func TestWithRequestCookiesUpdatesHostLookups(t *testing.T) {
	e := newRequestCookieTestEngine(newRequestCookieMovie())

	scoped, err := e.WithRequestCookies(map[string]string{
		requestCookieMovieName: "session=host",
	})
	if err != nil {
		t.Fatalf("WithRequestCookies: %v", err)
	}
	provider, err := scoped.GetMovieProviderByURL(requestCookieMovieBaseURL + "/v/host")
	if err != nil {
		t.Fatalf("host lookup: %v", err)
	}
	info, err := provider.GetMovieInfoByID("host")
	if err != nil {
		t.Fatalf("host provider info: %v", err)
	}
	if info.Title != "session=host" {
		t.Fatalf("host lookup used base provider: %q", info.Title)
	}
}

func TestWithRequestCookiesAppliesToActorProvider(t *testing.T) {
	original := newRequestCookieActor()
	e := &Engine{
		movieProviders:       maps.NewCaseInsensitiveMap[mt.MovieProvider](),
		actorProviders:       maps.NewCaseInsensitiveMap[mt.ActorProvider](),
		movieProviderConfigs: maps.NewCaseInsensitiveMap[mt.Config](),
		actorProviderConfigs: maps.NewCaseInsensitiveMap[mt.Config](),
		movieHostProviders:   maps.NewCaseInsensitiveMap[[]mt.MovieProvider](),
		actorHostProviders:   maps.NewCaseInsensitiveMap[[]mt.ActorProvider](),
	}
	e.actorProviders.Set(original.Name(), original)
	e.actorHostProviders.Set(original.URL().Hostname(), []mt.ActorProvider{original})

	scoped, err := e.WithRequestCookies(map[string]string{
		requestCookieActorName: "session=actor",
	})
	if err != nil {
		t.Fatalf("WithRequestCookies: %v", err)
	}
	provider, err := scoped.GetActorProviderByURL(requestCookieActorBaseURL + "/actor/one")
	if err != nil {
		t.Fatalf("actor host lookup: %v", err)
	}
	info, err := provider.GetActorInfoByID("one")
	if err != nil {
		t.Fatalf("actor info: %v", err)
	}
	if info.Name != "session=actor" {
		t.Fatalf("actor request cookie was not applied: %q", info.Name)
	}
	if original.cookies != "" {
		t.Fatalf("actor base provider was mutated: %q", original.cookies)
	}
}

func TestWithRequestCookiesRejectsUnsupportedProvider(t *testing.T) {
	unsupported := newRequestCookieUnsupportedMovie()
	if _, err := newRequestCookieTestEngine(unsupported).WithRequestCookies(map[string]string{
		requestCookieUnsupportedName: "session=unsupported",
	}); err == nil {
		t.Fatal("WithRequestCookies() error = nil, want unsupported provider error")
	}
}

func TestWithRequestCookiesIgnoresUnknownAndEmptyValues(t *testing.T) {
	original := newRequestCookieMovie()
	e := newRequestCookieTestEngine(original)

	scoped, err := e.WithRequestCookies(map[string]string{
		"MissingProvider":      "session=ignored",
		requestCookieMovieName: "   ",
	})
	if err != nil {
		t.Fatalf("WithRequestCookies: %v", err)
	}
	info, err := scoped.MustGetMovieProviderByName(requestCookieMovieName).GetMovieInfoByID("empty")
	if err != nil {
		t.Fatalf("provider info: %v", err)
	}
	if info.Title != requestCookieMovieDefaultInfo {
		t.Fatalf("empty cookie changed provider: %q", info.Title)
	}
}

func TestWithRequestCookiesRejectsCRLF(t *testing.T) {
	e := newRequestCookieTestEngine(newRequestCookieMovie())
	_, err := e.WithRequestCookies(map[string]string{
		requestCookieMovieName: "session=x\r\nX-Evil: y",
	})
	if err == nil {
		t.Fatal("WithRequestCookies() error = nil, want CR/LF error")
	}
}

func TestRequestCookieConfigOverridesBaseCookies(t *testing.T) {
	config := requestCookieTestConfig{
		"cookies": "session=base",
		"timeout": "10s",
	}
	overlay := &requestCookieConfig{base: config, cookie: "session=request"}
	if !overlay.Has("cookie") || !overlay.Has("cookies") {
		t.Fatal("request cookie should be present")
	}
	value, err := overlay.GetString("cookies")
	if err != nil || value != "session=request" {
		t.Fatalf("cookies = %q, err = %v", value, err)
	}
	timeout, err := overlay.GetDuration("timeout")
	if err != nil || timeout != 10*time.Second {
		t.Fatalf("timeout = %s, err = %v", timeout, err)
	}
}

type requestCookieTestConfig map[string]string

func (c requestCookieTestConfig) Has(key string) bool {
	_, ok := c[strings.ToLower(key)]
	return ok
}
func (c requestCookieTestConfig) GetString(key string) (string, error) {
	value, ok := c[strings.ToLower(key)]
	if !ok {
		return "", errConfigKeyNotFound
	}
	return value, nil
}
func (c requestCookieTestConfig) GetBool(key string) (bool, error) {
	return false, errConfigKeyNotFound
}
func (c requestCookieTestConfig) GetInt64(key string) (int64, error) {
	return 0, errConfigKeyNotFound
}
func (c requestCookieTestConfig) GetFloat64(key string) (float64, error) {
	return 0, errConfigKeyNotFound
}
func (c requestCookieTestConfig) GetDuration(key string) (time.Duration, error) {
	value, err := c.GetString(key)
	if err != nil {
		return 0, err
	}
	return time.ParseDuration(value)
}
