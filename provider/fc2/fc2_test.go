package fc2

import (
	"testing"

	"github.com/strange-dragon/metaX/provider/internal/testkit"
)

func TestFC2_ParseMovieIDFromURLValidation(t *testing.T) {
	fc2 := New()
	valid := []struct {
		url string
		id  string
	}{
		{"https://adult.contents.fc2.com/article/2812904/", "2812904"},
		{"https://adult.contents.fc2.com/article/2812904", "2812904"},
		{"https://adult.contents.fc2.com:443/article/2812904/", "2812904"},
	}
	for _, tt := range valid {
		got, err := fc2.ParseMovieIDFromURL(tt.url)
		if err != nil || got != tt.id {
			t.Errorf("ParseMovieIDFromURL(%q) = %q, %v; want %q", tt.url, got, err, tt.id)
		}
	}

	invalid := []string{
		"http://adult.contents.fc2.com/article/2812904/",
		"https://adult.contents.fc2.com:444/article/2812904/",
		"https://user@adult.contents.fc2.com/article/2812904/",
		"https://adult.contents.fc2.com.evil.invalid/article/2812904/",
		"https://example.invalid/article/2812904/",
		"https://adult.contents.fc2.com/article/../2812904/",
		"https://adult.contents.fc2.com/article/abc/",
		"/article/2812904/",
	}
	for _, rawURL := range invalid {
		if _, err := fc2.ParseMovieIDFromURL(rawURL); err == nil {
			t.Errorf("ParseMovieIDFromURL(%q) unexpectedly succeeded", rawURL)
		}
	}
}

func TestFC2_GetMovieInfoByIDRejectsInvalidID(t *testing.T) {
	for _, id := range []string{"", "../2812904", "2812904?x=1", "https://example.invalid/article/2812904"} {
		if _, err := New().GetMovieInfoByID(id); err == nil {
			t.Errorf("GetMovieInfoByID(%q) unexpectedly succeeded", id)
		}
	}
}

func TestFC2_GetMovieInfoByID(t *testing.T) {
	testkit.Test(t, New, []string{
		"2812904",
		"4669533",
	})
}
