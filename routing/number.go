package routing

import "strings"

// sitePrefixes are the catalogue names that get prepended to a site-local ID to
// keep it unique across providers: Gcolle stores "847256" as "GCOLLE-847256",
// HeyDouga stores "4037-479" as "HEYDOUGA-4037-479".
//
// A file on disk is named after the bare ID, so a request for "4037-479" folded
// to "4037479" while the provider answered "heydouga4037479". The merge compares
// the two, finds no match, and reports the source as not_found — which is why
// these titles scraped from one provider but never merged with the others.
// Stripping the prefix on both sides is what makes the same work comparable.
var sitePrefixes = []string{
	"heydouga", "mywife", "gcolle", "pcolle",
	"h0930", "h4610", "c0930",
}

// NormalizeNumber folds a catalog number to the key used when deciding whether
// two providers describe the same title.
//
// The base rules mirror Amane's crawler-side fold_number: comparison is
// case-insensitive, hyphens and spaces are insignificant, and underscores are
// significant (Amane treats 010115_001 and 010115-001 as different titles).
//
// On top of that, two families are canonicalized because providers spell the
// same work differently:
//
//   - FC2-123456, FC2PPV123456, FC2-PPV-123456 and FC2_PPV_123456 all fold to
//     fc2ppv123456. This matters because the FC2 and uncensored routes share one
//     provider chain.
//   - the site-prefixed IDs in sitePrefixes drop their prefix, so a filename's
//     bare catalog number matches the provider's stored spelling.
func NormalizeNumber(value string) string {
	folded := stripSitePrefix(value)
	folded = strings.NewReplacer("-", "", " ", "").Replace(folded)
	if digits, ok := fc2ProductDigits(folded); ok {
		return "fc2ppv" + digits
	}
	return folded
}

// stripSitePrefix lowercases and trims value, then removes a leading site name
// and its separator. Underscore and space separators are accepted alongside the
// hyphen so "GCOLLE_847256" folds the same way as "GCOLLE-847256".
func stripSitePrefix(value string) string {
	folded := strings.ToLower(strings.TrimSpace(value))
	for _, name := range sitePrefixes {
		if !strings.HasPrefix(folded, name) {
			continue
		}
		rest := folded[len(name):]
		if rest == "" {
			continue
		}
		switch rest[0] {
		case '-', '_', ' ':
			if trimmed := strings.TrimLeft(rest, "-_ "); trimmed != "" {
				return trimmed
			}
		}
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

// NumberLookupKeys returns the normalized spellings a stored catalog number may
// use for the same title. It is used as a bounded SQL pre-filter before
// SameNumber is applied in Go: SQL lowercases the value and removes hyphens and
// spaces, while the FC2 family also needs its common separator variants.
//
// Rows written before the site prefixes were stripped out store the prefixed
// spelling ("GCOLLE-847256"), and SQL folding leaves that prefix in place, so
// the prefixed variants have to be listed too. Otherwise the DB-first lookup
// misses every such row and falls back to a live search for a number the cache
// has held all along.
func NumberLookupKeys(value string) []string {
	normalized := NormalizeNumber(value)
	if normalized == "" {
		return nil
	}

	seen := map[string]struct{}{normalized: {}}
	keys := []string{normalized}
	add := func(key string) {
		if key == "" {
			return
		}
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}

	// Underscore survives SQL folding, so both separators need a key.
	for _, name := range sitePrefixes {
		add(name + normalized)
		add(name + "_" + normalized)
	}

	if digits, ok := fc2ProductDigits(normalized); ok {
		for _, key := range []string{
			"fc2" + digits,
			"fc2_" + digits,
			"fc2ppv" + digits,
			"fc2ppv_" + digits,
			"fc2_ppv" + digits,
			"fc2_ppv_" + digits,
		} {
			add(key)
		}
	}
	return keys
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
