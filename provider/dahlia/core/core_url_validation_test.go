package core

import (
	"testing"
)

func TestParseMovieIDFromURLValidatesOriginAndPath(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		rawURL  string
		wantID  string
		wantErr bool
	}{
		{"Dahlia valid", "https://dahlia-av.jp/", "https://dahlia-av.jp/works/DLDSS339/", "dldss339", false},
		{"Faleno valid with base path", "https://faleno.jp/top/", "https://faleno.jp/top/works/FNS-257/", "fns-257", false},
		{"wrong host", "https://dahlia-av.jp/", "https://evil.example/works/dldss339/", "", true},
		{"http scheme", "https://dahlia-av.jp/", "http://dahlia-av.jp/works/dldss339/", "", true},
		{"wrong port", "https://dahlia-av.jp/", "https://dahlia-av.jp:444/works/dldss339/", "", true},
		{"wrong path prefix", "https://faleno.jp/top/", "https://faleno.jp/works/fns257/", "", true},
		{"extra path segment", "https://dahlia-av.jp/", "https://dahlia-av.jp/works/dldss339/extra/", "", true},
		{"query", "https://dahlia-av.jp/", "https://dahlia-av.jp/works/dldss339/?next=evil", "", true},
		{"fragment", "https://dahlia-av.jp/", "https://dahlia-av.jp/works/dldss339/#other", "", true},
		{"userinfo", "https://dahlia-av.jp/", "https://dahlia-av.jp@evil.example/works/dldss339/", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			core := &Core{BaseURL: tt.baseURL}
			got, err := core.ParseMovieIDFromURL(tt.rawURL)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseMovieIDFromURL() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && got != tt.wantID {
				t.Errorf("ParseMovieIDFromURL() = %q, want %q", got, tt.wantID)
			}
		})
	}
}
