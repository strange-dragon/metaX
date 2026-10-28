// Copyright 2026 metaX contributors.
//
// metax-browserprobe is a diagnostic tool for Cloudflare-protected providers.
// It launches Chromium with a named launch profile, navigates to one URL and
// reports, second by second, what the page is doing plus the fingerprint values
// a bot check inspects. It exists to tell "the site blocks headless browsers"
// apart from "our fetch layer breaks the challenge", which otherwise look the
// same from a provider error message.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"

	"github.com/strange-dragon/metaX/internal/antibot"
)

type fingerprint struct {
	UserAgent        string   `json:"userAgent"`
	UA               Brands   `json:"uaBrands"`
	Platform         string   `json:"platform"`
	Webdriver        any      `json:"webdriver"`
	Languages        []string `json:"languages"`
	Plugins          int      `json:"plugins"`
	HardwareThreads  int      `json:"hardwareConcurrency"`
	DeviceMemory     float64  `json:"deviceMemory"`
	ChromeKeys       []string `json:"chromeKeys"`
	WebGLVendor      string   `json:"webglVendor"`
	WebGLRenderer    string   `json:"webglRenderer"`
	OuterWidth       int      `json:"outerWidth"`
	OuterHeight      int      `json:"outerHeight"`
	ScreenWidth      int      `json:"screenWidth"`
	NotificationPerm string   `json:"notificationPermission"`
	PermissionsState string   `json:"permissionsNotifications"`
}

type Brands []struct {
	Brand   string `json:"brand"`
	Version string `json:"version"`
}

const fingerprintScript = `(() => {
  const out = {
    userAgent: navigator.userAgent,
    uaBrands: (navigator.userAgentData && navigator.userAgentData.brands) || [],
    platform: navigator.platform,
    webdriver: navigator.webdriver,
    languages: navigator.languages,
    plugins: navigator.plugins.length,
    hardwareConcurrency: navigator.hardwareConcurrency,
    deviceMemory: navigator.deviceMemory || 0,
    chromeKeys: Object.keys(window.chrome || {}),
    outerWidth: window.outerWidth,
    outerHeight: window.outerHeight,
    screenWidth: screen.width,
    notificationPermission: Notification.permission,
    permissionsNotifications: "unknown"
  };
  try {
    const canvas = document.createElement("canvas");
    const gl = canvas.getContext("webgl");
    const info = gl && gl.getExtension("WEBGL_debug_renderer_info");
    out.webglVendor = info ? gl.getParameter(info.UNMASKED_VENDOR_WEBGL) : "";
    out.webglRenderer = info ? gl.getParameter(info.UNMASKED_RENDERER_WEBGL) : "";
  } catch (e) { out.webglVendor = "error: " + e; }
  return out;
})()`

func main() {
	var (
		rawURL  = flag.String("url", "", "URL to open")
		timeout = flag.Duration("timeout", 45*time.Second, "total wait")
		mode    = flag.String("mode", "stealth", "launch profile: plain | minimal | uacli | stealth | headed")
		ua      = flag.String("ua", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/156.0.0.0 Safari/537.36", "UA for the minimal/uacli modes")
		dump    = flag.Bool("dump", false, "print the full fingerprint as JSON")
		title   = flag.Bool("title", true, "print the title timeline")
	)
	flag.Parse()
	if *rawURL == "" {
		fmt.Fprintln(os.Stderr, "usage: metax-browserprobe -url <url> [-mode plain|stealth|headed]")
		os.Exit(2)
	}

	opts, cleanup, err := launchOptions(*mode, *ua)
	if err != nil {
		fmt.Fprintf(os.Stderr, "launch options: %v\n", err)
		os.Exit(1)
	}
	defer cleanup()

	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	defer allocCancel()
	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	defer browserCancel()

	start := time.Now()
	if err := chromedp.Run(browserCtx); err != nil {
		fmt.Fprintf(os.Stderr, "start browser: %v\n", err)
		os.Exit(1)
	}
	deadline := start.Add(*timeout)

	actions := []chromedp.Action{network.Enable()}
	switch *mode {
	case "minimal":
		// UA string only: isolates "the UA string is the tell" from "the
		// in-page patches are the tell".
		actions = append(actions, emulation.SetUserAgentOverride(*ua))
	}
	actions = append(actions, chromedp.Navigate(*rawURL))
	if err := chromedp.Run(browserCtx, actions...); err != nil {
		fmt.Fprintf(os.Stderr, "navigate: %v\n", err)
		os.Exit(1)
	}

	lastTitle, lastStatus := "", -1
	for {
		var state struct {
			URL    string `json:"url"`
			Title  string `json:"title"`
			Ready  string `json:"ready"`
			Status int    `json:"status"`
		}
		probeCtx, cancel := context.WithTimeout(browserCtx, 5*time.Second)
		err := chromedp.Run(probeCtx, chromedp.Evaluate(`({url: location.href, title: document.title, ready: document.readyState, status: performance.getEntriesByType("navigation").length ? performance.getEntriesByType("navigation")[0].responseStatus : 0})`, &state))
		cancel()
		if err == nil {
			elapsed := time.Since(start).Round(200 * time.Millisecond)
			if *title && (state.Title != lastTitle || state.Status != lastStatus) {
				fmt.Printf("[%6s] status=%d title=%q ready=%s url=%s\n", elapsed, state.Status, state.Title, state.Ready, truncate(state.URL, 90))
				lastTitle, lastStatus = state.Title, state.Status
			}
			if state.Ready == "complete" && !antibot.IsInterstitialTitle(state.Title) {
				fmt.Printf("[%6s] page settled: title=%q\n", elapsed, state.Title)
				break
			}
		}
		if time.Now().After(deadline) {
			fmt.Printf("[%6s] TIMEOUT still on %q\n", time.Since(start).Round(200*time.Millisecond), lastTitle)
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	if *dump {
		var fp fingerprint
		dumpCtx, cancel := context.WithTimeout(browserCtx, 5*time.Second)
		if err := chromedp.Run(dumpCtx, chromedp.Evaluate(fingerprintScript, &fp)); err != nil {
			fmt.Fprintf(os.Stderr, "fingerprint: %v\n", err)
		} else if raw, err := json.MarshalIndent(fp, "", "  "); err == nil {
			fmt.Println(string(raw))
		}
		cancel()
	}
}

func launchOptions(mode, ua string) ([]chromedp.ExecAllocatorOption, func(), error) {
	switch mode {
	case "uacli":
		// A real UA supplied by the browser itself: no CDP override at all.
		opts, cleanup, err := antibot.BrowserLaunchOptions()
		if err != nil {
			return nil, nil, err
		}
		return append(opts, chromedp.Flag("user-agent", ua), chromedp.Flag("lang", "ja-JP")), cleanup, nil
	case "plain":
		// Stock chromedp defaults: this is what a naive scraper looks like.
		return append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...), func() {}, nil
	case "minimal", "stealth":
		return antibot.BrowserLaunchOptions()
	case "headed":
		opts, cleanup, err := antibot.BrowserLaunchOptions()
		if err != nil {
			return nil, nil, err
		}
		return append(opts, chromedp.Flag("headless", false)), cleanup, nil
	default:
		return nil, nil, fmt.Errorf("unknown mode %q", mode)
	}
}

func truncate(raw string, n int) string {
	if len(raw) <= n {
		return raw
	}
	return strings.TrimRight(raw[:n], "/") + "…"
}
