package javbus

import (
	"strings"
	"testing"

	"github.com/strange-dragon/metaX/model"
	"github.com/strange-dragon/metaX/provider/internal/testkit"
)

func TestJavBus_ParseMovieIDFromURL(t *testing.T) {
	bus := New()
	tests := []struct {
		name    string
		url     string
		wantID  string
		wantErr bool
	}{
		{name: "root detail", url: "https://www.javbus.com/ssni-776", wantID: "SSNI-776"},
		{name: "Japanese detail", url: "https://www.javbus.com/ja/ssni-776", wantID: "SSNI-776"},
		{name: "canonical host", url: "https://javbus.com/SSNI-776", wantID: "SSNI-776"},
		{name: "reject HTTP downgrade", url: "http://www.javbus.com/SSNI-776", wantErr: true},
		{name: "reject lookalike host", url: "https://www.javbus.com.evil.example/SSNI-776", wantErr: true},
		{name: "reject unexpected port", url: "https://www.javbus.com:8443/SSNI-776", wantErr: true},
		{name: "reject userinfo", url: "https://javbus.com@evil.example/SSNI-776", wantErr: true},
		{name: "reject search path", url: "https://www.javbus.com/ja/search/SSNI-776", wantErr: true},
		{name: "reject relative URL", url: "/SSNI-776", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := bus.ParseMovieIDFromURL(tt.url)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseMovieIDFromURL(%q) error = nil, want error", tt.url)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseMovieIDFromURL(%q) error = %v", tt.url, err)
			}
			if !strings.EqualFold(got, tt.wantID) {
				t.Errorf("ParseMovieIDFromURL(%q) = %q, want %q", tt.url, got, tt.wantID)
			}
		})
	}
}

func TestHasRequiredMovieFields(t *testing.T) {
	tests := []struct {
		title, number string
		want          bool
	}{
		{"Movie", "SSNI-776", true},
		{"", "SSNI-776", false},
		{"Movie", "", false},
		{"  ", "SSNI-776", false},
		{"Movie", "  ", false},
	}
	for _, tt := range tests {
		if got := hasRequiredMovieFields(tt.title, tt.number); got != tt.want {
			t.Errorf("hasRequiredMovieFields(%q, %q) = %v, want %v", tt.title, tt.number, got, tt.want)
		}
	}
}

func TestMovieNumberMatch(t *testing.T) {
	tests := []struct {
		found, query string
		want         int
	}{
		{"SSNI-776", "SSNI-776", 2},
		{"ssni776", "SSNI-776", 2},
		{"SSNI_776", "SSNI-776", 0},
		{"261ARA-423", "ARA-423", 1},
		{"ARA-423", "261ARA-423", 1},
		{"200MIUM-1000", "300MIUM-1000", 0},
		{"ABF-375", "ABF-35", 0},
		{"", "SSNI-776", 0},
	}
	for _, tt := range tests {
		if got := movieNumberMatch(tt.found, tt.query); got != tt.want {
			t.Errorf("movieNumberMatch(%q, %q) = %d, want %d", tt.found, tt.query, got, tt.want)
		}
	}
}

func TestMovieNumberVariants(t *testing.T) {
	tests := []struct {
		number string
		want   []string
	}{
		{"261ARA-423", []string{"261ARA-423", "ARA-423"}},
		{"ARA-423", []string{"ARA-423", "261ARA-423"}},
		{"SSNI-776", []string{"SSNI-776"}},
	}
	for _, tt := range tests {
		got := movieNumberVariants(tt.number)
		if len(got) != len(tt.want) {
			t.Fatalf("movieNumberVariants(%q) = %#v, want %#v", tt.number, got, tt.want)
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("movieNumberVariants(%q) = %#v, want %#v", tt.number, got, tt.want)
				break
			}
		}
	}
}

func TestSelectMovieNumberResult(t *testing.T) {
	result := func(number string) *model.MovieSearchResult {
		return &model.MovieSearchResult{Number: number, Homepage: "/" + number}
	}
	tests := []struct {
		name    string
		results []*model.MovieSearchResult
		query   string
		want    string
	}{
		{"exact beats loose", []*model.MovieSearchResult{result("261ARA-423"), result("ARA-423")}, "ARA-423", "ARA-423"},
		{"unique loose accepted", []*model.MovieSearchResult{result("261ARA-423")}, "ARA-423", "261ARA-423"},
		{"duplicate same URL is still unique", []*model.MovieSearchResult{result("261ARA-423"), result("261ARA-423")}, "ARA-423", "261ARA-423"},
		{"ambiguous loose rejected", []*model.MovieSearchResult{result("261ARA-423"), result("300ARA-423")}, "ARA-423", ""},
		{"wrong number rejected", []*model.MovieSearchResult{result("ABF-375")}, "ABF-35", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := selectMovieNumberResult(tt.results, tt.query)
			if tt.want == "" {
				if len(got) != 0 {
					t.Fatalf("got %#v, want no result", got)
				}
				return
			}
			if len(got) != 1 || got[0].Number != tt.want {
				t.Fatalf("got %#v, want only %q", got, tt.want)
			}
		})
	}
}

func TestJavBus_GetMovieInfoByID(t *testing.T) {
	testkit.Test(t, New, []string{
		"SMBD-77",
		"SSNI-776",
		"ABP-331",
		"CEMD-232",
		"th101-000-110942",
	})
}

func TestJavBus_SearchMovie(t *testing.T) {
	testkit.Test(t, New, []string{
		"SSIS-033",
		"MIDV-005",
	})
}
