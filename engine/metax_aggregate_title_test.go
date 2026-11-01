package engine

import (
	"testing"

	"github.com/strange-dragon/metaX/model"
)

func TestHasScrapeMark(t *testing.T) {
	cases := []struct {
		title string
		want  bool
	}{
		{"【個人撮影】おじさんの精*の勢いに驚く可愛い制服素人**", true},
		{"【個人撮影】おじさんの精子の勢いに驚く可愛い制服素人女子", false},
		{"★衝撃Ｉカップ★天然爆乳・50歳・結婚14年目", false},
		{"j*y 1本目", false}, // a single asterisk is not a mark
		{"", false},
	}
	for _, tt := range cases {
		t.Run(tt.title, func(t *testing.T) {
			if got := hasScrapeMark(tt.title); got != tt.want {
				t.Errorf("hasScrapeMark(%q) = %v, want %v", tt.title, got, tt.want)
			}
		})
	}
}

func TestPreferUnmarkedTitle(t *testing.T) {
	marked := &model.MovieInfo{Provider: "FC2", Title: "おじさんの精*の勢いに驚く制服素人**"}
	clean := &model.MovieInfo{Provider: "FC2PPVDB", Title: "おじさんの精子の勢いに驚く制服素人女子"}
	alsoMarked := &model.MovieInfo{Provider: "JavDB", Title: "おじさんの精*の勢いに驚く制服素人**"}
	empty := &model.MovieInfo{Provider: "JavBus"}

	t.Run("a marked title takes a sibling's clean spelling", func(t *testing.T) {
		got := preferUnmarkedTitle(marked.Title, []*model.MovieInfo{marked, clean})
		if got != clean.Title {
			t.Errorf("got %q, want %q", got, clean.Title)
		}
	})

	t.Run("a clean title is kept", func(t *testing.T) {
		got := preferUnmarkedTitle(clean.Title, []*model.MovieInfo{clean, marked})
		if got != clean.Title {
			t.Errorf("got %q, want %q", got, clean.Title)
		}
	})

	t.Run("a marked title is kept when every source marks it", func(t *testing.T) {
		got := preferUnmarkedTitle(marked.Title, []*model.MovieInfo{marked, alsoMarked, empty})
		if got != marked.Title {
			t.Errorf("got %q, want the original %q", got, marked.Title)
		}
	})

	t.Run("nil entries are skipped", func(t *testing.T) {
		got := preferUnmarkedTitle(marked.Title, []*model.MovieInfo{nil, empty, clean})
		if got != clean.Title {
			t.Errorf("got %q, want %q", got, clean.Title)
		}
	})
}
