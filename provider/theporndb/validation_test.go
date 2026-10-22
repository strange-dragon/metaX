package theporndb

import "testing"

func TestParseThePornDBDetailID(t *testing.T) {
	tests := []struct {
		name       string
		rawURL     string
		collection string
		want       string
		wantErr    bool
	}{
		{"scene slug", "https://theporndb.net/scenes/i-want-clips-leaking-into-debt", "scenes", "i-want-clips-leaking-into-debt", false},
		{"movie UUID", "https://theporndb.net/movies/ee8cf0e2-55b3-41fa-a7f6-7db83068b2e4", "movies", "ee8cf0e2-55b3-41fa-a7f6-7db83068b2e4", false},
		{"performer numeric ID", "https://theporndb.net/performers/138309/", "performers", "138309", false},
		{"default port and query", "https://theporndb.net:443/scenes/6474100?ref=search", "scenes", "6474100", false},
		{"wrong scheme", "http://theporndb.net/scenes/6474100", "scenes", "", true},
		{"wrong host", "https://theporndb.net.evil.invalid/scenes/6474100", "scenes", "", true},
		{"userinfo", "https://user@theporndb.net/scenes/6474100", "scenes", "", true},
		{"nonstandard port", "https://theporndb.net:8443/scenes/6474100", "scenes", "", true},
		{"wrong collection", "https://theporndb.net/movies/6474100", "scenes", "", true},
		{"extra path", "https://theporndb.net/scenes/6474100/extra", "scenes", "", true},
		{"traversal", "https://theporndb.net/scenes/../6474100", "scenes", "", true},
		{"relative URL", "/scenes/6474100", "scenes", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseThePornDBDetailID(tt.rawURL, tt.collection)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseThePornDBDetailID(%q) expected error", tt.rawURL)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseThePornDBDetailID(%q): %v", tt.rawURL, err)
			}
			if got != tt.want {
				t.Errorf("parseThePornDBDetailID(%q) = %q, want %q", tt.rawURL, got, tt.want)
			}
		})
	}
}

func TestThePornDBVideoGetMovieInfoByIDRejectsInvalidID(t *testing.T) {
	tests := []string{"", "../6474100", "6474100?x=1", "a/b", "two--hyphens"}
	for _, id := range tests {
		t.Run(id, func(t *testing.T) {
			if _, err := NewThePornDBScene().GetMovieInfoByID(id); err == nil {
				t.Fatalf("GetMovieInfoByID(%q) accepted an invalid ID", id)
			}
		})
	}
}

func TestThePornDBActorGetActorInfoByIDRejectsInvalidID(t *testing.T) {
	if _, err := NewThePornDBActor().GetActorInfoByID("../138309"); err == nil {
		t.Fatal("GetActorInfoByID accepted an invalid actor ID")
	}
}
