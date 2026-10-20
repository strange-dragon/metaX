package routing

import (
	"errors"
	"reflect"
	"testing"
)

func TestDefaultRoutesMatchAmaneContentRoutes(t *testing.T) {
	config := DefaultConfig()
	want := map[Category][]string{
		CategoryCensored:   {"DMM", "JavDB", "JavBus", "Official"},
		CategoryUncensored: {"JavDB", "JavBus", "AVSOX", "Freejavbt", "FC2PPVDB", "FC2", "Airav"},
		CategoryChinese:    {"IQQTV", "JavDB", "Airav", "Freejavbt"},
		CategoryAmateur:    {"MGStage", "DMM", "JavDB", "JavBus"},
		CategoryWestern:    {"ThePornDB", "JavDB", "Freejavbt"},
		CategoryHentai:     {"Getchu", "DMM", "JavDB"},
	}
	for category, expected := range want {
		if got := config.Routes[category]; !reflect.DeepEqual(got, expected) {
			t.Errorf("route %q = %#v, want %#v", category, got, expected)
		}
	}
	if _, exists := config.Routes[CategoryChineseSubtitles]; exists {
		t.Error("legacy chinese_subtitles alias must not duplicate Amane's canonical route")
	}
	if _, exists := config.Routes[CategoryAnimation]; exists {
		t.Error("legacy animation alias must not duplicate Amane's canonical hentai route")
	}
	if err := config.Validate(); err != nil {
		t.Fatalf("default route config is invalid: %v", err)
	}
}

func TestDMMRouteResolvesToRegisteredFANZAProvider(t *testing.T) {
	registered := []string{"FANZA", "JavDB", "JavBus"}
	got, err := DefaultConfig().Resolve(CategoryCensored, "", registered)
	if err != nil {
		t.Fatalf("Resolve(censored) error = %v", err)
	}
	want := []string{"FANZA", "JavDB", "JavBus"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Resolve(censored) = %#v, want Amane DMM mapped to MetaTube FANZA: %#v", got, want)
	}
}

