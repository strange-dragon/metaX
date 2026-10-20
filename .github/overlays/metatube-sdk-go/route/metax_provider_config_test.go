package route_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"golang.org/x/text/language"

	"github.com/metatube-community/metatube-sdk-go/engine"
	"github.com/metatube-community/metatube-sdk-go/model"
	mt "github.com/metatube-community/metatube-sdk-go/provider"
	sdkroute "github.com/metatube-community/metatube-sdk-go/route"
	"github.com/metatube-community/metatube-sdk-go/route/auth"
)

const configRouteProviderName = "MetaXConfigRouteProvider"

type configRouteMovie struct {
	config mt.Config
	calls  int
}

func (*configRouteMovie) Name() string           { return configRouteProviderName }
func (*configRouteMovie) Priority() float64      { return 1 }
func (*configRouteMovie) SetPriority(float64)    {}
func (*configRouteMovie) Language() language.Tag { return language.English }
func (*configRouteMovie) URL() *url.URL {
	return &url.URL{Scheme: "https", Host: "config-route.invalid"}
}
func (*configRouteMovie) NormalizeMovieID(id string) string { return id }
func (*configRouteMovie) ParseMovieIDFromURL(rawURL string) (string, error) {
	return rawURL, nil
}
func (*configRouteMovie) GetMovieInfoByID(string) (*model.MovieInfo, error) {
	return nil, nil
}
func (*configRouteMovie) GetMovieInfoByURL(string) (*model.MovieInfo, error) {
	return nil, nil
}
func (p *configRouteMovie) SetConfig(config mt.Config) error {
	p.config = config
	p.calls++
	return nil
}

var configRouteTestProvider = &configRouteMovie{}

func TestProviderConfigEndpointRequiresAuthentication(t *testing.T) {
	registerConfigRouteProvider()
	router := sdkroute.New(engine.New(nil), auth.Token("secret"))

	request := httptest.NewRequest(http.MethodPut, "/v1/providers/config",
		bytes.NewBufferString(`{"type":"movie","provider":"`+configRouteProviderName+`","config":{"cookies":"x"}}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body: %s", response.Code, http.StatusUnauthorized, response.Body.String())
	}
	if configRouteTestProvider.calls != 0 {
		t.Fatalf("provider config was applied without authentication")
	}
}

func TestProviderConfigEndpointAppliesCookies(t *testing.T) {
	registerConfigRouteProvider()
	configRouteTestProvider.config = nil
	configRouteTestProvider.calls = 0
	router := sdkroute.New(engine.New(nil), auth.Token("secret"))

	request := httptest.NewRequest(http.MethodPut, "/v1/providers/config",
		bytes.NewBufferString(`{"type":"movie","provider":"`+configRouteProviderName+`","config":{"cookies":"session=abc; cf_clearance=def"}}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer secret")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", response.Code, http.StatusOK, response.Body.String())
	}
	var result struct {
		Data struct {
			Type     string `json:"type"`
			Provider string `json:"provider"`
			Applied  bool   `json:"applied"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result.Data.Type != "movie" || result.Data.Provider != configRouteProviderName || !result.Data.Applied {
		t.Fatalf("unexpected response: %+v", result.Data)
	}
	if configRouteTestProvider.calls != 1 || configRouteTestProvider.config == nil {
		t.Fatalf("provider config was not applied")
	}
	value, err := configRouteTestProvider.config.GetString("cookies")
	if err != nil || value != "session=abc; cf_clearance=def" {
		t.Fatalf("cookies = %q, err = %v", value, err)
	}
}

func TestProviderConfigEndpointRejectsUnsafeOrUnknownInput(t *testing.T) {
	registerConfigRouteProvider()
	router := sdkroute.New(engine.New(nil), auth.Token("secret"))

	for name, body := range map[string]string{
		"unknown provider": `{"type":"movie","provider":"MissingProvider","config":{"cookies":"x"}}`,
		"empty config":     `{"type":"movie","provider":"` + configRouteProviderName + `","config":{}}`,
		"unknown field":    `{"type":"movie","provider":"` + configRouteProviderName + `","config":{"cookies":"x"},"extra":true}`,
		"unsupported key":  `{"type":"movie","provider":"` + configRouteProviderName + `","config":{"proxy":"http://127.0.0.1:8080"}}`,
		"header injection": `{"type":"movie","provider":"` + configRouteProviderName + `","config":{"cookies":"x\r\nX-Evil: y"}}`,
		"trailing JSON":    `{"type":"movie","provider":"` + configRouteProviderName + `","config":{"cookies":"x"}}{}`,
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPut, "/v1/providers/config", bytes.NewBufferString(body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Authorization", "Bearer secret")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest && response.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 400 or 404; body: %s", response.Code, response.Body.String())
			}
		})
	}
}

func registerConfigRouteProvider() {
	mt.Register(configRouteProviderName, func() *configRouteMovie {
		return configRouteTestProvider
	})
}
