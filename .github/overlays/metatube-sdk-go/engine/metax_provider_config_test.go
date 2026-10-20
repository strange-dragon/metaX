package engine

import (
	"net/url"
	"testing"

	"golang.org/x/text/language"

	"github.com/metatube-community/metatube-sdk-go/collection/maps"
	"github.com/metatube-community/metatube-sdk-go/model"
	mt "github.com/metatube-community/metatube-sdk-go/provider"
)

type runtimeConfigMovieBase struct {
	name string
}

func (p *runtimeConfigMovieBase) Name() string           { return p.name }
func (p *runtimeConfigMovieBase) Priority() float64      { return 1 }
func (p *runtimeConfigMovieBase) SetPriority(float64)    {}
func (p *runtimeConfigMovieBase) Language() language.Tag { return language.English }
func (p *runtimeConfigMovieBase) URL() *url.URL {
	return &url.URL{Scheme: "https", Host: "example.invalid"}
}
func (p *runtimeConfigMovieBase) NormalizeMovieID(id string) string { return id }
func (p *runtimeConfigMovieBase) ParseMovieIDFromURL(rawURL string) (string, error) {
	return rawURL, nil
}
func (p *runtimeConfigMovieBase) GetMovieInfoByID(string) (*model.MovieInfo, error) {
	return nil, nil
}
func (p *runtimeConfigMovieBase) GetMovieInfoByURL(string) (*model.MovieInfo, error) {
	return nil, nil
}

type runtimeConfigTestMovie struct {
	runtimeConfigMovieBase
	config mt.Config
	calls  int
}

func (p *runtimeConfigTestMovie) SetConfig(config mt.Config) error {
	p.config = config
	p.calls++
	return nil
}

type runtimeConfigUnsupportedMovie struct {
	runtimeConfigMovieBase
}

func newConfigTestEngine(providers ...mt.MovieProvider) *Engine {
	e := &Engine{
		movieProviders:       maps.NewCaseInsensitiveMap[mt.MovieProvider](),
		actorProviders:       maps.NewCaseInsensitiveMap[mt.ActorProvider](),
		movieProviderConfigs: maps.NewCaseInsensitiveMap[mt.Config](),
		actorProviderConfigs: maps.NewCaseInsensitiveMap[mt.Config](),
	}
	for _, provider := range providers {
		e.movieProviders.Set(provider.Name(), provider)
	}
	return e
}

func TestSetProviderConfigAppliesRuntimeValues(t *testing.T) {
	provider := &runtimeConfigTestMovie{runtimeConfigMovieBase: runtimeConfigMovieBase{name: "RuntimeConfigMovie"}}
	e := newConfigTestEngine(provider)

	if err := e.SetProviderConfig("MOVIE", "runtimeconfigmovie", map[string]string{
		"COOKIES": "session=abc; cf_clearance=def",
	}); err != nil {
		t.Fatalf("SetProviderConfig() error = %v", err)
	}
	if provider.calls != 1 {
		t.Fatalf("SetConfig calls = %d, want 1", provider.calls)
	}
	if provider.config == nil || !provider.config.Has("cookies") {
		t.Fatalf("provider did not receive cookies config: %#v", provider.config)
	}
	value, err := provider.config.GetString("COOKIES")
	if err != nil || value != "session=abc; cf_clearance=def" {
		t.Fatalf("cookies = %q, err = %v", value, err)
	}
}

func TestSetProviderConfigRejectsInvalidUpdates(t *testing.T) {
	supported := &runtimeConfigTestMovie{runtimeConfigMovieBase: runtimeConfigMovieBase{name: "RuntimeConfigSupported"}}
	unsupported := &runtimeConfigUnsupportedMovie{runtimeConfigMovieBase: runtimeConfigMovieBase{name: "RuntimeConfigUnsupported"}}
	e := newConfigTestEngine(supported, unsupported)

	for name, test := range map[string]struct {
		providerType string
		provider     string
		values       map[string]string
	}{
		"missing type":         {"", supported.Name(), map[string]string{"cookies": "x"}},
		"unknown type":         {"series", supported.Name(), map[string]string{"cookies": "x"}},
		"unknown provider":     {"movie", "MissingProvider", map[string]string{"cookies": "x"}},
		"empty config":         {"movie", supported.Name(), map[string]string{}},
		"unsupported provider": {"movie", unsupported.Name(), map[string]string{"cookies": "x"}},
		"unsupported key":      {"movie", supported.Name(), map[string]string{"proxy": "http://127.0.0.1:8080"}},
		"header injection":     {"movie", supported.Name(), map[string]string{"cookies": "x\r\nX-Evil: y"}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := e.SetProviderConfig(test.providerType, test.provider, test.values); err == nil {
				t.Fatalf("SetProviderConfig() error = nil, want error")
			}
		})
	}
	if supported.calls != 0 {
		t.Fatalf("supported provider received invalid config %d times", supported.calls)
	}
}
