package mgstage

import (
	"testing"

	"github.com/strange-dragon/metaX/provider/internal/testkit"
)

func TestMGS_GetMovieInfoByID(t *testing.T) {
	testkit.Test(t, New, []string{
		"300MAAN-778",
	},
		testkit.FieldsNotEmpty("preview_images", "actors"),
		testkit.FieldsNotEmptyAny("maker", "label", "series"),
		testkit.FieldsNotEmptyAny("preview_video_url", "preview_video_hls_url"),
	)
}

func TestMGS_SearchMovie(t *testing.T) {
	testkit.Test(t, New, []string{
		"ABP-177",
		"200GANA-2701",
		"ABF-228",
	})
}

func TestMGS_GetMovieReviewsByID(t *testing.T) {
	testkit.Test(t, New, []string{
		"200GANA-2701",
		"MAAN-930",
		"300MIUM-973",
		"MAAN-929",
		"ABP-177",
	})
}

func TestMGSNormalizeMovieID(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "300maan-778", want: "300MAAN-778"},
		{input: " ABP-177 ", want: "ABP-177"},
		{input: "../ABP-177", want: ""},
		{input: "ABP-177?x=1", want: ""},
		{input: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := New().NormalizeMovieID(tt.input); got != tt.want {
				t.Errorf("NormalizeMovieID(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestMGSParseMovieIDFromURL(t *testing.T) {
	tests := []struct {
		url     string
		want    string
		wantErr bool
	}{
		{url: "https://www.mgstage.com/product/product_detail/300MAAN-778/", want: "300MAAN-778"},
		{url: "https://www.mgstage.com:443/product/product_detail/ABP-177?ref=search", want: "ABP-177"},
		{url: "http://www.mgstage.com/product/product_detail/ABP-177/", wantErr: true},
		{url: "https://www.mgstage.com:8443/product/product_detail/ABP-177/", wantErr: true},
		{url: "https://user@www.mgstage.com/product/product_detail/ABP-177/", wantErr: true},
		{url: "https://www.mgstage.com.evil.invalid/product/product_detail/ABP-177/", wantErr: true},
		{url: "https://www.mgstage.com/product/product_detail/../ABP-177/", wantErr: true},
		{url: "https://www.mgstage.com/search/cSearch.php?search_word=ABP-177", wantErr: true},
		{url: "/product/product_detail/ABP-177/", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			got, err := New().ParseMovieIDFromURL(tt.url)
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

func TestMGSGetMovieInfoByIDRejectsInvalidID(t *testing.T) {
	if _, err := New().GetMovieInfoByID("../ABP-177"); err == nil {
		t.Fatal("GetMovieInfoByID accepted an invalid movie ID")
	}
}

func TestMGSGetMovieReviewsByIDRejectsInvalidID(t *testing.T) {
	if _, err := New().GetMovieReviewsByID("../ABP-177"); err == nil {
		t.Fatal("GetMovieReviewsByID accepted an invalid movie ID")
	}
}
