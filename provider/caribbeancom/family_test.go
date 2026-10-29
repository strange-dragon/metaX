package caribbeancom_test

import (
	"testing"

	"github.com/strange-dragon/metaX/common/number"
	"github.com/strange-dragon/metaX/provider/caribbeancom"
	"github.com/strange-dragon/metaX/provider/caribbeancompr"
)

// The two Caribbean brands publish independent release sequences that happen to
// share the same YYMMDD-NNN shape. Verified against JavDB on 2026-10-29:
//
//	010123-001 -> 洗練された大人のいやし亭 ... 姫始め   (caribbeancom.com)
//	010123_001 -> 朝ゴミ出しする近所の遊び好きノーブラ奥さん 今田美玲 (caribbeancompr.com)
//
// Same date, same sequence number, different movies. So the separator is a
// brand selector, not a spelling variant. Neither normalizer may accept the
// other's form: a "helpful" conversion would answer with a different movie's
// metadata, which is worse than answering nothing at all.
func TestSeparatorSelectsBrandAndMustNotBeConverted(t *testing.T) {
	plain := caribbeancom.New()
	premium := caribbeancompr.New()

	cases := []struct {
		name      string
		normalize func(string) string
		id        string
		want      string
	}{
		{"Caribbeancom keeps the dash form", plain.NormalizeMovieID, "010123-001", "010123-001"},
		{"Caribbeancom rejects the underscore form", plain.NormalizeMovieID, "010123_001", ""},
		{"CaribbeancomPR keeps the underscore form", premium.NormalizeMovieID, "010123_001", "010123_001"},
		{"CaribbeancomPR rejects the dash form", premium.NormalizeMovieID, "010123-001", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.normalize(tc.id); got != tc.want {
				t.Fatalf("NormalizeMovieID(%q) = %q, want %q", tc.id, got, tc.want)
			}
		})
	}
}

// The number extractor has to preserve whichever separator the filename used,
// otherwise a Caribbeancom file would arrive at CaribbeancomPR already rewritten
// into the wrong brand's id. common/number builds the id from the captured
// `${pattern}`, so this is a regression guard on that capture.
func TestExtractorPreservesTheBrandSeparator(t *testing.T) {
	plain := caribbeancom.New()
	premium := caribbeancompr.New()

	cases := []struct {
		file     string
		want     string
		accepted func(string) string
	}{
		{"Caribbeancom_020317-001.mp4", "020317-001", plain.NormalizeMovieID},
		{"caribbeancom-020317-001.mp4", "020317-001", plain.NormalizeMovieID},
		{"[Caribbeancom] 020317-001.mp4", "020317-001", plain.NormalizeMovieID},
		{"CaribbeancomPR-020317_001.mp4", "020317_001", premium.NormalizeMovieID},
		{"caribbeancompr_020317_001.mp4", "020317_001", premium.NormalizeMovieID},
	}
	for _, tc := range cases {
		got := number.Trim(tc.file)
		if got != tc.want {
			t.Errorf("number.Trim(%q) = %q, want %q", tc.file, got, tc.want)
			continue
		}
		if accepted := tc.accepted(got); accepted == "" {
			t.Errorf("number.Trim(%q) = %q, which the matching provider rejects", tc.file, got)
		}
	}
}
