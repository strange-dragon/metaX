package getchu

import (
	"testing"

	"github.com/strange-dragon/metaX/provider/internal/testkit"
)

func TestGetchu_GetMovieInfoByID(t *testing.T) {
	testkit.Test(t, New, []string{
		"4018339",
		"4042392",
		"4041955",
		"4042423",
	})
}
