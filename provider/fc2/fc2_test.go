package fc2

import (
	"net/url"
	"strings"
	"testing"

	"github.com/antchfx/htmlquery"
	"golang.org/x/net/html"

	"github.com/strange-dragon/metaX/provider/internal/testkit"
)

// parseHeading is a helper that turns an article heading into the node the
// collector hands the OnXML callback.
func parseHeading(t *testing.T, body string) *html.Node {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("parse body: %v", err)
	}
	return htmlquery.FindOne(doc, "//div")
}

func TestCleanFC2TitleDropsTheWatermarkSpan(t *testing.T) {
	const (
		watermark = `<span style="zoom:0.01;color:#fff;width:1px;height:1px;display:inline-block;overflow:hidden;">**jjyoqz*ys </span>`
		visible   = "【個人撮影】おじさんの精子の勢いに驚く可愛い制服素人女子"
	)
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "the injected watermark is removed",
			body: `<div class="items_article_headerInfo"><h3>【個人撮影】` + watermark + visible + `</h3><ul><li>meta</li></ul></div>`,
			want: "【個人撮影】" + visible,
		},
		{
			name: "a watermark in the middle is removed too",
			body: `<div class="items_article_headerInfo"><h3>前半` + watermark + `後半</h3></div>`,
			want: "前半後半",
		},
		{
			name: "several hidden spans are all removed",
			body: `<div class="items_article_headerInfo"><h3>` + watermark + `タイトル` + watermark + `</h3></div>`,
			want: "タイトル",
		},
		{
			name: "a clean title is untouched",
			body: `<div class="items_article_headerInfo"><h3>` + visible + `</h3></div>`,
			want: visible,
		},
		{
			name: "the text around the span keeps its spacing",
			body: `<div class="items_article_headerInfo"><h3>Ａ　Ｂ` + watermark + `Ｃ</h3></div>`,
			want: "Ａ　ＢＣ",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := cleanFC2Title(parseHeading(t, tt.body)); got != tt.want {
				t.Errorf("cleanFC2Title() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCleanFC2TitleHandlesAMissingHeading(t *testing.T) {
	if got := cleanFC2Title(nil); got != "" {
		t.Errorf("cleanFC2Title(nil) = %q, want empty", got)
	}
	// The fallback path reads the whole element, so a page that changed its
	// heading tag still yields something instead of an empty title.
	body := `<div class="items_article_headerInfo"><h2>替代標題</h2></div>`
	if got := cleanFC2Title(parseHeading(t, body)); got != "替代標題" {
		t.Errorf("cleanFC2Title(no h3) = %q, want %q", got, "替代標題")
	}
}

func TestIsGonePage(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"the Japanese marker", `<title>お探しの商品が見つかりませんでした | FC2コンテンツマーケット</title>`, true},
		{"the English marker", `<p>We couldn't find any products that match your search</p>`, true},
		{"a normal article", `<h3>【個人撮影】タイトル</h3>`, false},
		{"an empty body", ``, false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := isGonePage([]byte(tt.body)); got != tt.want {
				t.Errorf("isGonePage(%q) = %v, want %v", tt.body, got, tt.want)
			}
		})
	}
}

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
