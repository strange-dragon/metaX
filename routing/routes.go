// Package routing defines ordered, category-based provider selection.
// It deliberately has no dependency on MetaTube's HTTP or provider packages so
// routing behavior can be tested independently and embedded at the SDK boundary.
package routing

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

type Category string

const (
	CategoryCensored         Category = "censored"
	CategoryUncensored       Category = "uncensored"
	CategoryFC2              Category = "fc2"
	CategoryChinese          Category = "chinese"
	CategoryChineseSubtitles Category = "chinese_subtitles" // Legacy alias for Amane's "chinese".
	CategoryAmateur          Category = "amateur"
	CategoryWestern          Category = "western"
	CategoryHentai           Category = "hentai"
	CategoryAnimation        Category = "animation" // Legacy alias for Amane's "hentai".
)

// Canonical folds legacy or narrower labels onto the category actually used by
// the route table. FC2 is intentionally merged into uncensored: both families
// share one provider chain and the public response reports a single category.
func (c Category) Canonical() Category {
	switch c {
	case CategoryFC2:
		return CategoryUncensored
	case CategoryChineseSubtitles:
		return CategoryChinese
	case CategoryAnimation:
		return CategoryHentai
	default:
		return c
	}
}

var (
	ErrUnknownCategory     = errors.New("unknown content category")
	ErrNoAvailableProvider = errors.New("no registered provider for category")
	ErrProviderUnavailable = errors.New("explicit provider is not registered")
)

// providerAliases bridges Amane source names to equivalent registered MetaTube
// providers without changing Amane's canonical route configuration.
var providerAliases = map[string][]string{
	"dmm": {"FANZA"},
}

// Config maps each Amane content category to providers in preferred execution order.
// Provider names are matched case-insensitively, but returned in their
// registered canonical spelling.
type Config struct {
	Routes map[Category][]string
}

// Fingerprint returns a deterministic representation of the route table. It is
// used by the aggregation cache so that requests evaluated under different
// route configurations never share cached results.
func (c Config) Fingerprint() string {
	categories := make([]string, 0, len(c.Routes))
	for category := range c.Routes {
		categories = append(categories, string(category))
	}
	sort.Strings(categories)

	var builder strings.Builder
	for _, category := range categories {
		builder.WriteString(category)
		builder.WriteByte('=')
		builder.WriteString(strings.Join(c.Routes[Category(category)], ","))
		builder.WriteByte(';')
	}
	return builder.String()
}

// DefaultConfig mirrors Amane's ordered content routes where the SDK has an equivalent.
// This project intentionally merges FC2 and uncensored into one shared route.
func DefaultConfig() Config {
	return Config{Routes: map[Category][]string{
		CategoryCensored:   {"DMM", "JavDB", "JavBus", "Official"},
		CategoryUncensored: {"JavDB", "JavBus", "AVSOX", "Freejavbt", "FC2PPVDB", "FC2", "Airav"},
		CategoryChinese:    {"IQQTV", "JavDB", "Airav", "Freejavbt"},
		CategoryAmateur:    {"MGStage", "DMM", "JavDB", "JavBus"},
		CategoryWestern:    {"ThePornDB", "JavDB", "Freejavbt"},
		CategoryHentai:     {"Getchu", "DMM", "JavDB"},
	}}
}

// Validate checks route configuration for empty categories, empty provider
// names, and duplicate providers within one route.
func (c Config) Validate() error {
	if len(c.Routes) == 0 {
		return errors.New("routing config has no categories")
	}
	for category, providers := range c.Routes {
		if strings.TrimSpace(string(category)) == "" {
			return errors.New("routing config contains an empty category")
		}
		if len(providers) == 0 {
			return fmt.Errorf("category %q has no providers", category)
		}
		seen := make(map[string]struct{}, len(providers))
		for _, provider := range providers {
			key := normalize(provider)
			if key == "" {
				return fmt.Errorf("category %q contains an empty provider", category)
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("category %q contains duplicate provider %q", category, provider)
			}
			seen[key] = struct{}{}
		}
	}
	return nil
}

