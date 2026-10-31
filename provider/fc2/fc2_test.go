package fc2

import (
	"net/url"
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
		{"https://contents.fc2.com/article/1695912/", "1695912"},
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
		"https://contents.fc2.com.evil.invalid/article/1695912/",
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
		"4996887",
	})
}

func TestIsEKYCURL(t *testing.T) {
	cases := []struct {
		url  string
		want bool
	}{
		{"https://redirect.fc2.com/ekyc_auth/?ref=contents&csms=20261101002200", true},
		{"https://redirect.fc2.com/", true},
		{"https://adult.contents.fc2.com/article/4861886/", false},
		{"https://adult.contents.fc2.com/article/4861886/ekyc_auth", true},
		{"https://contents-thumbnail2.fc2.com/w262h262/storage1.contents.fc2.com/x.jpg", false},
	}
	for _, tt := range cases {
		u, err := url.Parse(tt.url)
		if err != nil {
			t.Fatalf("parse %q: %v", tt.url, err)
		}
		if got := isEKYCURL(u); got != tt.want {
			t.Errorf("isEKYCURL(%q) = %v, want %v", tt.url, got, tt.want)
		}
	}
}

func TestActiveReaderMode(t *testing.T) {
	cases := []struct {
		value string
		want  readerMode
	}{
		{"", readerOnReaderBlock},
		{"on", readerOnReaderBlock},
		{"1", readerOnReaderBlock},
		{"off", readerOff},
		{"0", readerOff},
		{"disabled", readerOff},
		{"force", readerAlways},
		{"always", readerAlways},
		{" FORCE ", readerAlways},
	}
	for _, tt := range cases {
		t.Setenv(readerEnv, tt.value)
		if got := activeReaderMode(); got != tt.want {
			t.Errorf("activeReaderMode(%q) = %v, want %v", tt.value, got, tt.want)
		}
	}
}

func TestReaderURLKeepsTheArticleAddress(t *testing.T) {
	article := "https://adult.contents.fc2.com/article/4861886/"
	if got := readerPrefix + article; got != "https://r.jina.ai/https://adult.contents.fc2.com/article/4861886/" {
		t.Errorf("reader URL = %q", got)
	}
}

func TestUpgradeFC2Artwork(t *testing.T) {
	const (
		full    = "https://storage201000.contents.fc2.com/file/393/39294413/1773182792.93.jpg"
		thumb   = "https://contents-thumbnail2.fc2.com/w262h262/storage201000.contents.fc2.com/file/393/39294413/1773182792.93.jpg"
		foreign = "https://contents-thumbnail2.evil.invalid/w262h262/storage201000.contents.fc2.com/file/393/39294413/1773182792.93.jpg"
	)
	cases := []struct{ name, in, want string }{
		{"list thumbnail becomes the original file", thumb, full},
		{"already full resolution is left alone", full, full},
		{"other hosts are left alone", foreign, foreign},
		{"plain http is left alone", "http://contents-thumbnail2.fc2.com/w262h262/storage201000.contents.fc2.com/file/393/39294413/1773182792.93.jpg", "http://contents-thumbnail2.fc2.com/w262h262/storage201000.contents.fc2.com/file/393/39294413/1773182792.93.jpg"},
		{"missing rendition segment is left alone", "https://contents-thumbnail2.fc2.com/storage201000.contents.fc2.com/file/x.jpg", "https://contents-thumbnail2.fc2.com/storage201000.contents.fc2.com/file/x.jpg"},
		{"a non FC2 storage host is not adopted", "https://contents-thumbnail2.fc2.com/w262h262/evil.invalid/x.jpg", "https://contents-thumbnail2.fc2.com/w262h262/evil.invalid/x.jpg"},
		{"credentials in the path are refused", "https://contents-thumbnail2.fc2.com/w262h262/user@storage201000.contents.fc2.com/file/x.jpg", "https://contents-thumbnail2.fc2.com/w262h262/user@storage201000.contents.fc2.com/file/x.jpg"},
		{"empty stays empty", "", ""},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := upgradeFC2Artwork(tt.in); got != tt.want {
				t.Errorf("upgradeFC2Artwork(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
