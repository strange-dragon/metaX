package routing

import "strings"

// NormalizeNumber folds a catalog number to the key used when deciding whether
// two providers describe the same title.
//
// The base rules mirror Amane's crawler-side fold_number: comparison is
// case-insensitive, hyphens and spaces are insignificant, and underscores are
// significant (Amane treats 010115_001 and 010115-001 as different titles).
//
// On top of that, the FC2 family is canonicalized because providers spell the
// same product differently: FC2-123456, FC2PPV123456, FC2-PPV-123456 and
// FC2_PPV_123456 all fold to fc2ppv123456. This matters for metaX because the
// FC2 and uncensored routes are merged into one provider chain.
func NormalizeNumber(value string) string {
	folded := strings.ToLower(strings.TrimSpace(value))
	folded = strings.NewReplacer("-", "", " ", "").Replace(folded)
	if digits, ok := fc2ProductDigits(folded); ok {
		return "fc2ppv" + digits
	}
	return folded
}

// SameNumber reports whether two catalog numbers identify the same title.
// Empty values never match.
func SameNumber(left, right string) bool {
	if strings.TrimSpace(left) == "" || strings.TrimSpace(right) == "" {
		return false
	}
	return NormalizeNumber(left) == NormalizeNumber(right)
}

// fc2ProductDigits extracts the numeric FC2 product ID from a folded number.
// Underscores may still be present at this point (FC2_PPV_123456 folds to
// fc2_ppv_123456 without touching underscores).
func fc2ProductDigits(folded string) (string, bool) {
	compact := strings.ReplaceAll(folded, "_", "")
	if !strings.HasPrefix(compact, "fc2") {
		return "", false
	}
	rest := strings.TrimPrefix(compact, "fc2")
	rest = strings.TrimPrefix(rest, "ppv")
	if rest == "" || !allDigits(rest) {
		return "", false
	}
	return rest, true
}
