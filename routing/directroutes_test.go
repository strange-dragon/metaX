package routing_test

import (
	"testing"

	"github.com/strange-dragon/metaX/routing"
)

// blockedProviders fail before any parser runs: GeoIP 403, a Cloudflare
// challenge, or a site that is simply gone. Keeping them in the direct table
// only spends a fan-out slot.
//
// CaribbeancomPR is blocked on the datacenter side only: a Shenzhen residential
// line reaches caribbeancompr.com fine, Koyeb's `was` answers 403 every time.
// The engine runs on the datacenter side, so it belongs here.
//
// FC2 is deliberately absent from this list: the publisher catalogue answers a
// plain IP, and it is the only source that resolves FC2 numbers without JavDB
// (JavDB's FC2 detail pages redirect anonymous clients to /login).
var blockedProviders = []string{
	"MGS", "SOD", "Prestige", "FALENO", "HEYZO", "Caribbeancom", "CaribbeancomPR",
	"FC2PPVDB", "JAVFREE", "JavLibrary",
	"H0930", "H4610", "C0930", "KIN8", "HeyDouga",
	"AVSOX", "Freejavbt", "FC2Club", "fc2hub", "IQQTV", "AVE",
}

// idOnlyProviders resolve a site-local id and have no number search. The engine
// falls back to GetMovieInfoByID for them, and a non-matching number costs zero
// network requests, so they must stay routed.
var idOnlyProviders = []string{
	"1Pondo", "10musume", "PACOPACOMAMA", "MURAMURA",
	"MYWIFE", "TOKYO-HOT", "Pcolle", "Gcolle", "DUGA",
}

func TestRouteProfileFollowsProxyEnvironment(t *testing.T) {
	cases := []struct {
		name   string
		env    string
		proxy  string
		expect string
	}{
		{name: "auto without proxy", env: "", proxy: "", expect: routing.ProfileDirect},
		{name: "auto with proxy", env: "", proxy: "http://u:p@127.0.0.1:8080", expect: routing.ProfileFull},
		{name: "explicit auto without proxy", env: routing.ProfileAuto, proxy: "", expect: routing.ProfileDirect},
		{name: "explicit auto with proxy", env: routing.ProfileAuto, proxy: "socks5://127.0.0.1:1080", expect: routing.ProfileFull},
		{name: "explicit full wins over missing proxy", env: routing.ProfileFull, proxy: "", expect: routing.ProfileFull},
		{name: "explicit direct wins over proxy", env: routing.ProfileDirect, proxy: "http://u:p@127.0.0.1:8080", expect: routing.ProfileDirect},
		{name: "case and spaces are tolerated", env: "  FULL ", proxy: "", expect: routing.ProfileFull},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(routing.EnvRouteProfile, tc.env)
			t.Setenv(routing.EnvProviderProxy, tc.proxy)
			got, err := routing.RouteProfile()
			if err != nil {
				t.Fatalf("RouteProfile(): %v", err)
			}
			if got != tc.expect {
				t.Fatalf("profile = %q, want %q", got, tc.expect)
			}
		})
	}
}

func TestRouteProfileRejectsUnknownValues(t *testing.T) {
	t.Setenv(routing.EnvRouteProfile, "fast")
	if _, err := routing.RouteProfile(); err == nil {
		t.Fatal("RouteProfile() accepted an unknown profile")
	}
}

