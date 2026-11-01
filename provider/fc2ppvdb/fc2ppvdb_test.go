package fc2ppvdb

import (
	"errors"
	"testing"

	"github.com/strange-dragon/metaX/provider"
	"github.com/strange-dragon/metaX/provider/internal/testkit"
)

func TestFC2PPVDB_GetMovieInfoByID(t *testing.T) {
	// 2812904 used to be listed here but the catalogue that replaced
	// fc2ppvdb.com never archived it, so it only exercises the not-found path.
	testkit.Test(t, New, []string{
		"4669533",
		"4745474",
		"4137487",
		"4861886",
	})
}

func TestFC2PPVDBReportsAnUnknownIDAsNotFound(t *testing.T) {
	_, err := New().GetMovieInfoByID("2812904")
	if err == nil {
		t.Fatal("GetMovieInfoByID accepted an ID the catalogue does not carry")
	}
	if !errors.Is(err, provider.ErrMovieNotFound) {
		t.Fatalf("GetMovieInfoByID error = %v, want a 404 sentinel", err)
	}
}