func TestDefaultRoutesResolveAgainstIntegratedProviderInventory(t *testing.T) {
	// Mirrors the pinned SDK providers plus the native providers integrated by
	// build-metatube-server.sh. Official is intentionally absent: Amane's
	// Official entry is a studio-aware routing layer, not a registered scraper.
	registered := []string{
		"FANZA", "JavDB", "JavBus", "AVSOX", "Freejavbt", "FC2PPVDB",
		"FC2", "Airav", "IQQTV", "MGStage", "ThePornDB", "Getchu",
	}
	tests := []struct {
		category Category
		want     []string
	}{
		{CategoryCensored, []string{"FANZA", "JavDB", "JavBus"}},
		{CategoryUncensored, []string{"JavDB", "JavBus", "AVSOX", "Freejavbt", "FC2PPVDB", "FC2", "Airav"}},
		{CategoryChinese, []string{"IQQTV", "JavDB", "Airav", "Freejavbt"}},
		{CategoryAmateur, []string{"MGStage", "FANZA", "JavDB", "JavBus"}},
		{CategoryWestern, []string{"ThePornDB", "JavDB", "Freejavbt"}},
		{CategoryHentai, []string{"Getchu", "FANZA", "JavDB"}},
	}
	config := DefaultConfig()
	for _, tc := range tests {
		t.Run(string(tc.category), func(t *testing.T) {
			got, err := config.Resolve(tc.category, "", registered)
			if err != nil {
				t.Fatalf("Resolve(%s) error = %v", tc.category, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Resolve(%s) = %#v, want %#v", tc.category, got, tc.want)
			}
		})
	}
}

func TestExplicitDMMSelectionResolvesToFANZA(t *testing.T) {
	got, err := DefaultConfig().Resolve(CategoryCensored, "DMM", []string{"FANZA", "JavDB"})
	if err != nil {
		t.Fatalf("Resolve(explicit DMM) error = %v", err)
	}
	if want := []string{"FANZA"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Resolve(explicit DMM) = %#v, want %#v", got, want)
	}
}

func TestUncensoredRouteMatchesAmaneDefaults(t *testing.T) {
	registered := []string{"JavDB", "JavBus", "AVSOX", "Freejavbt", "FC2PPVDB", "FC2", "Airav"}
	got, err := DefaultConfig().Resolve(CategoryUncensored, "", registered)
	if err != nil {
		t.Fatalf("Resolve(uncensored) error = %v", err)
	}
	want := []string{"JavDB", "JavBus", "AVSOX", "Freejavbt", "FC2PPVDB", "FC2", "Airav"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Resolve(uncensored) = %#v, want merged FC2 providers %#v", got, want)
	}
}

func TestLegacyFC2CategoryResolvesToMergedUncensoredRoute(t *testing.T) {
	registered := []string{"JavDB", "JavBus", "AVSOX", "Freejavbt", "FC2PPVDB", "FC2", "Airav"}
	got, err := DefaultConfig().Resolve(CategoryFC2, "", registered)
	if err != nil {
		t.Fatalf("Resolve(FC2 legacy alias) error = %v", err)
	}
	want := []string{"JavDB", "JavBus", "AVSOX", "Freejavbt", "FC2PPVDB", "FC2", "Airav"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Resolve(FC2 legacy alias) = %#v, want merged uncensored providers %#v", got, want)
	}
}

func TestLegacyCategoryAliasesResolveToAmaneCategories(t *testing.T) {
	config := DefaultConfig()
	registered := []string{"Getchu", "DMM", "JavDB", "IQQTV", "JavBus", "Airav", "Freejavbt"}

	got, err := config.Resolve(CategoryAnimation, "", registered)
	if err != nil {
		t.Fatalf("Resolve(animation alias) error = %v", err)
	}
	if want := []string{"Getchu", "DMM", "JavDB"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Resolve(animation alias) = %#v, want %#v", got, want)
	}

	got, err = config.Resolve(CategoryChineseSubtitles, "", registered)
	if err != nil {
		t.Fatalf("Resolve(chinese_subtitles alias) error = %v", err)
	}
	if want := []string{"IQQTV", "JavDB", "Airav", "Freejavbt"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Resolve(chinese_subtitles alias) = %#v, want %#v", got, want)
	}
}

func TestResolveUsesExplicitProviderAsOverride(t *testing.T) {
	got, err := DefaultConfig().Resolve(CategoryCensored, "jAvDb", []string{"JavDB", "JavBus"})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if want := []string{"JavDB"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Resolve() = %#v, want %#v", got, want)
	}
}

func TestResolveRejectsUnknownCategory(t *testing.T) {
	_, err := DefaultConfig().Resolve(Category("unknown"), "", []string{"JavDB"})
	if !errors.Is(err, ErrUnknownCategory) {
		t.Fatalf("Resolve() error = %v, want ErrUnknownCategory", err)
	}
}

func TestResolveReportsWhenNoProviderForCategoryIsRegistered(t *testing.T) {
	_, err := DefaultConfig().Resolve(CategoryWestern, "", []string{"Airav", "IQQTV"})
	if !errors.Is(err, ErrNoAvailableProvider) {
		t.Fatalf("Resolve() error = %v, want ErrNoAvailableProvider", err)
	}
}

func TestResolveRejectsUnavailableExplicitProvider(t *testing.T) {
	_, err := DefaultConfig().Resolve(CategoryCensored, "DMM", []string{"JavDB"})
	if !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("Resolve() error = %v, want ErrProviderUnavailable", err)
	}
}

func TestConfigValidateRejectsDuplicates(t *testing.T) {
	config := Config{Routes: map[Category][]string{
		CategoryCensored: {"JavDB", " javdb "},
	}}
	if err := config.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want duplicate provider error")
	}
}

