package route

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/text/language"

	"github.com/metatube-community/metatube-sdk-go/engine"
	"github.com/metatube-community/metatube-sdk-go/model"
	mt "github.com/metatube-community/metatube-sdk-go/provider"
	"github.com/metatube-community/metatube-sdk-go/routing"
)

const (
	routeHealthyProvider       = "MetaXRouteHealthyProvider"
	routeSecondHealthyProvider = "MetaXRouteSecondHealthyProvider"
	routeFailingProvider       = "MetaXRouteFailingProvider"
)

var (
	routeSearchStarted chan string
	routeSearchRelease chan struct{}
)

type routeTestMovieProvider struct {
	name string
	fail bool
}

func (p *routeTestMovieProvider) Name() string         { return p.name }
func (*routeTestMovieProvider) Priority() float64      { return 1 }
func (*routeTestMovieProvider) SetPriority(float64)    {}
func (*routeTestMovieProvider) Language() language.Tag { return language.English }
func (p *routeTestMovieProvider) URL() *url.URL {
	u, _ := url.Parse("https://" + p.name + ".invalid")
	return u
}
func (*routeTestMovieProvider) NormalizeMovieID(id string) string { return id }
func (*routeTestMovieProvider) ParseMovieIDFromURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	return u.Path, nil
}
func (p *routeTestMovieProvider) GetMovieInfoByID(id string) (*model.MovieInfo, error) {
	return routeTestMovieInfo(p.name, id), nil
}
func (p *routeTestMovieProvider) GetMovieInfoByURL(raw string) (*model.MovieInfo, error) {
	id, err := p.ParseMovieIDFromURL(raw)
	if err != nil {
		return nil, err
	}
	return p.GetMovieInfoByID(id)
}
func (p *routeTestMovieProvider) NormalizeMovieKeyword(keyword string) string { return keyword }
func (p *routeTestMovieProvider) SearchMovie(string) ([]*model.MovieSearchResult, error) {
	routeSearchStarted <- p.name
	<-routeSearchRelease
	if p.fail {
		return nil, errors.New("simulated provider failure")
	}
	return []*model.MovieSearchResult{routeTestMovieInfo(p.name, "healthy-123").ToSearchResult()}, nil
}

func routeTestMovieInfo(provider, id string) *model.MovieInfo {
	return &model.MovieInfo{
		ID: id, Number: "TEST-123", Title: "Category Route Test",
		Provider: provider, Homepage: "https://" + provider + ".invalid/" + id,
		CoverURL: "https://images.invalid/route-test.jpg",
	}
}

func init() {
	mt.Register(routeHealthyProvider, func() *routeTestMovieProvider {
		return &routeTestMovieProvider{name: routeHealthyProvider}
	})
	mt.Register(routeSecondHealthyProvider, func() *routeTestMovieProvider {
		return &routeTestMovieProvider{name: routeSecondHealthyProvider}
	})
	mt.Register(routeFailingProvider, func() *routeTestMovieProvider {
		return &routeTestMovieProvider{name: routeFailingProvider, fail: true}
	})
}

