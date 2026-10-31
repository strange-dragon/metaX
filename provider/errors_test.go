package provider

import (
	stderrors "errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/strange-dragon/metaX/errors"
)

func TestNotFoundSentinelsCarryHTTPStatus(t *testing.T) {
	// Providers wrap these sentinels with their own prefix. The HTTP layer
	// unwraps them so "the source has no such record" answers 404 instead of
	// an opaque 500.
	for _, sentinel := range []error{ErrMovieNotFound, ErrActorNotFound} {
		var httpErr *errors.HTTPError
		if !stderrors.As(sentinel, &httpErr) {
			t.Fatalf("%v is not HTTP-aware", sentinel)
		}
		if httpErr.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", httpErr.Code, http.StatusNotFound)
		}
	}
}

func TestWrappedNotFoundKeepsProviderMessage(t *testing.T) {
	wrapped := fmt.Errorf("%w: %s", ErrMovieNotFound, "https://airav.io/video/QC-DB-111492")
	if !stderrors.Is(wrapped, ErrMovieNotFound) {
		t.Fatal("wrapped error should still match the sentinel")
	}
	var httpErr *errors.HTTPError
	if !stderrors.As(wrapped, &httpErr) {
		t.Fatal("wrapped error should unwrap to an HTTP error")
	}
	if httpErr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", httpErr.Code, http.StatusNotFound)
	}
	if got := wrapped.Error(); got != "movie not found: https://airav.io/video/QC-DB-111492" {
		t.Fatalf("message = %q", got)
	}
}
