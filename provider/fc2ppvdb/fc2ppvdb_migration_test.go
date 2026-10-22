package fc2ppvdb

import "testing"

func TestFC2PPVDBNormalizeMovieID(t *testing.T) {
	p := New()
	tests := []struct {
		input string
		want  string
	}{
		{input: "2812904", want: "2812904"},
		{input: "FC2-2812904", want: "2812904"},
		{input: " fc2_ppv_2812904 ", want: "2812904"},
		{input: "../2812904", want: ""},
		{input: "2812904?x=1", want: ""},
		{input: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := p.NormalizeMovieID(tt.input); got != tt.want {
				t.Errorf("NormalizeMovieID(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestFC2PPVDBParseMovieIDFromURL(t *testing.T) {
	p := New()
	tests := []struct {
		url     string
		want    string
		wantErr bool
	}{
		{url: "https://fc2ppvdb.com/articles/2812904", want: "2812904"},
		{url: "https://fc2ppvdb.com/articles/4669533/?ref=search", want: "4669533"},
		{url: "http://fc2ppvdb.com/articles/2812904", wantErr: true},
		{url: "https://fc2ppvdb.com:8443/articles/2812904", wantErr: true},
		{url: "https://user@fc2ppvdb.com/articles/2812904", wantErr: true},
		{url: "https://fc2ppvdb.com.evil.invalid/articles/2812904", wantErr: true},
		{url: "https://fc2ppvdb.com/articles/abc", wantErr: true},
		{url: "https://fc2ppvdb.com/articles/2812904/extra", wantErr: true},
		{url: "https://fc2ppvdb.com/actresses/2812904", wantErr: true},
		{url: "/articles/2812904", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			got, err := p.ParseMovieIDFromURL(tt.url)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseMovieIDFromURL(%q) error = nil, want error", tt.url)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseMovieIDFromURL(%q) error = %v", tt.url, err)
			}
			if got != tt.want {
				t.Errorf("ParseMovieIDFromURL(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestFC2PPVDBGetMovieInfoByIDRejectsInvalidID(t *testing.T) {
	if _, err := New().GetMovieInfoByID("../2812904"); err == nil {
		t.Fatal("GetMovieInfoByID accepted an invalid movie ID")
	}
}