func TestMetaXCategorySearchFansOutAndKeepsSuccessfulProviderResults(t *testing.T) {
	gin.SetMode(gin.TestMode)
	routeSearchStarted = make(chan string, 3)
	routeSearchRelease = make(chan struct{})
	defer func() {
		select {
		case <-routeSearchRelease:
		default:
			close(routeSearchRelease)
		}
	}()

	config := routing.Config{Routes: map[routing.Category][]string{
		routing.Category("test-category"): {routeHealthyProvider, routeSecondHealthyProvider, routeFailingProvider},
	}}
	router := gin.New()
	router.GET("/v1/movies/search", getSearchWithConfig(engine.New(nil), movieSearchType, config))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/movies/search?q=route-test&category=test-category&fallback=false", nil)
	done := make(chan struct{})
	go func() {
		router.ServeHTTP(recorder, request)
		close(done)
	}()

	seen := map[string]bool{}
	for len(seen) < 3 {
		select {
		case name := <-routeSearchStarted:
			seen[name] = true
		case <-time.After(2 * time.Second):
			t.Fatalf("category search did not start all providers concurrently; started=%v", seen)
		}
	}
	close(routeSearchRelease)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("category search did not return after providers completed")
	}

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d want=%d body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var response struct {
		Data []*model.MovieSearchResult `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, recorder.Body.String())
	}
	if len(response.Data) != 3 {
		t.Fatalf("result count=%d want=3 (merged + 2 providers): %+v", len(response.Data), response.Data)
	}
	if response.Data[0].Provider != engine.AggregatedProviderName || response.Data[0].ID != "TEST-123" {
		t.Fatalf("first result should be the merged candidate: %+v", response.Data[0])
	}
	if response.Data[1].Provider != routeHealthyProvider || response.Data[1].ID != "healthy-123" {
		t.Fatalf("first provider result should follow Amane route order: %+v", response.Data)
	}
	if response.Data[2].Provider != routeSecondHealthyProvider || response.Data[2].ID != "healthy-123" {
		t.Fatalf("second provider result should follow Amane route order: %+v", response.Data)
	}
}

func TestMetaXFC2NumberAutoRoutesWithoutCategory(t *testing.T) {
	gin.SetMode(gin.TestMode)
	routeSearchStarted = make(chan string, 3)
	routeSearchRelease = make(chan struct{})
	defer func() {
		select {
		case <-routeSearchRelease:
		default:
			close(routeSearchRelease)
		}
	}()

	config := routing.Config{Routes: map[routing.Category][]string{
		routing.CategoryUncensored: {routeHealthyProvider, routeSecondHealthyProvider, routeFailingProvider},
	}}
	router := gin.New()
	router.GET("/v1/movies/search", getSearchWithConfig(engine.New(nil), movieSearchType, config))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/movies/search?q=fc2-ppv3954834&fallback=false", nil)
	done := make(chan struct{})
	go func() {
		router.ServeHTTP(recorder, request)
		close(done)
	}()

	seen := map[string]bool{}
	for len(seen) < 3 {
		select {
		case name := <-routeSearchStarted:
			seen[name] = true
		case <-time.After(2 * time.Second):
			t.Fatalf("automatic FC2 routing did not start all providers; started=%v", seen)
		}
	}
	close(routeSearchRelease)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("automatically routed search did not return")
	}

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d want=%d body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var response struct {
		Data []*model.MovieSearchResult
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, recorder.Body.String())
	}
	if len(response.Data) != 3 {
		t.Fatalf("result count=%d want=3 (merged + 2 providers): %+v", len(response.Data), response.Data)
	}
	if response.Data[0].Provider != engine.AggregatedProviderName {
		t.Fatalf("first result should be the merged candidate: %+v", response.Data[0])
	}
	if response.Data[1].Provider != routeHealthyProvider || response.Data[2].Provider != routeSecondHealthyProvider {
		t.Fatalf("automatic FC2 route did not preserve configured provider order: %+v", response.Data)
	}
}

func TestMetaXCategorySearchUsesActiveRouteTable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	routeSearchStarted = make(chan string, 2)
	routeSearchRelease = make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-routeSearchRelease:
		default:
			close(routeSearchRelease)
		}
		if err := routing.SetActiveConfig(routing.DefaultConfig()); err != nil {
			t.Errorf("restore default routing config: %v", err)
		}
	})

	config := routing.Config{Routes: map[routing.Category][]string{
		routing.CategoryCensored: {routeHealthyProvider},
	}}
	if err := routing.SetActiveConfig(config); err != nil {
		t.Fatalf("set active routing config: %v", err)
	}

	router := gin.New()
	router.GET("/v1/movies/search", getSearch(engine.New(nil), movieSearchType))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/movies/search?q=route-test&category=censored&fallback=false", nil)
	done := make(chan struct{})
	go func() {
		router.ServeHTTP(recorder, request)
		close(done)
	}()

	select {
	case name := <-routeSearchStarted:
		if name != routeHealthyProvider {
			t.Errorf("active route searched %q, want %q", name, routeHealthyProvider)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("active route search did not start the configured provider")
	}
	close(routeSearchRelease)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("active route search did not return")
	}

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d want=%d body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var response struct {
		Data []*model.MovieSearchResult `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Data) != 2 {
		t.Fatalf("results=%d want=2 (merged + one original): %s", len(response.Data), recorder.Body.String())
	}
	if response.Data[0].Provider != engine.AggregatedProviderName {
		t.Fatalf("first provider=%q want=%q", response.Data[0].Provider, engine.AggregatedProviderName)
	}
	if response.Data[1].Provider != routeHealthyProvider {
		t.Fatalf("original provider=%q want=%q", response.Data[1].Provider, routeHealthyProvider)
	}
}
