package engine

import "testing"

func TestWithFC2Prefix(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"a bare seven-digit publisher ID gains the prefix", "5001937", "FC2-5001937"},
		{"an already prefixed number is untouched", "FC2-5001937", "FC2-5001937"},
		{"the PPV spelling is untouched", "FC2PPV-5001937", "FC2PPV-5001937"},
		{"lowercase prefixes are recognised", "fc2-5001937", "fc2-5001937"},
		{"a censored catalog number is untouched", "ABC-123", "ABC-123"},
		{"an eight-digit run is left alone", "20250101", "20250101"},
		{"a dated uncensored number is untouched", "101926_01", "101926_01"},
		{"empty stays empty", "", ""},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := withFC2Prefix(tt.in); got != tt.want {
				t.Errorf("withFC2Prefix(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
