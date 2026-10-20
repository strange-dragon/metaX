package route

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gin-gonic/gin"
	"golang.org/x/text/language"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/glebarez/sqlite"

	"github.com/metatube-community/metatube-sdk-go/engine"
	"github.com/metatube-community/metatube-sdk-go/model"
	mt "github.com/metatube-community/metatube-sdk-go/provider"
	"github.com/metatube-community/metatube-sdk-go/routing"
)

const endpointAggregateProvider = "MetaXEndpointAggregateProvider"

type endpointAggregateMovieProvider struct{}

func (*endpointAggregateMovieProvider) Name() string           { return endpointAggregateProvider }
func (*endpointAggregateMovieProvider) Priority() float64      { return 1 }
func (*endpointAggregateMovieProvider) SetPriority(float64)    {}
func (*endpointAggregateMovieProvider) Language() language.Tag { return language.English }
func (*endpointAggregateMovieProvider) URL() *url.URL {
	u, _ := url.Parse("https://endpoint-aggregate.invalid")
	return u
}
func (*endpointAggregateMovieProvider) NormalizeMovieID(id string) string { return id }
func (*endpointAggregateMovieProvider) ParseMovieIDFromURL(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	return u.Path, nil
}
func (*endpointAggregateMovieProvider) GetMovieInfoByID(id string) (*model.MovieInfo, error) {
	return endpointAggregateMovieInfo(id), nil
}
func (p *endpointAggregateMovieProvider) GetMovieInfoByURL(rawURL string) (*model.MovieInfo, error) {
	id, err := p.ParseMovieIDFromURL(rawURL)
	if err != nil {
		return nil, err
	}
	return p.GetMovieInfoByID(id)
}
func (*endpointAggregateMovieProvider) NormalizeMovieKeyword(keyword string) string { return keyword }
func (*endpointAggregateMovieProvider) SearchMovie(string) ([]*model.MovieSearchResult, error) {
	return []*model.MovieSearchResult{endpointAggregateMovieInfo("endpoint-1").ToSearchResult()}, nil
}

func endpointAggregateMovieInfo(id string) *model.MovieInfo {
	return &model.MovieInfo{
		ID: id, Number: "ABP-123", Title: "Endpoint title",
		Provider: endpointAggregateProvider,
		Homepage: "https://endpoint-aggregate.invalid/movie/" + id,
		CoverURL: "https://endpoint-aggregate.invalid/cover.jpg",
		Summary:  "Endpoint summary",
	}
}

func init() {
	mt.Register(endpointAggregateProvider, func() *endpointAggregateMovieProvider {
		return &endpointAggregateMovieProvider{}
	})
}

func openEndpointAggregateDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:metax-endpoint-aggregate?mode=memory&cache=shared"), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("open in-memory database: %v", err)
	}
	if err := db.AutoMigrate(&model.MovieInfo{}); err != nil {
		t.Fatalf("migrate movie metadata table: %v", err)
	}
	return db
}

func TestAggregatedDetailEndpointKeepsReservedIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	config := routing.Config{Routes: map[routing.Category][]string{
		routing.CategoryCensored: {endpointAggregateProvider},
	}}
	router := gin.New()
	router.GET("/v1/movies/:provider/:id", getInfoWithConfig(engine.New(openEndpointAggregateDB(t)), movieInfoType, config))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet,
		"/v1/movies/"+engine.AggregatedProviderName+"/ABP-123?category=censored", nil)
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d want=%d body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var response struct {
		Data struct {
			ID       string `json:"id"`
			Provider string `json:"provider"`
			Number   string `json:"number"`
			Title    string `json:"title"`
			Category string `json:"category"`
			Sources  []struct {
				Provider string         `json:"provider"`
				ID       string         `json:"id"`
				Status   string         `json:"status"`
				Metadata map[string]any `json:"metadata"`
			} `json:"sources"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, recorder.Body.String())
	}
	data := response.Data
	if data.Provider != engine.AggregatedProviderName || data.ID != "ABP-123" {
		t.Fatalf("merged identity = %s/%s, want %s/ABP-123", data.Provider, data.ID, engine.AggregatedProviderName)
	}
	if data.Number != "ABP-123" || data.Title != "Endpoint title" {
		t.Fatalf("merged fields changed: %+v", data)
	}
	if data.Category != string(routing.CategoryCensored) {
		t.Fatalf("category = %q, want censored", data.Category)
	}
	if len(data.Sources) != 1 || data.Sources[0].Provider != endpointAggregateProvider ||
		data.Sources[0].Status != "success" || data.Sources[0].Metadata["title"] != "Endpoint title" {
		t.Fatalf("original provider record missing: %+v", data.Sources)
	}
}

func TestAggregatedDetailEndpointRejectsUnknownCategory(t *testing.T) {
	gin.SetMode(gin.TestMode)
	config := routing.Config{Routes: map[routing.Category][]string{
		routing.CategoryCensored: {endpointAggregateProvider},
	}}
	router := gin.New()
	router.GET("/v1/movies/:provider/:id", getInfoWithConfig(engine.New(openEndpointAggregateDB(t)), movieInfoType, config))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet,
		"/v1/movies/"+engine.AggregatedProviderName+"/ABP-123?category=not-a-category", nil)
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want=%d body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
}

func TestAggregatedDetailEndpointRejectsUnknownCategoryForRealProvider(t *testing.T) {
	gin.SetMode(gin.TestMode)
	config := routing.Config{Routes: map[routing.Category][]string{
		routing.CategoryCensored: {endpointAggregateProvider},
	}}
	router := gin.New()
	router.GET("/v1/movies/:provider/:id", getInfoWithConfig(engine.New(openEndpointAggregateDB(t)), movieInfoType, config))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet,
		"/v1/movies/"+endpointAggregateProvider+"/endpoint-1?aggregate=true&category=not-a-category", nil)
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want=%d body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
}

func TestAggregatedDetailEndpointRejectsCategoryWithoutProviders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	config := routing.Config{Routes: map[routing.Category][]string{
		routing.CategoryCensored: {"MetaXUnregisteredProvider"},
	}}
	router := gin.New()
	router.GET("/v1/movies/:provider/:id", getInfoWithConfig(engine.New(openEndpointAggregateDB(t)), movieInfoType, config))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet,
		"/v1/movies/"+endpointAggregateProvider+"/endpoint-1?aggregate=true&category=censored", nil)
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want=%d body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet,
		"/v1/movies/"+engine.AggregatedProviderName+"/ABP-123?category=censored", nil)
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("merged detail status=%d want=%d body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
}

func TestMetaXImageRejectsUnknownCategory(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/v1/images/primary/:provider/:id", getImage(engine.New(nil), primaryImageType))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet,
		"/v1/images/primary/"+engine.AggregatedProviderName+"/ABP-123?category=not-a-category", nil)
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want=%d body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
}
