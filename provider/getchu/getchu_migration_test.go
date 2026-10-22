package getchu

import "testing"

func TestNormalizeMovieIDForDetailLookup(t *testing.T) {
	tests := map[string]string{
		"4042392":        "4042392",
		"GETCHU-4042392": "4042392",
		"getchu_4042392": "4042392",
		"item4042392":    "",
		"4042392/other":  "",
		"GETCHU-ABC":     "",
		"":               "",
	}
	for input, want := range tests {
		if got := New().NormalizeMovieID(input); got != want {
			t.Errorf("NormalizeMovieID(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestGetMovieInfoByIDRejectsInvalidID(t *testing.T) {
	info, err := New().GetMovieInfoByID("GETCHU-ABC")
	if err == nil {
		t.Fatal("expected invalid movie ID to be rejected")
	}
	if info != nil {
		t.Fatalf("expected nil info for invalid ID, got %#v", info)
	}
}

func TestParseMovieIDFromURLValidatesOriginAndPath(t *testing.T) {
	gcu := New()
	id, err := gcu.ParseMovieIDFromURL("https://dl.getchu.com/i/item4042392")
	if err != nil || id != "4042392" {
		t.Fatalf("valid detail URL: id=%q err=%v", id, err)
	}
	for _, raw := range []string{
		"http://dl.getchu.com/i/item4042392",
		"https://dl.getchu.com:444/i/item4042392",
		"https://user@dl.getchu.com/i/item4042392",
		"https://dl.getchu.com.evil.invalid/i/item4042392",
		"https://dl.getchu.com/soft.phtml?id=4042392",
		"https://dl.getchu.com/i/itemabc",
		"https://dl.getchu.com/i/item4042392/",
	} {
		if _, err := gcu.ParseMovieIDFromURL(raw); err == nil {
			t.Errorf("ParseMovieIDFromURL(%q) unexpectedly succeeded", raw)
		}
	}
}
