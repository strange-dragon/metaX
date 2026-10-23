package theporndb

import (
	"strings"

	"github.com/strange-dragon/metaX/provider"
)

// ThePornDB is compiled into the default build, but requires an API token.
// Configure it through the provider setting `access_token` (or `api_token`).
// As a convenience for the Jellyfin plugin, `access_token=<token>` may also be
// supplied in the existing provider cookie field; no extra plugin setting is
// required. Environment/provider configuration remains supported:
// `export MT_ACTOR_PROVIDER_THEPORNDBACTOR__ACCESS_TOKEN=your-token`
// `export MT_MOVIE_PROVIDER_THEPORNDBMOVIE__ACCESS_TOKEN=your-token`
// `export MT_MOVIE_PROVIDER_THEPORNDBSCENE__ACCESS_TOKEN=your-token`

const Priority = 1000

func init() {
	provider.Register(SceneProviderName, NewThePornDBScene)
	provider.Register(MovieProviderName, NewThePornDBMovie)
	provider.Register(ActorProviderName, NewThePornDBActor)
}

// accessTokenFromConfig reads the shared ThePornDB API token. Both the SDK's
// documented ACCESS_TOKEN key and Amane's api_token name are accepted; provider
// configuration keys are case-insensitive.
func accessTokenFromConfig(config provider.Config) (string, error) {
	if config == nil {
		return "", nil
	}
	for _, key := range []string{"access_token", "api_token"} {
		if !config.Has(key) {
			continue
		}
		token, err := config.GetString(key)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(token), nil
	}

	// The Jellyfin plugin only exposes provider cookies. Allow a token to be
	// supplied there as `access_token=<token>` (or `api_token=<token>`); the
	// same value is harmless when installed as a transport cookie.
	for _, key := range []string{"cookies", "cookie"} {
		if !config.Has(key) {
			continue
		}
		raw, err := config.GetString(key)
		if err != nil {
			return "", err
		}
		for _, part := range strings.Split(raw, ";") {
			name, value, ok := strings.Cut(strings.TrimSpace(part), "=")
			if !ok {
				continue
			}
			switch strings.ToLower(strings.TrimSpace(name)) {
			case "access_token", "api_token":
				if token := strings.TrimSpace(value); token != "" {
					return token, nil
				}
			}
		}
	}
	return "", nil
}
