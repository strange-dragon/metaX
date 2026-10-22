package theporndb

import (
	"strings"

	"github.com/strange-dragon/metaX/provider"
)

// ThePornDB is disabled by default, to enable:
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
	return "", nil
}
