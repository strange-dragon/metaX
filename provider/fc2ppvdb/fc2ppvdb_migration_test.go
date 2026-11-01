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
		{url: "https://ppvdatabank.com/article/2812904/", want: "2812904"},
		{url: "https://ppvdatabank.com/article/4669533/?ref=search", want: "4669533"},
		{url: "https://ppvdatabank.com/article/2812904", want: "2812904"},
		{url: "http://ppvdatabank.com/article/2812904/", wantErr: true},
		{url: "https://ppvdatabank.com:8443/article/2812904/", wantErr: true},
		{url: "https://user@ppvdatabank.com/article/2812904/", wantErr: true},
		{url: "https://ppvdatabank.com.evil.invalid/article/2812904/", wantErr: true},
		{url: "https://fc2ppvdb.com/articles/2812904", wantErr: true}, // the retired host
		{url: "https://ppvdatabank.com/article/abc/", wantErr: true},
		{url: "https://ppvdatabank.com/article/2812904/extra", wantErr: true},
		{url: "https://ppvdatabank.com/actresses/2812904", wantErr: true},
		{url: "/article/2812904/", wantErr: true},
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
