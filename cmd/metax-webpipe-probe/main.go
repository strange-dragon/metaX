// Command metax-webpipe-probe answers one question: can the hosted WebPipe
// fetcher get past the anti-bot wall on a target that our own container cannot
// reach?
//
// It is deliberately standalone. Wiring WebPipe into a provider before knowing
// whether its egress is trusted would mean shipping a code path that may never
// work, and then debugging it through deploys. One command here settles that
// first.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/strange-dragon/metaX/internal/webpipe"
)

func main() {
	var (
		key      = flag.String("key", "", "WebPipe API key (defaults to WEBPIPE_API_KEY)")
		target   = flag.String("url", "", "target URL to fetch")
		useProxy = flag.Bool("proxy", false, "let the service use its residential proxy pool")
		proxyURL = flag.String("proxy-url", "", "send the request through this http(s) proxy instead of the pool")
		show     = flag.Int("show", 400, "bytes of the returned HTML to print")
		timeout  = flag.Duration("timeout", webpipe.DefaultTimeout, "overall request timeout")
		asJSON   = flag.Bool("json", false, "print the full result as JSON")
	)
	flag.Parse()

	if strings.TrimSpace(*target) == "" {
		fail("missing -url")
	}
	apiKey := strings.TrimSpace(*key)
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv("WEBPIPE_API_KEY"))
	}
	if apiKey == "" {
		fail("missing API key: pass -key or set WEBPIPE_API_KEY")
	}

	client := webpipe.New(apiKey)
	client.UseProxy = *useProxy
	client.Proxy = *proxyURL
	client.HTTP.Timeout = *timeout

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	started := time.Now()
	result, err := client.Scrape(ctx, *target)
	elapsed := time.Since(started)

	if err != nil {
		fmt.Printf("FAILED after %s: %v\n", elapsed.Round(time.Millisecond), err)
		os.Exit(1)
	}

	blocked := looksBlocked(result)
	verdict := "PASSED (real page)"
	if blocked {
		verdict = "STILL BLOCKED (challenge page)"
	}

	if *asJSON {
		out, _ := json.MarshalIndent(map[string]any{
			"verdict":    verdict,
			"blocked":    blocked,
			"elapsed_ms": elapsed.Milliseconds(),
			"status":     result.StatusCode,
			"title":      result.Title,
			"bytes":      len(result.RawHTML),
			"source_url": result.SourceURL,
		}, "", "  ")
		fmt.Println(string(out))
		return
	}

	fmt.Printf("verdict:  %s\n", verdict)
	fmt.Printf("elapsed:  %s\n", elapsed.Round(time.Millisecond))
	fmt.Printf("status:   %d\n", result.StatusCode)
	fmt.Printf("title:    %s\n", firstNonEmpty(result.Title, "(none)"))
	fmt.Printf("bytes:    %d\n", len(result.RawHTML))
	if *show > 0 {
		body := result.RawHTML
		if len(body) > *show {
			body = body[:*show] + "..."
		}
		fmt.Printf("\n--- first %d bytes ---\n%s\n", *show, body)
	}
}

// looksBlocked recognises the interstitial rather than trusting the status code:
// Cloudflare serves the challenge with 200, so a healthy code says nothing
// about whether the page behind it was reached.
func looksBlocked(result *webpipe.Result) bool {
	if result.StatusCode != 0 && result.StatusCode != 200 {
		return true
	}
	title := strings.ToLower(result.Title)
	body := result.RawHTML
	markers := []string{
		"just a moment",
		"challenge-platform",
		"cf-browser-verification",
		"__cf_chl_",
		"attention required",
	}
	for _, marker := range markers {
		if strings.Contains(title, marker) || strings.Contains(body, marker) {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func fail(message string) {
	fmt.Fprintln(os.Stderr, "error: "+message)
	os.Exit(2)
}
