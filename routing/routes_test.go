package routing

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestDefaultRoutesRouteEveryIntegratedProvider(t *testing.T) {
	config := DefaultConfig()
	want := map[Category][]string{
		CategoryCensored: {
			"FANZA", "JavDB", "JavBus", "MGS", "SOD", "Prestige", "FALENO", "DAHLIA",
			"JavLibrary", "Jav321", "GIGA", "XCity", "AVBASE",
		},
		CategoryUncensored: {
			"JavDB", "JavBus", "Jav321", "AVSOX", "Freejavbt", "FC2PPVDB", "FC2", "FC2Club", "fc2hub",
			"Airav", "HEYZO", "TOKYO-HOT", "1Pondo", "Caribbeancom", "CaribbeancomPR",
			"10musume", "HeyDouga", "KIN8", "DUGA", "AVE", "JAVFREE", "PACOPACOMAMA",
			"MURAMURA", "H0930", "H4610", "C0930", "Gcolle", "Pcolle",
		},
		CategoryChinese: {"IQQTV", "JavDB", "Airav", "Freejavbt", "MadouQu"},
		CategoryAmateur: {
			"MGS", "FANZA", "JavDB", "JavBus", "10musume", "1Pondo", "Caribbeancom",
			"CaribbeancomPR", "PACOPACOMAMA", "MURAMURA", "HeyDouga", "H0930", "H4610",
			"C0930", "Gcolle", "Pcolle", "MYWIFE", "KIN8", "TOKYO-HOT",
		},
		CategoryWestern: {"ThePornDBMovie", "ThePornDBScene", "JavDB", "Freejavbt"},
		CategoryHentai:  {"Getchu", "AniList", "FANZA", "JavDB", "GIGA"},
	}
	if len(config.Routes) != len(want) {
		t.Fatalf("default route categories = %d, want %d", len(config.Routes), len(want))
	}
	for category, expected := range want {
		if got := config.Routes[category]; !reflect.DeepEqual(got, expected) {
			t.Errorf("route %q = %#v, want %#v", category, got, expected)
		}
	}
	if _, exists := config.Routes[CategoryChineseSubtitles]; exists {
		t.Error("legacy chinese_subtitles alias must not duplicate the canonical route")
	}
	if _, exists := config.Routes[CategoryAnimation]; exists {
		t.Error("legacy animation alias must not duplicate the canonical hentai route")
	}
	if err := config.Validate(); err != nil {
		t.Fatalf("default route config is invalid: %v", err)
	}
}

func TestDefaultConfigReturnsIndependentCopies(t *testing.T) {
	first := DefaultConfig()
	first.Routes[CategoryCensored][0] = "Mutated"
	first.Routes[Category("extra")] = []string{"JavDB"}

	second := DefaultConfig()
	if second.Routes[CategoryCensored][0] != "FANZA" {
		t.Fatalf("DefaultConfig() shares its route slices: censored[0] = %q", second.Routes[CategoryCensored][0])
	}
	if _, exists := second.Routes[Category("extra")]; exists {
		t.Fatal("DefaultConfig() shares its route map")
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
		t.Fatalf("Resolve(censored) = %#v, want Amane DMM mapped to metaX FANZA: %#v", got, want)
	}
}

