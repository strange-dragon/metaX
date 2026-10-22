package avbase

import (
	"encoding/json"
	"net/url"
	"testing"

	"github.com/strange-dragon/metaX/provider/internal/testkit"
)

func TestMovieURLForWork(t *testing.T) {
	ab := New()
	tests := []struct {
		name, prefix, workID, want string
	}{
		{"prefixed work", "prestige", "ABP-588", "https://www.avbase.net/works/prestige:ABP-588"},
		{"unprefixed work", "", "SSIS-354", "https://www.avbase.net/works/SSIS-354"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := url.Parse(ab.movieURLForWork(tt.prefix, tt.workID))
			if err != nil {
				t.Fatal(err)
			}
			if got.String() != tt.want {
				t.Errorf("movieURLForWork() = %q, want %q", got.String(), tt.want)
			}
		})
	}
}

func TestAVBase_GetMovieInfoByID(t *testing.T) {
	testkit.Test(t, New, []string{
		"prestige:ABP-588",
		"tameike:MEYD-856",
		"SSIS-354",
		"glory:GVH-186",
	})
}

func TestAVBase_SearchMovie(t *testing.T) {
	testkit.Test(t, New, []string{
		"ABP-588",
		"MEYD-856",
		"SSIS-354",
		"HMN",
	})
}
func TestNormalizeAVBaseNumber(t *testing.T) {
	for _, tt := range []struct {
		input string
		want  string
	}{
		{"ssis-00497", "SSIS497"},
		{"SSIS 497", "SSIS497"},
		{"ABP-00588", "ABP588"},
	} {
		if got := normalizeAVBaseNumber(tt.input); got != tt.want {
			t.Errorf("normalizeAVBaseNumber(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestAVBaseQueryForms(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"SSIS497", []string{"SSIS497", "SSIS-497"}},
		{"SSIS-497", []string{"SSIS-497", "SSIS497"}},
		{"", nil},
	}
	for _, tt := range tests {
		got := avbaseQueryForms(tt.input)
		if len(got) != len(tt.want) {
			t.Fatalf("avbaseQueryForms(%q) = %#v, want %#v", tt.input, got, tt.want)
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("avbaseQueryForms(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
			}
		}
	}
}

func TestPickAVBaseWorkExactAndCollision(t *testing.T) {
	works := []workResponse{
		{Prefix: "label-a", WorkID: "ABP-00588"},
		{Prefix: "label-b", WorkID: "ABP-588"},
		{Prefix: "", WorkID: "ABP-588"},
	}
	got, ok := pickAVBaseWork(works, "ABP588")
	if !ok || got.Prefix != "" {
		t.Fatalf("pickAVBaseWork() = (%q, %v), want unique unprefixed candidate", got.Prefix, ok)
	}

	works = []workResponse{
		{Prefix: "label-a", WorkID: "ABP-588"},
		{Prefix: "label-b", WorkID: "ABP-588"},
	}
	if _, ok := pickAVBaseWork(works, "ABP588"); ok {
		t.Fatal("pickAVBaseWork() unexpectedly selected ambiguous prefixed work")
	}

	var productMatch workResponse
	if err := json.Unmarshal([]byte(`{"prefix":"label-a","work_id":"ABP-588","products":[{"product_id":"ABP-588"}]}`), &productMatch); err != nil {
		t.Fatal(err)
	}
	works = []workResponse{
		productMatch,
		{Prefix: "label-b", WorkID: "ABP-588"},
	}
	got, ok = pickAVBaseWork(works, "ABP588")
	if !ok || got.Prefix != "label-a" {
		t.Fatalf("product-number candidate = (%q, %v), want label-a", got.Prefix, ok)
	}
}
