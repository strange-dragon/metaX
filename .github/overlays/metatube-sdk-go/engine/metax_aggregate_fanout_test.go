package engine

import (
	"encoding/json"
	"errors"
	"net/url"
	"testing"

	"golang.org/x/text/language"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/glebarez/sqlite"
	"github.com/lib/pq"

	"github.com/metatube-community/metatube-sdk-go/engine/providerid"
	"github.com/metatube-community/metatube-sdk-go/model"
	mt "github.com/metatube-community/metatube-sdk-go/provider"
	"github.com/metatube-community/metatube-sdk-go/routing"
)

const (
	aggregateBaselineProvider = "MetaXAggregateBaseline"
	aggregateSecondProvider   = "MetaXAggregateSecond"
)

type aggregateTestMovieProvider struct {
	name string
}

func (p *aggregateTestMovieProvider) Name() string         { return p.name }
func (*aggregateTestMovieProvider) Priority() float64      { return 1 }
func (*aggregateTestMovieProvider) SetPriority(float64)    {}
func (*aggregateTestMovieProvider) Language() language.Tag { return language.English }
func (p *aggregateTestMovieProvider) URL() *url.URL {
	u, _ := url.Parse("https://" + p.name + ".invalid")
	return u
}
func (*aggregateTestMovieProvider) NormalizeMovieID(id string) string { return id }
func (*aggregateTestMovieProvider) ParseMovieIDFromURL(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	return u.Path, nil
}
func (p *aggregateTestMovieProvider) GetMovieInfoByID(id string) (*model.MovieInfo, error) {
	info := aggregateTestMovieInfo(p.name, id)
	if p.name == aggregateSecondProvider {
		info.Title = "Second title"
		info.Summary = "Second summary"
		info.Actors = pq.StringArray{"Bob"}
	}
	return info, nil
}
func (p *aggregateTestMovieProvider) GetMovieInfoByURL(rawURL string) (*model.MovieInfo, error) {
	id, err := p.ParseMovieIDFromURL(rawURL)
	if err != nil {
		return nil, err
	}
	return p.GetMovieInfoByID(id)
}
func (*aggregateTestMovieProvider) NormalizeMovieKeyword(keyword string) string { return keyword }
func (p *aggregateTestMovieProvider) SearchMovie(string) ([]*model.MovieSearchResult, error) {
	return []*model.MovieSearchResult{aggregateTestMovieInfo(p.name, p.name+"-id").ToSearchResult()}, nil
}

// aggregateTestMovieInfo spells the same FC2 product differently per provider
// on purpose: the aggregation identity gate must fold them together.
func aggregateTestMovieInfo(provider, id string) *model.MovieInfo {
	number := "FC2-123456"
	if provider == aggregateBaselineProvider {
		number = "FC2-PPV-123456"
	}
	return &model.MovieInfo{
		ID:       id,
		Number:   number,
		Title:    "Baseline title",
		Provider: provider,
		Homepage: "https://" + provider + ".invalid/" + id,
		CoverURL: "https://images.invalid/" + provider + ".jpg",
		Actors:   pq.StringArray{"Alice"},
	}
}

func init() {
	mt.Register(aggregateBaselineProvider, func() *aggregateTestMovieProvider {
		return &aggregateTestMovieProvider{name: aggregateBaselineProvider}
	})
	mt.Register(aggregateSecondProvider, func() *aggregateTestMovieProvider {
		return &aggregateTestMovieProvider{name: aggregateSecondProvider}
	})
}

func openAggregateTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:metax-aggregate-fanout?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open in-memory database: %v", err)
	}
	if err := db.AutoMigrate(&model.MovieInfo{}); err != nil {
		t.Fatalf("migrate movie metadata table: %v", err)
	}
	return db
}