// Resolve returns the registered providers in configured preference order.
// An explicit provider is a manual override and takes precedence over category
// routing. Legacy category labels are translated to their current Amane category.
func (c Config) Resolve(category Category, explicitProvider string, registered []string) ([]string, error) {
	canonical := make(map[string]string, len(registered))
	for _, name := range registered {
		if key := normalize(name); key != "" {
			canonical[key] = name
		}
	}

	if name := normalize(explicitProvider); name != "" {
		if registeredName, ok := canonical[name]; ok {
			return []string{registeredName}, nil
		}
		for _, alias := range providerAliases[name] {
			if registeredName, ok := canonical[normalize(alias)]; ok {
				return []string{registeredName}, nil
			}
		}
		return nil, fmt.Errorf("%w: %s", ErrProviderUnavailable, explicitProvider)
	}

	category = category.Canonical()

	providers, ok := c.Routes[category]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownCategory, category)
	}

	result := make([]string, 0, len(providers))
	seen := make(map[string]struct{}, len(providers))
	for _, provider := range providers {
		candidates := append([]string{provider}, providerAliases[normalize(provider)]...)
		for _, candidate := range candidates {
			registeredName, exists := canonical[normalize(candidate)]
			if !exists {
				continue
			}
			key := normalize(registeredName)
			if _, duplicate := seen[key]; duplicate {
				break
			}
			seen[key] = struct{}{}
			result = append(result, registeredName)
			break
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrNoAvailableProvider, category)
	}
	return result, nil
}

// InferCategory classifies recognizable catalog-number families using Amane's
// content-type distinctions. It is deliberately identifier-based: directory
// overrides (for example a folder named "Getchu" or "欧美") are unavailable at
// this API boundary and must not be guessed from a title.
func InferCategory(keyword string) (Category, bool) {
	value := strings.ToUpper(strings.TrimSpace(keyword))
	if value == "" || strings.Contains(value, "://") || strings.ContainsAny(value, "/\\") {
		return "", false
	}
	compact := strings.NewReplacer("-", "", "_", "", " ", "", ".", "").Replace(value)

	if strings.HasPrefix(compact, "GETCHU") {
		return CategoryHentai, true
	}
	if strings.HasPrefix(compact, "FC2") {
		number := strings.TrimPrefix(compact, "FC2")
		number = strings.TrimPrefix(number, "PPV")
		if allDigits(number) {
			return CategoryFC2, true
		}
	}
	if strings.HasPrefix(compact, "KIN8TENGOKU") || strings.HasPrefix(compact, "KIN8") {
		number := strings.TrimPrefix(strings.TrimPrefix(compact, "KIN8TENGOKU"), "KIN8")
		if len(number) >= 1 && len(number) <= 4 && allDigits(number) {
			return CategoryWestern, true
		}
	}

	// Amane treats six-digit date/sequence catalog numbers as uncensored; the
	// metaX FC2/uncensored routes intentionally share the same provider chain.
	parts := strings.Split(value, "_")
	if len(parts) == 2 && len(parts[0]) == 6 &&
		(len(parts[1]) == 2 || len(parts[1]) == 3 || len(parts[1]) == 4) &&
		allDigits(parts[0]) && allDigits(parts[1]) {
		return CategoryUncensored, true
	}
	if len(parts) == 2 && len(parts[0]) == 8 &&
		(len(parts[1]) == 2 || len(parts[1]) == 3) &&
		allDigits(parts[0]) && allDigits(parts[1]) {
		return CategoryUncensored, true
	}
	if sixDigitCatalogPattern(value) {
		return CategoryUncensored, true
	}
	if len(value) >= 8 && allDigits(strings.ReplaceAll(value, "-", "")) {
		// Pure numeric catalog IDs do not reliably identify a content family.
		return "", false
	}

	// Type-specific catalog families take precedence over the generic JAV
	// catalog pattern, matching Amane's number-based classification behavior.
	if strings.HasPrefix(compact, "MD") && !strings.HasPrefix(compact, "MDVR") ||
		strings.HasPrefix(compact, "MKY") {
		return CategoryChinese, true
	}
	if strings.HasPrefix(compact, "LUXU") || strings.HasPrefix(compact, "ARA") ||
		strings.HasPrefix(compact, "MIUM") || strings.HasPrefix(compact, "NTK") ||
		strings.HasPrefix(compact, "MAAN") || strings.HasPrefix(compact, "SIRO") ||
		strings.HasPrefix(compact, "300MIUM") {
		return CategoryAmateur, true
	}
	if isUncensoredCatalog(value) || strings.HasPrefix(compact, "TH101") ||
		(len(compact) == 5 && (strings.HasPrefix(compact, "N") || strings.HasPrefix(compact, "K")) && allDigits(compact[1:])) {
		return CategoryUncensored, true
	}
	if amateurNumericPrefixPattern(value) {
		return CategoryAmateur, true
	}

	// Western studio/date codes are not standard Japanese catalog numbers.
	if westernDatePattern(value) {
		return CategoryWestern, true
	}
	// Generic catalog number: standard two-or-more-letter prefix + digits.
	// Unlike the earlier narrow classifier, this allows ordinary censored JAV
	// numbers (e.g. ABC-123) to use Amane's censored route.
	if catalogNumberPattern(value) {
		if amateurNumericPrefixPattern(value) {
			return CategoryAmateur, true
		}
		return CategoryCensored, true
	}
	return "", false
}

