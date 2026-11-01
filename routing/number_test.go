package routing

import (
	"strings"
	"testing"
)

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

func TestNumberLookupKeysCoverStoredFC2Spellings(t *testing.T) {
	keys := NumberLookupKeys("FC2-3954834")
	want := []string{
		"fc2ppv3954834",
		"fc23954834",
		"fc2_3954834",
		"fc2ppv_3954834",
		"fc2_ppv3954834",
		"fc2_ppv_3954834",
	}
	for _, expected := range want {
		found := false
		for _, key := range keys {
			if key == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("NumberLookupKeys(%q) = %v, missing %q", "FC2-3954834", keys, expected)
		}
	}
}

func TestNumberLookupKeysKeepNonFC2NumbersExact(t *testing.T) {
	keys := NumberLookupKeys(" ABP-123 ")
	if len(keys) == 0 || keys[0] != "abp123" {
		t.Fatalf("NumberLookupKeys(ABP-123) = %v, want abp123 first", keys)
	}
	// No FC2 spelling may be invented for a number that is not FC2.
	for _, key := range keys {
		if strings.HasPrefix(key, "fc2") {
			t.Errorf("NumberLookupKeys(ABP-123) = %v, unexpected FC2 key %q", keys, key)
		}
	}
	if keys := NumberLookupKeys("   "); keys != nil {
		t.Fatalf("NumberLookupKeys(empty) = %v, want nil", keys)
	}
}

// TestNumberLookupKeysCoverSitePrefixedRows covers the rows written before the
// site prefixes were folded out of the number: the stored value is
// "GCOLLE-847256", SQL folding leaves the prefix, so a lookup for the bare ID
// only matches when the prefixed spellings are listed as keys.
func TestNumberLookupKeysCoverSitePrefixedRows(t *testing.T) {
	for _, value := range []string{"847256", "GCOLLE-847256", "GCOLLE_847256"} {
		keys := NumberLookupKeys(value)
		// The SQL pre-filter lowercases and strips hyphens and spaces only.
		want := []string{"847256", "gcolle847256", "gcolle_847256"}
		for _, expected := range want {
			found := false
			for _, key := range keys {
				if key == expected {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("NumberLookupKeys(%q) = %v, missing %q", value, keys, expected)
			}
		}
	}
}

// TestNormalizeNumberFoldsSitePrefixedIDs covers the catalogue names metaX
// prepends to a site-local ID. A file is named after the bare ID, so without the
// fold the merge compared "4037479" with "heydouga4037479", found no match, and
// reported the source as not_found — the title scraped but never merged.
func TestNormalizeNumberFoldsSitePrefixedIDs(t *testing.T) {
	tests := [][2]string{
		{"HEYDOUGA-4037-479", "4037-479"},
		{"heydouga_4037-479", "4037-479"},
		{"MYWIFE-1542", "1542"},
		{"GCOLLE-847256", "847256"},
		{"GCOLLE_847256", "847256"},
		{"PCOLLE-156785614478ab480db", "156785614478ab480db"},
		{"H0930-ori1643", "ori1643"},
		{"C0930-ki220913", "ki220913"},
	}
	for _, pair := range tests {
		if !SameNumber(pair[0], pair[1]) {
			t.Errorf("SameNumber(%q, %q) = false, want true", pair[0], pair[1])
		}
	}
	// A site name without a separator is part of the ID, not a prefix.
	if SameNumber("GCOLLE847256", "847256") {
		t.Error("a site name without a separator must not be stripped")
	}
	// Ordinary catalogue numbers are untouched.
	for _, value := range []string{"ABP-123", "SSIS-001", "010115_001", "FC2-3954834"} {
		if got := NormalizeNumber(value); got != NormalizeNumber(value) || got == "" {
			t.Errorf("NormalizeNumber(%q) = %q, want a stable non-empty fold", value, got)
		}
	}
}