func TestAggregatedMovieInfoMergesFieldsAndKeepsOriginalProviderMetadata(t *testing.T) {
	e := New(openAggregateTestDB(t))
	config := routing.Config{Routes: map[routing.Category][]string{
		routing.CategoryUncensored: {aggregateBaselineProvider, aggregateSecondProvider},
	}}

	got, err := e.GetAggregatedMovieInfoByProviderIDWithConfig(providerid.ProviderID{
		Provider: aggregateBaselineProvider, ID: "baseline-id",
	}, false, config)
	if err != nil {
		t.Fatalf("aggregate detail: %v", err)
	}

	// FC2 is merged into uncensored in the public response.
	if got.Category != string(routing.CategoryUncensored) {
		t.Fatalf("category = %q, want %q", got.Category, routing.CategoryUncensored)
	}
	if got.Provider != aggregateBaselineProvider || got.ID != "baseline-id" {
		t.Fatalf("selected provider identity changed: %s/%s", got.Provider, got.ID)
	}
	// Merged top level: route order picks the baseline title and fills the
	// missing summary from the second provider.
	if got.Title != "Baseline title" || got.Summary != "Second summary" {
		t.Fatalf("merged fields = title %q, summary %q", got.Title, got.Summary)
	}
	if len(got.Actors) != 2 || got.Actors[0] != "Alice" || got.Actors[1] != "Bob" {
		t.Fatalf("merged actors = %#v", got.Actors)
	}

	// The response keeps every provider's original metadata, not just the
	// merged view.
	if len(got.Sources) != 2 {
		t.Fatalf("source records = %d, want 2: %#v", len(got.Sources), got.Sources)
	}
	byProvider := make(map[string]*model.MovieInfo, len(got.Sources))
	for _, source := range got.Sources {
		if source.Status != "success" {
			t.Fatalf("source %s status = %q, want success (error: %s)", source.Provider, source.Status, source.Error)
		}
		if source.Metadata == nil {
			t.Fatalf("source %s has no original metadata", source.Provider)
		}
		byProvider[source.Provider] = source.Metadata
	}
	baseline, ok := byProvider[aggregateBaselineProvider]
	if !ok || baseline.Title != "Baseline title" || baseline.Summary != "" {
		t.Fatalf("baseline original metadata changed: %#v", baseline)
	}
	second, ok := byProvider[aggregateSecondProvider]
	if !ok || second.Title != "Second title" || second.Summary != "Second summary" {
		t.Fatalf("second provider original metadata missing: %#v", second)
	}

	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal aggregate: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(encoded, &body); err != nil {
		t.Fatalf("unmarshal aggregate: %v", err)
	}
	if body["title"] != "Baseline title" || body["summary"] != "Second summary" {
		t.Fatalf("merged result is not at the top level: %s", encoded)
	}
	sources, ok := body["sources"].([]any)
	if !ok || len(sources) != 2 {
		t.Fatalf("original provider records missing from JSON: %s", encoded)
	}
	for _, raw := range sources {
		source, ok := raw.(map[string]any)
		if !ok || source["metadata"] == nil {
			t.Fatalf("source record lost its original metadata in JSON: %v", raw)
		}
	}
}

func TestAggregatedMovieInfoByNumberUsesRouteOrderIdentity(t *testing.T) {
	e := New(openAggregateTestDB(t))
	config := routing.Config{Routes: map[routing.Category][]string{
		routing.CategoryUncensored: {aggregateSecondProvider, aggregateBaselineProvider},
	}}

	got, err := e.GetAggregatedMovieInfoByNumber("FC2-PPV-123456", routing.CategoryUncensored, false, config)
	if err != nil {
		t.Fatalf("aggregate by number: %v", err)
	}
	if got.Category != string(routing.CategoryUncensored) {
		t.Fatalf("category = %q, want uncensored", got.Category)
	}
	// The first matching provider in route order becomes the public identity.
	if got.Provider != aggregateSecondProvider || got.ID != aggregateSecondProvider+"-id" {
		t.Fatalf("identity = %s/%s, want %s", got.Provider, got.ID, aggregateSecondProvider)
	}
	if got.Title != "Second title" || got.Summary != "Second summary" {
		t.Fatalf("merged fields = title %q, summary %q", got.Title, got.Summary)
	}
	if len(got.Sources) != 2 {
		t.Fatalf("source records = %d, want 2: %#v", len(got.Sources), got.Sources)
	}
	for _, source := range got.Sources {
		if source.Status != "success" || source.Metadata == nil {
			t.Fatalf("source %s lost its original metadata: %+v", source.Provider, source)
		}
	}
}

func TestAggregatedMovieInfoByNumberRejectsUnknownCategory(t *testing.T) {
	e := New(openAggregateTestDB(t))

	_, err := e.GetAggregatedMovieInfoByNumber(
		"FC2-123456",
		routing.Category("not-a-category"),
		false,
		routing.DefaultConfig(),
	)
	if !errors.Is(err, routing.ErrUnknownCategory) {
		t.Fatalf("error = %v, want ErrUnknownCategory", err)
	}
}

func TestResolveAggregatedProviderIDUsesRouteOrder(t *testing.T) {
	e := New(openAggregateTestDB(t))
	config := routing.Config{Routes: map[routing.Category][]string{
		routing.CategoryUncensored: {aggregateSecondProvider, aggregateBaselineProvider},
	}}

	pid, err := e.ResolveAggregatedProviderID("FC2-PPV-123456", routing.CategoryUncensored, config)
	if err != nil {
		t.Fatalf("resolve aggregated provider id: %v", err)
	}
	if pid.Provider != aggregateSecondProvider || pid.ID != aggregateSecondProvider+"-id" {
		t.Fatalf("resolved identity = %s/%s, want %s", pid.Provider, pid.ID, aggregateSecondProvider)
	}
}
