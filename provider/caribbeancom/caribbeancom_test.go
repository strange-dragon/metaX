package caribbeancom

import (
	"testing"

	"github.com/strange-dragon/metaX/provider/internal/testkit"
)

func TestCaribbeancom_GetMovieInfoByID(t *testing.T) {
	testkit.Test(t, New, []string{
		"050422-001",
		"031222-001",
		"061014-618",
	})
}

func TestCaribbeancom_GetMovieReviewsByID(t *testing.T) {
	testkit.Test(t, New, []string{
		"050422-001",
	})
}
