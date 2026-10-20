package engine

import (
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/text/language"

	"github.com/metatube-community/metatube-sdk-go/engine/providerid"
	"github.com/metatube-community/metatube-sdk-go/model"
	mt "github.com/metatube-community/metatube-sdk-go/provider"
	"github.com/metatube-community/metatube-sdk-go/routing"
)

const fanoutTestNumber = "FC2-PPV-123456"

var fanoutTestProviderNames = []string{
	"MetaXFanoutTestProvider1",
	"MetaXFanoutTestProvider2",
	"MetaXFanoutTestProvider3",
	"MetaXFanoutTestProvider4",
	"MetaXFanoutTestProvider5",
	"MetaXFanoutTestProvider6",
}

type fanoutProbe struct {
	inFlight    atomic.Int32
	maxInFlight atomic.Int32
	delay       time.Duration
}

func (p *fanoutProbe) enter() {
	current := p.inFlight.Add(1)
	for {
		observed := p.maxInFlight.Load()
		if current <= observed || p.maxInFlight.CompareAndSwap(observed, current) {
			break
		}
	}
	if p.delay > 0 {
		time.Sleep(p.delay)
	}
}

func (p *fanoutProbe) leave() {
	p.inFlight.Add(-1)
}

func (p *fanoutProbe) reset() {
	p.inFlight.Store(0)
	p.maxInFlight.Store(0)
}

var fanoutTestProbe = &fanoutProbe{delay: 25 * time.Millisecond}

type fanoutTestMovieProvider struct {
	name  string
	probe *fanoutProbe
}

func (p *fanoutTestMovieProvider) Name() string         { return p.name }
func (*fanoutTestMovieProvider) Priority() float64      { return 1 }
func (*fanoutTestMovieProvider) SetPriority(float64)    {}
func (*fanoutTestMovieProvider) Language() language.Tag { return language.English }
func (p *fanoutTestMovieProvider) URL() *url.URL {
	u, _ := url.Parse("https://" + p.name + ".invalid")
	return u
}
func (*fanoutTestMovieProvider) NormalizeMovieID(id string) string { return id }
func (p *fanoutTestMovieProvider) ParseMovieIDFromURL(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	return u.Path, nil
}
func (p *fanoutTestMovieProvider) GetMovieInfoByID(id string) (*model.MovieInfo, error) {
	p.probe.enter()
	defer p.probe.leave()
	info := fanoutTestMovieInfo(p.name)
	info.ID = id
	return info, nil
}
func (p *fanoutTestMovieProvider) GetMovieInfoByURL(rawURL string) (*model.MovieInfo, error) {
	id, err := p.ParseMovieIDFromURL(rawURL)
	if err != nil {
		return nil, err
	}
	return p.GetMovieInfoByID(id)
}
func (*fanoutTestMovieProvider) NormalizeMovieKeyword(keyword string) string { return keyword }
func (p *fanoutTestMovieProvider) SearchMovie(string) ([]*model.MovieSearchResult, error) {
	p.probe.enter()
	defer p.probe.leave()
	return []*model.MovieSearchResult{fanoutTestMovieInfo(p.name).ToSearchResult()}, nil
}

func fanoutTestMovieInfo(provider string) *model.MovieInfo {
	return &model.MovieInfo{
		ID:       provider + "-id",
		Number:   fanoutTestNumber,
		Title:    "Fanout title " + provider,
		Provider: provider,
		Homepage: "https://" + provider + ".invalid/" + provider + "-id",
		CoverURL: "https://images.invalid/" + provider + ".jpg",
	}
}

func init() {
	for _, name := range fanoutTestProviderNames {
		name := name
		mt.Register(name, func() *fanoutTestMovieProvider {
			return &fanoutTestMovieProvider{name: name, probe: fanoutTestProbe}
		})
	}
}

func TestParseFanoutLimit(t *testing.T) {
	tests := map[string]int{
		"":         defaultAggregateFanout,
		"4":        4,
		"0":        0,
		"off":      0,
		"disabled": 0,
		"none":     0,
		"-1":       defaultAggregateFanout,
		"bogus":    defaultAggregateFanout,
	}
	for raw, want := range tests {
		if got := parseFanoutLimit(raw); got != want {
			t.Errorf("parseFanoutLimit(%q) = %d, want %d", raw, got, want)
		}
	}
}

func TestFanoutLimiterBoundsCategorySearch(t *testing.T) {
	t.Cleanup(setAggregateFanoutLimit(2))
	fanoutTestProbe.reset()

	app := New(openAggregateTestDB(t))
	results, err := app.SearchMovieProviders("FANOUT-SEARCH-001", fanoutTestProviderNames, false)
	if err != nil {
		t.Fatalf("search providers: %v", err)
	}
	if len(results) != len(fanoutTestProviderNames) {
		t.Fatalf("results = %d, want %d", len(results), len(fanoutTestProviderNames))
	}
	if got := fanoutTestProbe.maxInFlight.Load(); got > 2 {
		t.Fatalf("max concurrent provider calls = %d, want <= 2", got)
	}
}

func TestFanoutLimiterBoundsAggregatedDetail(t *testing.T) {
	t.Cleanup(setAggregateFanoutLimit(2))
	t.Cleanup(restoreAggregateCaches)
	resetAggregateCaches(time.Minute)
	fanoutTestProbe.reset()

	app := New(openAggregateTestDB(t))
	config := routing.Config{Routes: map[routing.Category][]string{
		// aggregateTestMovieInfo uses an FC2 number, which folds into uncensored.
		routing.CategoryUncensored: append([]string{aggregateBaselineProvider}, fanoutTestProviderNames...),
	}}
	got, err := app.GetAggregatedMovieInfoByProviderIDWithConfig(
		providerid.ProviderID{Provider: aggregateBaselineProvider, ID: "fanout-baseline"},
		false,
		config,
	)
	if err != nil {
		t.Fatalf("aggregate detail: %v", err)
	}
	if len(got.Sources) != len(fanoutTestProviderNames)+1 {
		t.Fatalf("sources = %d, want %d", len(got.Sources), len(fanoutTestProviderNames)+1)
	}
	for _, source := range got.Sources {
		if source.Status != "success" {
			t.Fatalf("source %s status = %q, want success: %+v", source.Provider, source.Status, source)
		}
	}
	if got := fanoutTestProbe.maxInFlight.Load(); got > 2 {
		t.Fatalf("max concurrent provider calls = %d, want <= 2", got)
	}
}
