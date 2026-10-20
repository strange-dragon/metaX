package route_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"golang.org/x/text/language"

	"github.com/metatube-community/metatube-sdk-go/model"
	mt "github.com/metatube-community/metatube-sdk-go/provider"
)

const (
	requestCookieRouteName       = "MetaXRequestCookieRouteProvider"
	requestCookieRouteBaseURL    = "https://request-cookie-route.invalid"
	requestCookieRouteHeaderName = "X-MetaX-Provider-Cookies"
)

type requestCookieRouteMovie struct {
	baseURL *url.URL
	cookies string
}

func (*requestCookieRouteMovie) Name() string           { return requestCookieRouteName }
func (*requestCookieRouteMovie) Priority() float64      { return 1 }
func (*requestCookieRouteMovie) SetPriority(float64)    {}
func (*requestCookieRouteMovie) Language() language.Tag { return language.English }
func (p *requestCookieRouteMovie) URL() *url.URL        { return p.baseURL }
func (*requestCookieRouteMovie) NormalizeMovieID(id string) string {
	return strings.TrimSpace(id)
}
func (p *requestCookieRouteMovie) ParseMovieIDFromURL(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	return strings.Trim(parsed.Path, "/"), nil
}
func (p *requestCookieRouteMovie) GetMovieInfoByID(id string) (*model.MovieInfo, error) {
	title := p.cookies
	if title == "" {
		title = "default"
	}
	return &model.MovieInfo{
		ID:       id,
		Number:   "ABC-123",
		Title:    title,
		Provider: p.Name(),
		Homepage: requestCookieRouteBaseURL + "/v/" + id,
		CoverURL: requestCookieRouteBaseURL + "/cover.jpg",
	}, nil
}
func (p *requestCookieRouteMovie) GetMovieInfoByURL(rawURL string) (*model.MovieInfo, error) {
	id, err := p.ParseMovieIDFromURL(rawURL)
	if err != nil {
		return nil, err
	}
	return p.GetMovieInfoByID(id)
}
func (*requestCookieRouteMovie) NormalizeMovieKeyword(keyword string) string {
	return strings.TrimSpace(keyword)
}
func (p *requestCookieRouteMovie) SearchMovie(keyword string) ([]*model.MovieSearchResult, error) {
	info, err := p.GetMovieInfoByID(keyword)
	if err != nil {
		return nil, err
	}
	return []*model.MovieSearchResult{info.ToSearchResult()}, nil
}
func (p *requestCookieRouteMovie) SetConfig(config mt.Config) error {
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

func init() {
	baseURL, _ := url.Parse(requestCookieRouteBaseURL)
	mt.Register(requestCookieRouteName, func() *requestCookieRouteMovie {
		return &requestCookieRouteMovie{baseURL: baseURL}
	})
}

func requestJSONWithCookieHeader(t *testing.T, router http.Handler, target, header string) (int, map[string]any) {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, target, nil)
	if header != "" {
		request.Header.Set(requestCookieRouteHeaderName, header)
	}
	router.ServeHTTP(recorder, request)
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response JSON: %v; body=%s", err, recorder.Body.String())
	}
	return recorder.Code, body
}

func TestRequestProviderCookiesApplyToSearchAndDetail(t *testing.T) {
	router := newContractRouter(t)
	header := fmt.Sprintf(`{"%s":"session=abc; cf_clearance=def"}`, requestCookieRouteName)

	cases := []string{
		"/v1/movies/search?q=ABC-123&provider=" + requestCookieRouteName + "&fallback=false",
		"/v1/movies/" + requestCookieRouteName + "/ABC-123?lazy=false",
	}
	for _, target := range cases {
		status, body := requestJSONWithCookieHeader(t, router, target, header)
		if status != http.StatusOK {
			t.Fatalf("%s status=%d body=%v", target, status, body)
		}
		if title := firstTitle(body); title != "session=abc; cf_clearance=def" {
			t.Fatalf("%s title=%q body=%v", target, title, body)
		}
	}
}

func TestRequestProviderCookiesDoNotLeakBetweenRequests(t *testing.T) {
	router := newContractRouter(t)
	const requests = 16
	var wg sync.WaitGroup
	errs := make(chan error, requests)
	for i := 0; i < requests; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			cookie := fmt.Sprintf("session=%d", index)
			header := fmt.Sprintf(`{"%s":%q}`, requestCookieRouteName, cookie)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet,
				"/v1/movies/search?q=ABC-123&provider="+requestCookieRouteName+"&fallback=false", nil)
			request.Header.Set(requestCookieRouteHeaderName, header)
			router.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusOK {
				errs <- fmt.Errorf("request %d status=%d body=%s", index, recorder.Code, recorder.Body.String())
				return
			}
			var body map[string]any
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				errs <- fmt.Errorf("request %d decode: %w", index, err)
				return
			}
			if title := firstTitle(body); title != cookie {
				errs <- fmt.Errorf("request %d title=%q want=%q", index, title, cookie)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestRequestProviderCookiesRejectInvalidInput(t *testing.T) {
	router := newContractRouter(t)
	target := "/v1/movies/search?q=ABC-123&provider=" + requestCookieRouteName + "&fallback=false"
	cases := map[string]string{
		"malformed JSON":   `{`,
		"array":            `["session=abc"]`,
		"trailing JSON":    `{"a":"b"}{}`,
		"CRLF":             fmt.Sprintf(`{"%s":"session=x\r\nX-Evil: y"}`, requestCookieRouteName),
		"empty provider":   `{"":"session=abc"}`,
		"oversized header": fmt.Sprintf(`{"%s":%q}`, requestCookieRouteName, strings.Repeat("a", 17<<10)),
	}
	for name, header := range cases {
		t.Run(name, func(t *testing.T) {
			status, body := requestJSONWithCookieHeader(t, router, target, header)
			if status != http.StatusBadRequest {
				t.Fatalf("status=%d want=%d body=%v", status, http.StatusBadRequest, body)
			}
		})
	}
}

func TestRequestProviderCookiesIgnoreUnknownProviders(t *testing.T) {
	router := newContractRouter(t)
	header := fmt.Sprintf(`{"MissingProvider":"session=ignored","%s":"session=abc"}`, requestCookieRouteName)
	status, body := requestJSONWithCookieHeader(t, router,
		"/v1/movies/search?q=ABC-123&provider="+requestCookieRouteName+"&fallback=false", header)
	if status != http.StatusOK {
		t.Fatalf("status=%d body=%v", status, body)
	}
	if title := firstTitle(body); title != "session=abc" {
		t.Fatalf("title=%q body=%v", title, body)
	}
}

func TestRequestProviderCookiesRejectProvidersWithoutConfigSetter(t *testing.T) {
	router := newContractRouter(t)
	header := fmt.Sprintf(`{"%s":"session=abc"}`, contractMovieName)
	status, body := requestJSONWithCookieHeader(t, router,
		"/v1/movies/search?q=contract&provider="+contractMovieName+"&fallback=false", header)
	if status != http.StatusBadRequest {
		t.Fatalf("status=%d body=%v", status, body)
	}
}

func firstTitle(body map[string]any) string {
	switch data := body["data"].(type) {
	case []any:
		if len(data) == 0 {
			return ""
		}
		record, _ := data[0].(map[string]any)
		title, _ := record["title"].(string)
		return title
	case map[string]any:
		title, _ := data["title"].(string)
		return title
	default:
		return ""
	}
}
