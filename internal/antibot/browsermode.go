package antibot

import (
	"os"
	"strings"
)

// Browser modes. A provider only ever needs a browser to execute a JavaScript
// challenge, so the mode decides whether one is worth its 200-400 MB.
const (
	// BrowserModeAuto probes the egress once at boot and starts a browser only
	// when a plain request is challenged.
	BrowserModeAuto = "auto"
	// BrowserModeOff never starts a browser.
	BrowserModeOff = "off"
	// BrowserModeAlways starts a browser without asking.
	BrowserModeAlways = "always"
)

// BrowserMode resolves METAX_BROWSER_MODE.
//
// Unrecognised values fall back to auto, the only mode that costs nothing when
// the browser turns out to be unnecessary. METAX_WARMUP_BROWSER=0 is still
// honoured as off, for deployments configured before this variable existed.
func BrowserMode() string {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("METAX_BROWSER_MODE")))
	switch mode {
	case "off", "never", "disable", "disabled", "none":
		return BrowserModeOff
	case "always", "on", "enable", "enabled", "required", "force":
		return BrowserModeAlways
	case "", BrowserModeAuto:
		if os.Getenv("METAX_WARMUP_BROWSER") == "0" {
			return BrowserModeOff
		}
		return BrowserModeAuto
	default:
		return BrowserModeAuto
	}
}