// TestDirectConfigStaysInsideDefaultConfig keeps the two tables related: the
// direct table may only narrow categories, never invent a provider the full
// table does not contain. Ordering is deliberately free - the direct table
// promotes JavDB to the front because it answers fastest - so this test checks
// membership only.
func TestDirectConfigStaysInsideDefaultConfig(t *testing.T) {
	full := routing.DefaultConfig()
	direct := routing.DirectConfig()
	if err := direct.Validate(); err != nil {
		t.Fatalf("direct route table is invalid: %v", err)
	}
	for category, providers := range direct.Routes {
		fullProviders, ok := full.Routes[category]
		if !ok {
			t.Fatalf("direct table defines category %q that the full table does not", category)
		}
		allowed := make(map[string]struct{}, len(fullProviders))
		for _, provider := range fullProviders {
			allowed[provider] = struct{}{}
		}
		for _, provider := range providers {
			if _, ok := allowed[provider]; !ok {
				t.Fatalf("category %q: provider %q is not in the full table (%v)", category, provider, fullProviders)
			}
		}
	}
}

func TestDirectConfigDropsProvidersThatNeedAJapaneseAddress(t *testing.T) {
	direct := routing.DirectConfig()
	for category, providers := range direct.Routes {
		for _, provider := range providers {
			for _, blocked := range blockedProviders {
				if provider == blocked {
					t.Errorf("category %q still routes %q, which cannot answer without a proxy or does not exist", category, provider)
				}
			}
		}
	}
}

// TestDirectConfigKeepsIDOnlyProviders guards the free coverage: these answer a
// site-local id and reject a foreign number with zero network requests, so
// dropping them would lose metadata without saving any time.
func TestDirectConfigKeepsIDOnlyProviders(t *testing.T) {
	direct := routing.DirectConfig()
	routed := make(map[string]struct{})
	for _, providers := range direct.Routes {
		for _, provider := range providers {
			routed[provider] = struct{}{}
		}
	}
	for _, provider := range idOnlyProviders {
		if _, ok := routed[provider]; !ok {
			t.Errorf("provider %q resolves site-local ids for free but is not routed", provider)
		}
	}
}

// TestDirectConfigKeepsJavDBEverywhere documents the fallback contract: JavDB
// has the broadest number coverage, so every category must be able to reach it.
func TestDirectConfigKeepsJavDBEverywhere(t *testing.T) {
	direct := routing.DirectConfig()
	for category, providers := range direct.Routes {
		has := false
		for _, provider := range providers {
			if provider == "JavDB" {
				has = true
				break
			}
		}
		if !has {
			t.Errorf("category %q has no JavDB fallback: %v", category, providers)
		}
	}
}

// TestDirectConfigRoutesFC2Titles guards the FC2 path: the publisher catalogue
// is the only source that returns an FC2 entry without JavDB, and JavDB now
// sends anonymous FC2 detail requests to /login. An FC2 number infers
// CategoryFC2, which canonicalises to uncensored, so "FC2" has to be in that
// chain or every FC2 scrape degenerates into a JavDB login wall.
func TestDirectConfigRoutesFC2Titles(t *testing.T) {
	category, ok := routing.InferCategory("FC2-4861886")
	if !ok {
		t.Fatal("FC2-4861886 did not infer a category")
	}
	canonical := category.Canonical()
	// Resolve intersects the table with the registered providers, so the test
	// supplies the names this build actually registers.
	registered := []string{"JavDB", "JavBus", "Jav321", "FC2", "Airav"}
	providers, err := routing.DirectConfig().Resolve(canonical, "", registered)
	if err != nil {
		t.Fatalf("resolve %s: %v", canonical, err)
	}
	for _, provider := range providers {
		if provider == "FC2" {
			return
		}
	}
	t.Fatalf("category %q resolves to %v, which cannot serve an FC2 number", canonical, providers)
}

func TestConfigForProfileReturnsMatchingTable(t *testing.T) {
	if got := routing.ConfigForProfile(routing.ProfileFull).Fingerprint(); got != routing.DefaultConfig().Fingerprint() {
		t.Fatalf("full profile fingerprint = %q, want the default table", got)
	}
	if got := routing.ConfigForProfile(routing.ProfileDirect).Fingerprint(); got != routing.DirectConfig().Fingerprint() {
		t.Fatalf("direct profile fingerprint = %q, want the direct table", got)
	}
}
