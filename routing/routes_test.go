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
		CategoryUncensored: {"JavDB", "JavBus", "AVSOX", "Freejavbt"},
		CategoryFC2:        {"JavDB", "FC2PPVDB", "FC2", "Freejavbt", "Airav"},
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

func TestUncensoredRouteDoesNotIncludeFC2OnlyProviders(t *testing.T) {
	registered := []string{"JavDB", "JavBus", "AVSOX", "Freejavbt", "FC2PPVDB", "FC2", "Airav"}
	got, err := DefaultConfig().Resolve(CategoryUncensored, "", registered)
	if err != nil {
		t.Fatalf("Resolve(uncensored) error = %v", err)
	}
	want := []string{"JavDB", "JavBus", "AVSOX", "Freejavbt"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Resolve(uncensored) = %#v, want %#v", got, want)
	}
}

func TestFC2RouteMatchesAmaneOrder(t *testing.T) {
	registered := []string{"JavDB", "JavBus", "AVSOX", "Freejavbt", "FC2PPVDB", "FC2", "Airav"}
	got, err := DefaultConfig().Resolve(CategoryFC2, "", registered)
	if err != nil {
		t.Fatalf("Resolve(FC2) error = %v", err)
	}
	want := []string{"JavDB", "FC2PPVDB", "FC2", "Freejavbt", "Airav"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Resolve(FC2) = %#v, want Amane FC2 providers %#v", got, want)
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