func TestInferCategoryRecognizesAmaneNumberFamilies(t *testing.T) {
	tests := []struct {
		keyword string
		want    Category
	}{
		{keyword: "fc2-ppv3954834", want: CategoryFC2},
		{keyword: "FC2PPV3954834", want: CategoryFC2},
		{keyword: "fc2-ppv-3954834", want: CategoryFC2},
		{keyword: "101926_01", want: CategoryUncensored},
		{keyword: "KIN8-1234", want: CategoryWestern},
		{keyword: "kin8tengoku_42", want: CategoryWestern},
		{keyword: "ABC-123", want: CategoryCensored},
		{keyword: "MD-1234", want: CategoryChinese},
		{keyword: "MKY-ABC-123", want: CategoryChinese},
		{keyword: "200GANA-123", want: CategoryAmateur},
		{keyword: "HEYZO-1234", want: CategoryUncensored},
		{keyword: "JAV-123", want: CategoryUncensored},
		{keyword: "JAVDB-123", want: CategoryCensored},
		{keyword: "Vixen.24.01.02", want: CategoryWestern},
		{keyword: "Getchu-12345", want: CategoryHentai},
	}
	for _, tc := range tests {
		t.Run(tc.keyword, func(t *testing.T) {
			got, ok := InferCategory(tc.keyword)
			if !ok || got != tc.want {
				t.Fatalf("InferCategory(%q) = (%q, %v), want (%q, true)", tc.keyword, got, ok, tc.want)
			}
		})
	}
}

func TestInferCategoryLeavesAmbiguousNumbersUnclassified(t *testing.T) {
	for _, keyword := range []string{
		"", "unclassified", "123456789", "https://example.invalid/movie/123",
	} {
		if got, ok := InferCategory(keyword); ok {
			t.Errorf("InferCategory(%q) = (%q, true), want unclassified", keyword, got)
		}
	}
}

func TestConfigFingerprintIsDeterministicAndOrderSensitive(t *testing.T) {
	first := Config{Routes: map[Category][]string{
		CategoryCensored:   {"JavDB", "DMM"},
		CategoryUncensored: {"JavDB", "FC2"},
	}}
	second := Config{Routes: map[Category][]string{
		CategoryUncensored: {"JavDB", "FC2"},
		CategoryCensored:   {"JavDB", "DMM"},
	}}
	if first.Fingerprint() != second.Fingerprint() {
		t.Fatalf("map insertion order changed the fingerprint: %q != %q",
			first.Fingerprint(), second.Fingerprint())
	}

	reordered := Config{Routes: map[Category][]string{
		CategoryCensored:   {"DMM", "JavDB"},
		CategoryUncensored: {"JavDB", "FC2"},
	}}
	if first.Fingerprint() == reordered.Fingerprint() {
		t.Fatal("provider order must affect the fingerprint")
	}

	if first.Fingerprint() == DefaultConfig().Fingerprint() {
		t.Fatal("different route tables must not share a fingerprint")
	}
}

func TestParseCategoryCanonicalizesAliasesAndRejectsUnknown(t *testing.T) {
	tests := []struct {
		input   string
		want    Category
		wantErr bool
	}{
		{input: "censored", want: CategoryCensored},
		{input: " CENSORED ", want: CategoryCensored},
		{input: "fc2", want: CategoryUncensored},
		{input: "chinese_subtitles", want: CategoryChinese},
		{input: "animation", want: CategoryHentai},
		{input: "bogus", wantErr: true},
		{input: "", wantErr: true},
	}
	for _, tc := range tests {
		got, err := ParseCategory(tc.input)
		if tc.wantErr {
			if !errors.Is(err, ErrUnknownCategory) {
				t.Errorf("ParseCategory(%q) error = %v, want ErrUnknownCategory", tc.input, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseCategory(%q) error = %v", tc.input, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseCategory(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
