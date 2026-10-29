package routing

import (
	"fmt"
	"os"
	"strings"
)

const (
	// EnvRouteProfile selects how much of the provider inventory the default
	// route table exposes:
	//
	//	direct - only providers that answer from a plain datacenter IP
	//	full   - every provider, including the ones that need a Japanese IP
	//	auto   - "full" when METAX_PROVIDER_PROXY is set, otherwise "direct"
	//
	// The zero value (unset) behaves like "auto".
	EnvRouteProfile = "METAX_ROUTE_PROFILE"
	// EnvProviderProxy is read here only to decide the route profile. The
	// engine is what actually hands the proxy to providers; routing must not
	// depend on that package, so the environment is the shared contract.
	EnvProviderProxy = "METAX_PROVIDER_PROXY"
)

const (
	ProfileDirect = "direct"
	ProfileFull   = "full"
	ProfileAuto   = "auto"
)

// directRoutes keeps every provider that answers from an ordinary overseas
// datacenter IP (Koyeb `was`, verified 2026-10-29).
//
// The table deliberately KEEPS the providers that resolve a site-local id but
// have no number search: the engine falls back to GetMovieInfoByID for those,
// and a non-matching number costs one NormalizeMovieID call with no network
// request at all. Dropping them would lose real coverage for free.
//
// What is dropped here fails on the site side, before any parser runs:
//
//   - GeoIP-blocked, 403 for every non-Japanese address (verified from a
//     Shenzhen residential line as well as from the deployment): MGS, SOD,
//     Prestige, FALENO, HEYZO, Caribbeancom, FC2PPVDB, JAVFREE, and the
//     0930 family plus KIN8 and HeyDouga.
//
//     CaribbeancomPR belongs here too, but only on the datacenter side: a
//     Shenzhen residential line gets 200 from caribbeancompr.com while Koyeb's
//     `was` answers 403 on every attempt (5/5, ~0.5s each). The sibling
//     caribbeancom.com is 403 from both. So neither Caribbean brand can serve
//     the direct profile, and keeping PR routed only spends a fan-out slot.
//   - Cloudflare JavaScript challenge: JavLibrary.
//   - Site policy: FC2 now forces redirect.fc2.com/ekyc_auth.
//   - Dead or unreachable: AVSOX, Freejavbt, FC2Club, fc2hub, IQQTV, AVE.
//
// A deployment that sets METAX_PROVIDER_PROXY (a Japanese residential proxy)
// gets DefaultConfig instead, because that proxy unblocks the first group.
var directRoutes = Config{Routes: map[Category][]string{
	CategoryCensored: {
		"JavDB", // broadest coverage, Chinese titles, ~0.9s
		"JavBus",
		"Jav321", // DMM covers, used as the image source
		"FANZA",  // largest catalogue; intermittently region-blocked
		"DAHLIA",
		"GIGA",
	},
	CategoryUncensored: {
		"JavDB",
		"JavBus",
		"Airav",
		"TOKYO-HOT",
		"1Pondo",
		"10musume",
		"DUGA",
		"PACOPACOMAMA",
		"MURAMURA",
		"Gcolle",
		"Pcolle",
	},
	CategoryChinese: {
		"JavDB",
		"Airav",
	},
	CategoryAmateur: {
		"JavDB",
		"JavBus",
		"FANZA",
		"10musume",
		"1Pondo",
		"PACOPACOMAMA",
		"MURAMURA",
		"Gcolle",
		"Pcolle",
		"MYWIFE",
		"TOKYO-HOT",
	},
	CategoryWestern: {
		"ThePornDBMovie",
		"ThePornDBScene",
		"JavDB",
	},
	CategoryHentai: {
		"Getchu",
		"AniList",
		"FANZA",
		"JavDB",
		"GIGA",
	},
}}

// DirectConfig returns a copy of the datacenter-friendly route table.
func DirectConfig() Config {
	return cloneConfig(directRoutes)
}

// RouteProfile resolves EnvRouteProfile against the current environment.
// Unknown values are rejected so a typo cannot silently fall back to a
// different provider set.
func RouteProfile() (string, error) {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv(EnvRouteProfile)))
	switch raw {
	case "":
		return profileForEnvironment(), nil
	case ProfileDirect, ProfileFull:
		return raw, nil
	case ProfileAuto:
		return profileForEnvironment(), nil
	default:
		return "", fmt.Errorf("invalid %s %q: want %s, %s or %s",
			EnvRouteProfile, raw, ProfileAuto, ProfileDirect, ProfileFull)
	}
}

func profileForEnvironment() string {
	if strings.TrimSpace(os.Getenv(EnvProviderProxy)) != "" {
		return ProfileFull
	}
	return ProfileDirect
}

// ConfigForProfile returns the built-in route table for a resolved profile.
func ConfigForProfile(profile string) Config {
	if profile == ProfileFull {
		return DefaultConfig()
	}
	return DirectConfig()
}
