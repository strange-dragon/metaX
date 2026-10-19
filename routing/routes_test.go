package routing

import (
	"errors"
	"reflect"
	"testing"
)


func TestDefaultRoutesMatchAmaneContentRoutes(t *testing.T) {
	config := DefaultConfig()
	want := map[Category][]string{
		CategoryCensored:         {"DMM", "JavDB", "JavBus", "Official"},
		CategoryUncensored:       {"JavDB", "JavBus", "AVSOX", "Freejavbt"},
		CategoryFC2:              {"JavDB", "FC2PPVDB", "FC2", "Freejavbt", "Airav"},
		CategoryChineseSubtitles: {"IQQTV", "JavDB", "Airav", "Freejavbt"},
		CategoryAmateur:          {"MGStage", "DMM", "JavDB", "JavBus"},
		CategoryWestern:          {"ThePornDB", "JavDB", "Freejavbt"},
		CategoryHentai:           {"Getchu", "DMM", "JavDB"},
		CategoryAnimation:        {"Getchu", "DMM", "JavDB"},
	}
	for category, expected := range want {
		if got := config.Routes[category]; !reflect.DeepEqual(got, expected) {
			t.Errorf("route %q = %#v, want %#v", category, got, expected)
		}
	}
	if err := config.Validate(); err != nil {
		t.Fatalf("default route config is invalid: %v", err)
	}
}

func TestDefaultRoutesResolveInConfiguredOrderAndSkipUnavailableProviders(t *testing.T) {
	config := DefaultConfig()
	registered := []string{"javdb", "JavBus", "AVSOX", "Freejavbt"}

	got, err := config.Resolve(CategoryUncensored, "", registered)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	want := []string{"javdb", "JavBus", "AVSOX", "Freejavbt"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Resolve() = %#v, want %#v", got, want)
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
