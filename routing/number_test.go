package routing

import "testing"

func TestNormalizeNumberMirrorsAmaneFoldNumber(t *testing.T) {
	tests := []struct {
		left, right string
		want        bool
	}{
		{"ABC-123", "abc 123", true},
		{"ABC-123", "ABC123", true},
		{"ABC-123", "ABC-124", false},
		// Amane treats underscores as significant.
		{"010115_001", "010115-001", false},
		{"010115_001", "010115001", false},
		{"010115_001", "010115_001", true},
	}
	for _, tc := range tests {
		if got := SameNumber(tc.left, tc.right); got != tc.want {
			t.Errorf("SameNumber(%q, %q) = %v, want %v", tc.left, tc.right, got, tc.want)
		}
	}
}

func TestNormalizeNumberCanonicalizesFC2Family(t *testing.T) {
	variants := []string{
		"FC2-3954834",
		"FC2PPV3954834",
		"FC2-PPV-3954834",
		"FC2_PPV_3954834",
		"fc2 ppv 3954834",
	}
	for _, variant := range variants {
		if got := NormalizeNumber(variant); got != "fc2ppv3954834" {
			t.Errorf("NormalizeNumber(%q) = %q, want fc2ppv3954834", variant, got)
		}
	}
	if SameNumber("3954834", "FC2-PPV-3954834") {
		t.Error("bare numeric IDs must not be treated as FC2 catalog numbers")
	}
}

func TestSameNumberRejectsEmptyValues(t *testing.T) {
	for _, pair := range [][2]string{{"", ""}, {"ABC-123", ""}, {"", "ABC-123"}} {
		if SameNumber(pair[0], pair[1]) {
			t.Errorf("SameNumber(%q, %q) = true, want false", pair[0], pair[1])
		}
	}
}

func TestCategoryCanonicalMergesFC2IntoUncensored(t *testing.T) {
	tests := map[Category]Category{
		CategoryFC2:              CategoryUncensored,
		CategoryChineseSubtitles: CategoryChinese,
		CategoryAnimation:        CategoryHentai,
		CategoryCensored:         CategoryCensored,
	}
	for input, want := range tests {
		if got := input.Canonical(); got != want {
			t.Errorf("%q.Canonical() = %q, want %q", input, got, want)
		}
	}
}
