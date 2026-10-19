package javdb

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// JavDB has used different copy-control element/class combinations over time.
// Detail parsing should depend on the explicit work-number attribute, not the
// full styling class list of one specific HTML layout.
func TestGetMovieInfoByURLAcceptsCopyControlMarkupVariant(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `<!doctype html><html><body>
<h2 class="title is-4"><strong class="current-title">Variant Layout Title</strong></h2>
<button class="copy-to-clipboard" data-clipboard-text="101926_01">Copy</button>
<img class="video-cover" src="/covers/variant.jpg">
</body></html>`)
	}))
	defer server.Close()

	p := newForTest(server.URL, server.Client())
	info, err := p.GetMovieInfoByURL(server.URL + "/v/opaque-id")
	require.NoError(t, err)
	require.NotNil(t, info)
	require.Equal(t, "101926_01", info.Number)
	require.Equal(t, "Variant Layout Title", info.Title)
	require.Equal(t, server.URL+"/covers/variant.jpg", info.CoverURL)
}
