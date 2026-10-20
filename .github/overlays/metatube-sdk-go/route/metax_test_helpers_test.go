package route_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"golang.org/x/text/language"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/glebarez/sqlite"

	"github.com/metatube-community/metatube-sdk-go/engine"
	"github.com/metatube-community/metatube-sdk-go/model"
	mt "github.com/metatube-community/metatube-sdk-go/provider"
	sdkroute "github.com/metatube-community/metatube-sdk-go/route"
)

const (
	contractMovieName = "MetaXContractMovieProvider"
	contractActorName = "MetaXContractActorProvider"
)

type contractMovieProvider struct {
	baseURL *url.URL
}

func (*contractMovieProvider) Name() string           { return contractMovieName }
func (*contractMovieProvider) Priority() float64      { return 1 }
func (*contractMovieProvider) SetPriority(float64)    {}
func (*contractMovieProvider) Language() language.Tag { return language.English }
func (p *contractMovieProvider) URL() *url.URL        { return p.baseURL }
func (*contractMovieProvider) NormalizeMovieID(id string) string {
	return strings.TrimSpace(id)
}
func (*contractMovieProvider) ParseMovieIDFromURL(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) == 0 {
		return "", nil
	}
	return parts[len(parts)-1], nil
}
func (p *contractMovieProvider) GetMovieInfoByID(id string) (*model.MovieInfo, error) {
	return contractMovieInfo(p.baseURL.String(), id), nil
}
func (p *contractMovieProvider) GetMovieInfoByURL(rawURL string) (*model.MovieInfo, error) {
	id, err := p.ParseMovieIDFromURL(rawURL)
	if err != nil {
		return nil, err
	}
	return p.GetMovieInfoByID(id)
}
func (*contractMovieProvider) NormalizeMovieKeyword(keyword string) string {
	return strings.TrimSpace(keyword)
}
func (p *contractMovieProvider) SearchMovie(keyword string) ([]*model.MovieSearchResult, error) {
	if strings.TrimSpace(keyword) == "" {
		return nil, mt.ErrInvalidKeyword
	}
	return []*model.MovieSearchResult{contractMovieInfo(p.baseURL.String(), "movie-123").ToSearchResult()}, nil
}

func contractMovieInfo(baseURL, id string) *model.MovieInfo {
	return &model.MovieInfo{
		ID: id, Number: "contract", Title: "Contract title",
		Provider: contractMovieName,
		Homepage: baseURL + "/movie/" + id,
		CoverURL: baseURL + "/cover.jpg",
		Summary:  "Contract summary",
	}
}

type contractActorProvider struct {
	baseURL *url.URL
}

func (*contractActorProvider) Name() string           { return contractActorName }
func (*contractActorProvider) Priority() float64      { return 1 }
func (*contractActorProvider) SetPriority(float64)    {}
func (*contractActorProvider) Language() language.Tag { return language.English }
func (p *contractActorProvider) URL() *url.URL        { return p.baseURL }
func (*contractActorProvider) NormalizeActorID(id string) string {
	return strings.TrimSpace(id)
}
func (*contractActorProvider) ParseActorIDFromURL(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) == 0 {
		return "", nil
	}
	return parts[len(parts)-1], nil
}
func (p *contractActorProvider) GetActorInfoByID(id string) (*model.ActorInfo, error) {
	return &model.ActorInfo{
		ID: id, Name: "Contract Actor", Provider: contractActorName,
		Homepage: p.baseURL.String() + "/actor/" + id,
	}, nil
}
func (p *contractActorProvider) GetActorInfoByURL(rawURL string) (*model.ActorInfo, error) {
	id, err := p.ParseActorIDFromURL(rawURL)
	if err != nil {
		return nil, err
	}
	return p.GetActorInfoByID(id)
}
func (p *contractActorProvider) SearchActor(string) ([]*model.ActorSearchResult, error) {
	info, err := p.GetActorInfoByID("actor-42")
	if err != nil {
		return nil, err
	}
	return []*model.ActorSearchResult{info.ToSearchResult()}, nil
}

func init() {
	movieURL, _ := url.Parse("https://contract.invalid")
	mt.Register(contractMovieName, func() *contractMovieProvider {
		return &contractMovieProvider{baseURL: movieURL}
	})
	actorURL, _ := url.Parse("https://contract-actor.invalid")
	mt.Register(contractActorName, func() *contractActorProvider {
		return &contractActorProvider{baseURL: actorURL}
	})
}

// newContractRouter uses a real in-memory database because the SDK engine
// persists every successfully fetched MovieInfo. engine.New(nil) panics on
// detail requests and would surface as an HTTP 500 in endpoint tests.
func newContractRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:metax-api-contract?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open in-memory database: %v", err)
	}
	if err := db.AutoMigrate(&model.MovieInfo{}, &model.ActorInfo{}); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	return sdkroute.New(engine.New(db), nil)
}

func requestJSON(t *testing.T, router http.Handler, target string) (int, map[string]any) {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, target, nil)
	router.ServeHTTP(recorder, request)
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response JSON: %v; body=%s", err, recorder.Body.String())
	}
	return recorder.Code, body
}
