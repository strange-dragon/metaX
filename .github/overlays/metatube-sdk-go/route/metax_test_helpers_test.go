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

	"github.com/metatube-community/metatube-sdk-go/engine"
	"github.com/metatube-community/metatube-sdk-go/model"
	mt "github.com/metatube-community/metatube-sdk-go/provider"
	sdkroute "github.com/metatube-community/metatube-sdk-go/route"
)

const contractMovieName = "MetaXContractMovieProvider"

type contractMovieProvider struct {
	baseURL *url.URL
}

func (*contractMovieProvider) Name() string         { return contractMovieName }
func (*contractMovieProvider) Priority() float64    { return 1 }
func (*contractMovieProvider) SetPriority(float64)  {}
func (*contractMovieProvider) Language() language.Tag { return language.English }
func (p *contractMovieProvider) URL() *url.URL      { return p.baseURL }
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
	return &model.MovieInfo{
		ID: id, Number: "contract", Title: "Contract title",
		Provider: contractMovieName,
		Homepage: p.baseURL.String() + "/movie/" + id,
		CoverURL: p.baseURL.String() + "/cover.jpg",
		Summary: "Contract summary",
	}, nil
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
	return []*model.MovieSearchResult{{
		ID: "movie-123", Number: "contract", Title: "Contract title",
		Provider: contractMovieName,
		Homepage: p.baseURL.String() + "/movie/movie-123",
	}}, nil
}

func init() {
	baseURL, _ := url.Parse("https://contract.invalid")
	mt.Register(contractMovieName, func() *contractMovieProvider {
		return &contractMovieProvider{baseURL: baseURL}
	})
}

func newContractRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	return sdkroute.New(engine.New(nil), nil)
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