func TestDefaultRoutesResolveAgainstIntegratedProviderInventory(t *testing.T) {
	// Mirrors the in-tree provider inventory registered by engine/register.go on
	// a default build. Amane's Official studio router is not part of the
	// category fan-out model.
	registered := []string{
		"10musume", "1Pondo", "AVE", "AVSOX", "Airav", "AniList", "C0930",
		"Caribbeancom", "CaribbeancomPR", "DAHLIA", "DUGA", "FALENO", "FANZA", "FC2",
		"FC2Club", "FC2PPVDB", "Freejavbt", "GIGA", "Gcolle", "Getchu", "H0930",
		"H4610", "HEYZO", "HeyDouga", "IQQTV", "JAVFREE", "Jav321", "JavBus", "JavDB",
		"JavLibrary", "KIN8", "MGS", "MURAMURA", "MYWIFE", "PACOPACOMAMA", "Pcolle",
		"Prestige", "SOD", "TOKYO-HOT", "ThePornDBMovie", "ThePornDBScene", "XCity", "fc2hub",
	}
	tests := []struct {
		category Category
		want     []string
	}{
		{CategoryCensored, []string{
			"FANZA", "JavDB", "JavBus", "MGS", "SOD", "Prestige", "FALENO", "DAHLIA",
			"JavLibrary", "Jav321", "GIGA", "XCity",
		}},
		{CategoryUncensored, []string{
			"JavDB", "JavBus", "Jav321", "AVSOX", "Freejavbt", "FC2PPVDB", "FC2", "FC2Club", "fc2hub",
			"Airav", "HEYZO", "TOKYO-HOT", "1Pondo", "Caribbeancom", "CaribbeancomPR",
			"10musume", "HeyDouga", "KIN8", "DUGA", "AVE", "JAVFREE", "PACOPACOMAMA",
			"MURAMURA", "H0930", "H4610", "C0930", "Gcolle", "Pcolle",
		}},
		{CategoryChinese, []string{"IQQTV", "JavDB", "Airav", "Freejavbt"}},
		{CategoryAmateur, []string{
			"MGS", "FANZA", "JavDB", "JavBus", "10musume", "1Pondo", "Caribbeancom",
			"CaribbeancomPR", "PACOPACOMAMA", "MURAMURA", "HeyDouga", "H0930", "H4610",
			"C0930", "Gcolle", "Pcolle", "MYWIFE", "KIN8", "TOKYO-HOT",
		}},
		{CategoryWestern, []string{"ThePornDBMovie", "ThePornDBScene", "JavDB", "Freejavbt"}},
		{CategoryHentai, []string{"Getchu", "AniList", "FANZA", "JavDB", "GIGA"}},
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

func TestUncensoredRouteKeepsFC2FamilyAdjacent(t *testing.T) {
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

func TestLegacyCategoryAliasesResolveToCanonicalCategories(t *testing.T) {
	config := DefaultConfig()
	registered := []string{"Getchu", "DMM", "FANZA", "JavDB", "IQQTV", "JavBus", "Airav", "Freejavbt"}

	got, err := config.Resolve(CategoryAnimation, "", registered)
	if err != nil {
		t.Fatalf("Resolve(animation alias) error = %v", err)
	}
	if want := []string{"Getchu", "FANZA", "JavDB"}; !reflect.DeepEqual(got, want) {
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

func TestDefaultRoutesLeaveNoDeferredGaps(t *testing.T) {
	// Every default-build provider must be routed, so the only gaps a default
	// deployment can report are the three experimental entries.
	registered := []string{
		"10musume", "1Pondo", "AVE", "AVSOX", "Airav", "AniList", "C0930",
		"Caribbeancom", "CaribbeancomPR", "DAHLIA", "DUGA", "FALENO", "FANZA", "FC2",
		"FC2Club", "FC2PPVDB", "Freejavbt", "GIGA", "Gcolle", "Getchu", "H0930",
		"H4610", "HEYZO", "HeyDouga", "IQQTV", "JAVFREE", "Jav321", "JavBus", "JavDB",
		"JavLibrary", "KIN8", "MGS", "MURAMURA", "MYWIFE", "PACOPACOMAMA", "Pcolle",
		"Prestige", "SOD", "TOKYO-HOT", "ThePornDBMovie", "ThePornDBScene", "XCity", "fc2hub",
	}
	coverage := DefaultConfig().ProviderCoverage(registered)
	want := ProviderCoverage{Gaps: []ProviderGap{
		{Category: CategoryCensored, Missing: []string{"AVBASE"}},
		{Category: CategoryChinese, Missing: []string{"MadouQu"}},
	}}
	if !reflect.DeepEqual(coverage, want) {
		t.Fatalf("DefaultConfig().ProviderCoverage() = %#v, want %#v", coverage, want)
	}
	if err := DefaultConfig().ValidateRegisteredProviders(registered); err != nil {
		t.Fatalf("ValidateRegisteredProviders() error = %v, want nil", err)
	}
}

func TestProviderCoverageResolvesAliasesAndReportsIntentionalGaps(t *testing.T) {
	config := Config{Routes: map[Category][]string{
		CategoryCensored:   {"DMM", "JavDB", "MissingProvider"},
		CategoryUncensored: {"FC2", "Airav"},
	}}
	coverage := config.ProviderCoverage([]string{"FANZA", "JavDB", "Airav"})
	want := ProviderCoverage{
		Gaps: []ProviderGap{
			{Category: CategoryCensored, Missing: []string{"MissingProvider"}},
			{Category: CategoryUncensored, Missing: []string{"FC2"}},
		},
	}
	if !reflect.DeepEqual(coverage, want) {
		t.Fatalf("ProviderCoverage() = %#v, want %#v", coverage, want)
	}
	if err := config.ValidateRegisteredProviders([]string{"FANZA", "JavDB", "Airav"}); err != nil {
		t.Fatalf("ValidateRegisteredProviders() error = %v, want nil", err)
	}
}

func TestResolveProviderAliases(t *testing.T) {
	tests := []struct {
		name string
		want []string
	}{
		{name: "JavDB", want: []string{"JavDB"}},
		{name: "DMM", want: []string{"DMM", "FANZA"}},
		{name: " dmm ", want: []string{"dmm", "FANZA"}},
		{name: "MGStage", want: []string{"MGStage", "MGS"}},
		{name: "ThePornDB", want: []string{"ThePornDB", "ThePornDBMovie", "ThePornDBScene"}},
		{name: "", want: nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ResolveProviderAliases(test.name); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("ResolveProviderAliases(%q) = %#v, want %#v", test.name, got, test.want)
			}
		})
	}
}

func TestValidateRegisteredProvidersRejectsEmptyCategory(t *testing.T) {
	config := Config{Routes: map[Category][]string{
		CategoryWestern: {"ThePornDB"},
	}}
	err := config.ValidateRegisteredProviders([]string{"JavDB"})
	if !errors.Is(err, ErrNoAvailableProvider) {
		t.Fatalf("ValidateRegisteredProviders() error = %v, want ErrNoAvailableProvider", err)
	}
	if !strings.Contains(err.Error(), "western") {
		t.Fatalf("ValidateRegisteredProviders() error = %q, want western category", err)
	}
}

func TestConfigFingerprintHashIsStableAndOrderSensitive(t *testing.T) {
	first := Config{Routes: map[Category][]string{
		CategoryCensored:   {"JavDB", "DMM"},
		CategoryUncensored: {"JavDB", "FC2"},
	}}
	second := Config{Routes: map[Category][]string{
		CategoryUncensored: {"JavDB", "FC2"},
		CategoryCensored:   {"JavDB", "DMM"},
	}}
	if got := first.FingerprintHash(); len(got) != 64 {
		t.Fatalf("FingerprintHash() length = %d, want 64: %q", len(got), got)
	}
	if first.FingerprintHash() != second.FingerprintHash() {
		t.Fatal("map insertion order changed the fingerprint hash")
	}
	reordered := Config{Routes: map[Category][]string{
		CategoryCensored:   {"DMM", "JavDB"},
		CategoryUncensored: {"JavDB", "FC2"},
	}}
	if first.FingerprintHash() == reordered.FingerprintHash() {
		t.Fatal("provider order must affect the fingerprint hash")
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
