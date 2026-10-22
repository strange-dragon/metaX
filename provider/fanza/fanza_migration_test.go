package fanza

import "testing"

func TestFANZANormalizeMovieIDValidation(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: " JUX00109 ", want: "jux00109"},
		{input: "h_346rebd655", want: "h_346rebd655"},
		{input: "ssis-00964", want: "ssis-00964"},
		{input: "../jux00109", want: ""},
		{input: "jux00109?x=1", want: ""},
		{input: "", want: ""},
	}
	for _, tt := range tests {
		if got := New().NormalizeMovieID(tt.input); got != tt.want {
			t.Errorf("NormalizeMovieID(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFANZAParseMovieIDFromURLValidation(t *testing.T) {
	tests := []struct {
		rawURL  string
		want    string
		wantErr bool
	}{
		{rawURL: "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=jux00109/", want: "jux00109"},
		{rawURL: "https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=h_346rebd655/", want: "h_346rebd655"},
		{rawURL: "https://video.dmm.co.jp/av/content/?id=jux00109", want: "jux00109"},
		{rawURL: "https://video.dmm.co.jp/anime/content/?id=196glod00359", want: "196glod00359"},
		{rawURL: "https://video.dmm.co.jp.evil.invalid/av/content/?id=jux00109", wantErr: true},
		{rawURL: "https://user@video.dmm.co.jp/av/content/?id=jux00109", wantErr: true},
		{rawURL: "https://video.dmm.co.jp:8443/av/content/?id=jux00109", wantErr: true},
		{rawURL: "http://www.dmm.co.jp/digital/videoa/-/detail/=/cid=jux00109/", wantErr: true},
		{rawURL: "https://www.dmm.co.jp/search/=/searchstr=jux00109/", wantErr: true},
		{rawURL: "/digital/videoa/-/detail/=/cid=jux00109/", wantErr: true},
		{rawURL: "https://video.dmm.co.jp/av/content/?id=../jux00109", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.rawURL, func(t *testing.T) {
			got, err := New().ParseMovieIDFromURL(tt.rawURL)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseMovieIDFromURL(%q) error = nil, want error", tt.rawURL)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseMovieIDFromURL(%q) error = %v", tt.rawURL, err)
			}
			if got != tt.want {
				t.Errorf("ParseMovieIDFromURL(%q) = %q, want %q", tt.rawURL, got, tt.want)
			}
		})
	}
}

func TestFANZAGetMovieInfoByIDRejectsInvalidID(t *testing.T) {
	if _, err := New().GetMovieInfoByID("../jux00109"); err == nil {
		t.Fatal("GetMovieInfoByID accepted an invalid content ID")
	}
}
