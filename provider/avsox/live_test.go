package avsox

import (
	"testing"

	"github.com/strange-dragon/metaX/provider/internal/testkit"
)

// These are live tests. They exist because AVSOX is the one provider whose
// liveness cannot be judged from its HTML: avsox.click serves a Vue SPA shell
// with no movie markup, so an HTML probe reads as an empty site and the route
// table once dropped the provider on that reading alone. The metadata lives
// behind /javu/data/api/*, and only a real call proves the site is up.
//
// The numbers below are from providers whose pages are stable and long-lived
// (HEYZO, Caribbeancom), so a failure here means the API moved, not that a
// particular title was delisted.

func TestAVSOX_GetMovieInfoByID(t *testing.T) {
	testkit.Test(t, New, []string{
		"HEYZO-1234",
		"010120-001",
	})
}

func TestAVSOX_SearchMovie(t *testing.T) {
	testkit.Test(t, New, []string{
		"HEYZO-1234",
	})
}
