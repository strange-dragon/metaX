package engine

import (
	"net/url"
	"testing"
	"time"

	"golang.org/x/text/language"

	"github.com/strange-dragon/metaX/model"
	mt "github.com/strange-dragon/metaX/provider"
)

const (
	budgetFastProvider = "MetaXBudgetFastTest"
	budgetSlowProvider = "MetaXBudgetSlowTest"
)

// budgetTestProvider answers a search after an optional delay so the fan-out
// budget can be observed without a real network call.
type budgetTestProvider struct {
	name  string
	delay time.Duration
}

func (p *budgetTestProvider) Name() string         { return p.name }
func (*budgetTestProvider) Priority() float64      { return 1 }
func (*budgetTestProvider) SetPriority(float64)    {}
func (*budgetTestProvider) Language() language.Tag { return language.English }
func (p *budgetTestProvider) URL() *url.URL {
	u, _ := url.Parse("https://" + p.name + ".invalid")
	return u
}
func (*budgetTestProvider) NormalizeMovieID(id string) string { return id }
func (p *budgetTestProvider) ParseMovieIDFromURL(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	return u.Path, nil
}
func (p *budgetTestProvider) GetMovieInfoByID(id string) (*model.MovieInfo, error) {
	info := p.searchResultInfo()
	info.ID = id
	return info, nil
}
func (p *budgetTestProvider) GetMovieInfoByURL(rawURL string) (*model.MovieInfo, error) {
	id, err := p.ParseMovieIDFromURL(rawURL)
	if err != nil {
		return nil, err
	}
	return p.GetMovieInfoByID(id)
}
func (*budgetTestProvider) NormalizeMovieKeyword(keyword string) string { return keyword }
func (p *budgetTestProvider) SearchMovie(string) ([]*model.MovieSearchResult, error) {
	if p.delay > 0 {
		time.Sleep(p.delay)
	}
	return []*model.MovieSearchResult{p.searchResultInfo().ToSearchResult()}, nil
}
func (p *budgetTestProvider) searchResultInfo() *model.MovieInfo {
	return &model.MovieInfo{
		ID:       p.name + "-id",
		Number:   "ABC-123",
		Title:    p.name + " title",
		Provider: p.name,
		Homepage: "https://" + p.name + ".invalid/ABC-123",
		CoverURL: "https://images.invalid/" + p.name + ".jpg",
		ThumbURL: "https://images.invalid/" + p.name + ".jpg",
	}
}

func init() {
	mt.Register(budgetFastProvider, func() *budgetTestProvider {
		return &budgetTestProvider{name: budgetFastProvider}
	})
	mt.Register(budgetSlowProvider, func() *budgetTestProvider {
		return &budgetTestProvider{name: budgetSlowProvider, delay: 150 * time.Millisecond}
	})
}

// One hanging provider used to hold the whole category search, which is what
// makes a Jellyfin library refresh look hung. The budget answers with the
// providers that did reply.
func TestSearchMovieProvidersAnswersWithoutTheSlowestProvider(t *testing.T) {
	t.Setenv(EnvSearchBudget, "60ms")
	e := New(openAggregateTestDB(t))

	started := time.Now()
	results, err := e.SearchMovieProviders("ABC-123", []string{budgetFastProvider, budgetSlowProvider}, false)
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if elapsed >= 150*time.Millisecond {
		t.Fatalf("search waited %s, want it to stop at the 60ms budget", elapsed)
	}
	if len(results) != 1 || results[0].Provider != budgetFastProvider {
		t.Fatalf("results = %#v, want only the provider that answered", results)
	}
}

func TestSearchMovieProvidersWaitsForEveryProviderWhenBudgetIsOff(t *testing.T) {
	t.Setenv(EnvSearchBudget, "off")
	e := New(openAggregateTestDB(t))

	results, err := e.SearchMovieProviders("ABC-123", []string{budgetFastProvider, budgetSlowProvider}, false)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %d, want both providers", len(results))
	}
}

func TestSearchBudgetFromEnvFallsBackToTheDefault(t *testing.T) {
	t.Setenv(EnvSearchBudget, "not-a-duration")
	if got := searchBudget(); got != defaultSearchBudget {
		t.Fatalf("budget = %s, want default %s", got, defaultSearchBudget)
	}
	t.Setenv(EnvSearchBudget, "2s")
	if got := searchBudget(); got != 2*time.Second {
		t.Fatalf("budget = %s, want 2s", got)
	}
	t.Setenv(EnvSearchBudget, "disabled")
	if got := searchBudget(); got != 0 {
		t.Fatalf("budget = %s, want 0 for disabled", got)
	}
}