func isUncensoredCatalog(value string) bool {
	value = strings.ToUpper(strings.TrimSpace(value))
	// Hyphenated prefixes must keep their boundary (JAV- is uncensored;
	// JAVDB- is not). The compact prefixes below are families whose canonical
	// forms do not require a separator.
	for _, prefix := range []string{
		"BT-", "CT-", "EMP-", "CCDV-", "CWP-", "CWPBD-", "DSAM-", "DRC-",
		"DRG-", "DRGBD-", "GACHI-", "GACHIG-", "JAV-", "LAF-", "LAFBD-",
		"HEYZO-", "KTG-", "KP-", "KG-", "LLDV-", "MCDV-", "MKD-", "MKBD-",
		"MMDV-", "NIP-", "PB-", "PT-", "QE-", "RED-", "RHJ-", "S2M-",
		"SKY-", "SKYHD-", "SMD-", "SMBD-", "SMDV-", "SSDV-", "SSKP-",
		"TRG-", "TS-", "XXX-AV-", "H4610-", "C0930-", "H0930-",
	} {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	for _, prefix := range []string{"HEYDOUGA", "YKB", "BIRD", "BOUGA", "S2M", "MCB3D"} {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

func catalogNumberPattern(value string) bool {
	parts := strings.Split(value, "-")
	if len(parts) != 2 || len(parts[0]) < 2 || len(parts[1]) < 2 {
		return false
	}
	for _, r := range parts[0] {
		if r < 'A' || r > 'Z' {
			return false
		}
	}
	digits := strings.TrimSuffix(parts[1], "Z")
	return allDigits(digits)
}

func amateurNumericPrefixPattern(value string) bool {
	parts := strings.Split(value, "-")
	if len(parts) != 2 || len(parts[1]) < 2 || !allDigits(parts[1]) {
		return false
	}
	prefix := parts[0]
	i := 0
	for i < len(prefix) && prefix[i] >= '0' && prefix[i] <= '9' {
		i++
	}
	if i < 3 || i == len(prefix) {
		return false
	}
	for _, r := range prefix[i:] {
		if r < 'A' || r > 'Z' {
			return false
		}
	}
	return true
}

func sixDigitCatalogPattern(value string) bool {
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == '-' || r == '_' })
	return len(parts) == 2 && len(parts[0]) == 6 && len(parts[1]) >= 2 &&
		len(parts[1]) <= 4 && allDigits(parts[0]) && allDigits(parts[1])
}

func westernDatePattern(value string) bool {
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == '.' || r == '-' })
	return len(parts) >= 4 && len(parts[0]) >= 2 &&
		len(parts[len(parts)-3]) == 2 && len(parts[len(parts)-2]) == 2 && len(parts[len(parts)-1]) == 2 &&
		allDigits(parts[len(parts)-3]) && allDigits(parts[len(parts)-2]) && allDigits(parts[len(parts)-1])
}

func allDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func normalize(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
