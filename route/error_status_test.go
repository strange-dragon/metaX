package route

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/strange-dragon/metaX/provider/airav"
)

// Provider misses are HTTP 404 so a client can skip the title instead of
// treating the response as a server failure. Opaque provider errors stay 500:
// they are the ones worth investigating.
func TestAbortWithErrorMapsProviderMissToNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, tc := range []struct {
		name string
		err  error
		want int
	}{
		{
			name: "provider miss",
			err:  fmt.Errorf("%w: %s", airav.ErrMovieNotFound, "https://airav.io/video/QC-DB-111492"),
			want: http.StatusNotFound,
		},
		{
			name: "provider miss with upstream cause",
			err: fmt.Errorf("%w: QC-DB-111492: %v", airav.ErrMovieNotFound,
				fmt.Errorf("airav: GET https://airav.io/video/QC-DB-111492: unexpected HTTP status 403 Forbidden")),
			want: http.StatusNotFound,
		},
		{
			name: "opaque provider error",
			err:  fmt.Errorf("airav: GET https://airav.io/search_result: dial tcp: i/o timeout"),
			want: http.StatusInternalServerError,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			abortWithError(c, tc.err)
			require.Equal(t, tc.want, recorder.Code)
			require.Contains(t, recorder.Body.String(), fmt.Sprintf(`"code":%d`, tc.want))
		})
	}
}

func TestAbortWithErrorKeepsProviderMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const detailURL = "https://airav.io/video/QC-DB-111492"
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	abortWithError(c, fmt.Errorf("%w: %s", airav.ErrMovieNotFound, detailURL))

	require.Equal(t, http.StatusNotFound, recorder.Code)
	require.Contains(t, recorder.Body.String(), detailURL)
}
