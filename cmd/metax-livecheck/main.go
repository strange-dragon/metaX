// Command metax-livecheck probes registered movie providers against the live
// sites and reports which provider, at which stage, fails.
//
// It is a diagnostic tool: it never writes fixtures and never claims success
// for a provider whose live site is unreachable from the current network.
//
// Usage:
//
//	metax-livecheck -providers jav321, javbus
//	metax-livecheck -id SSIS-033 -providers jav321 -verbose
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/strange-dragon/metaX/engine"
	"github.com/strange-dragon/metaX/internal/antibot"
	mt "github.com/strange-dragon/metaX/provider"
)

// defaultSamples maps provider name to a catalog number that the provider is
// expected to carry. Values are point-in-time choices; override with -id or
// the -samples flag when a sample has been removed upstream.
var defaultSamples = map[string]string{
	"10musume":         "010123_01",
	"1pondo":           "010123_001",
	"airav":            "ARA-001",
	"caribbeancom":     "010123-001",
	"caribbeancompr":   "010123_001",
	"dahlia":           "dldss325",
	"duga":             "SSIS-033",
	"faleno":           "FSDSS-001",
	"fanza":            "ssis00123",
	"fc2":              "FC2-PPV-1234567",
	"fc2club":          "FC2-PPV-1234567",
	"fc2hub":           "FC2-PPV-1234567",
	"fc2ppvdb":         "FC2-PPV-1234567",
	"freejavbt":        "SSIS-033",
	"gcolle":           "SSIS-033",
	"getchu":           "SSIS-033",
	"giga":             "SSIS-033",
	"h0930":            "SSIS-033",
	"h4610":            "SSIS-033",
	"heydouga":         "SSIS-033",
	"heyzo":            "HEYZO-0001",
	"iqqtv":            "SSIS-033",
	"jav321":           "SSIS-033",
	"javbus":           "SSIS-033",
	"javdb":            "SSIS-033",
	"javfree":          "SSIS-033",
	"javlibrary":       "SSIS-033",
	"kin8tengoku":      "SSIS-033",
	"mgstage":          "300MIUM-001",
	"minnano":          "SSIS-033",
	"muramura":         "SSIS-033",
	"mywife":           "SSIS-033",
	"pacopacomama":     "SSIS-033",
	"pcolle":           "SSIS-033",
	"prestige":         "ABP-123",
	"sod":              "STARS-001",
	"tokyo-hot":        "n1234",
	"aventertainments": "SSIS-033",
	"avsox":            "SSIS-033",
	"av-league":        "SSIS-033",
	"c0930":            "SSIS-033",
	"madouqu":          "SSIS-033",
}

type result struct {
	provider string
	stage    string
	elapsed  time.Duration
	number   string
	title    string
	err      error
}

func main() {
	var (
		names     = flag.String("providers", "", "comma separated provider names (default: all movie providers)")
		id        = flag.String("id", "", "override the sample number for every provider")
		timeout   = flag.Duration("timeout", 30*time.Second, "per-provider timeout")
		verbose   = flag.Bool("verbose", false, "print full error chains")
		jsonOut   = flag.Bool("json", false, "print one JSON object per provider")
		rawURL    = flag.String("url", "", "fetch a single URL through the shared antibot layer instead of probing providers")
		noBrowser = flag.Bool("no-browser", false, "disable the Chromium fallback for -url")
		conc      = flag.Int("concurrency", 4, "how many providers to probe at once")
		egress    = flag.Bool("egress", false, "print the public egress address, ASN and country, then exit")
	)
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if *egress {
		os.Exit(runEgressCheck(ctx))
	}

	if *rawURL != "" {
		os.Exit(runURLCheck(*rawURL, *timeout, *noBrowser))
	}

	factories := make(map[string]mt.MovieFactory)
	canonical := make(map[string]string)
	mt.RangeMovieFactory(func(name string, factory mt.MovieFactory) bool {
		factories[name] = factory
		canonical[strings.ToLower(name)] = name
		return true
	})

	selected := make([]string, 0, len(factories))
	for name := range factories {
		selected = append(selected, name)
	}
	sort.Strings(selected)

	if *names != "" {
		selected = selected[:0]
		for _, raw := range strings.Split(*names, ",") {
			name := strings.TrimSpace(raw)
			if name == "" {
				continue
			}
			real, ok := canonical[strings.ToLower(name)]
			if !ok {
				fmt.Fprintf(os.Stderr, "unknown provider %q\n", name)
				os.Exit(2)
			}
			selected = append(selected, real)
		}
	}

	_ = engine.DefaultEngineName // keep engine import for provider registration

	results := make([]result, len(selected))
	var wg sync.WaitGroup
	limit := *conc
	if limit < 1 {
		limit = 1
	}
	sem := make(chan struct{}, limit)
	for i, name := range selected {
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = probe(ctx, factories[name], name, *id, *timeout)
		}(i, name)
	}
	wg.Wait()

	failed, skipped := 0, 0
	for _, res := range results {
		if res.err != nil {
			if isHarnessLimitation(res) {
				skipped++
			} else {
				failed++
			}
		}
		if *jsonOut {
			fmt.Println(jsonLine(res))
			continue
		}
		printResult(res, *verbose)
	}
	fmt.Printf("\n%d/%d ok, %d failed, %d skipped (no sample or id format not supported)\n",
		len(results)-failed-skipped, len(results), failed, skipped)
	if failed > 0 {
		os.Exit(1)
	}
}

// applyBrowserMode mirrors the server's boot decision so a probe run measures
// the same configuration the deployment runs with.
func applyBrowserMode(p mt.MovieProvider) {
	setter, ok := p.(mt.BrowserAllowedSetter)
	if !ok {
		return
	}
	mode := antibot.BrowserMode()
	setter.SetBrowserAllowed(true, "METAX_BROWSER_MODE="+mode)
	if mode == antibot.BrowserModeOff {
		setter.SetBrowserAllowed(false, "METAX_BROWSER_MODE=off")
		return
	}
	prober, ok := p.(mt.BrowserProber)
	if !ok || mode == antibot.BrowserModeAlways {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	required, detail := prober.ProbeBrowserNeed(ctx)
	setter.SetBrowserAllowed(required, detail)
	if !required {
		fmt.Fprintf(os.Stderr, "  browser not needed: %s\n", detail)
	}
}

func probe(ctx context.Context, factory mt.MovieFactory, name, override string, timeout time.Duration) (res result) {
	res = result{provider: name}
	start := time.Now()
	defer func() { res.elapsed = time.Since(start) }()

	sample := override
	if sample == "" {
		sample = defaultSamples[strings.ToLower(name)]
	}
	if sample == "" {
		res.stage = "sample"
		res.err = errors.New("no sample number configured; pass -id")
		return res
	}

	p := factory()
	// Providers that fall back to Chromium own a browser process; closing it
	// keeps a probe run from leaving stray Chrome instances behind.
	if closer, ok := p.(io.Closer); ok {
		defer func() { _ = closer.Close() }()
	}
	if setter, ok := p.(mt.RequestTimeoutSetter); ok {
		setter.SetRequestTimeout(timeout)
	}
	if setter, ok := p.(mt.ProxySetter); ok && os.Getenv("METAX_PROVIDER_PROXY") != "" {
		if err := setter.SetProxy(os.Getenv("METAX_PROVIDER_PROXY")); err != nil {
			res.stage = "proxy"
			res.err = err
			return res
		}
	}
	if setter, ok := p.(interface{ SetCookies(string) error }); ok && os.Getenv("METAX_PROVIDER_COOKIES") != "" {
		_ = setter.SetCookies(os.Getenv("METAX_PROVIDER_COOKIES"))
	}
	// Measure what the deployment would do, not what this machine can do with a
	// browser. `METAX_BROWSER_MODE=off metax-livecheck -providers javlibrary`
	// therefore answers the only question that matters when buying a proxy:
	// does this egress pass Cloudflare without Chromium?
	applyBrowserMode(p)

	done := make(chan error, 1)
	go func() {
		info, err := p.GetMovieInfoByID(p.NormalizeMovieID(sample))
		if err != nil {
			done <- err
			return
		}
		if info == nil {
			done <- errors.New("nil info without error")
			return
		}
		res.number, res.title = info.Number, info.Title
		done <- nil
	}()

	select {
	case err := <-done:
		res.stage = "detail"
		res.err = err
	case <-ctx.Done():
		res.stage = "detail"
		res.err = ctx.Err()
	case <-time.After(timeout + 10*time.Second):
		res.stage = "detail"
		res.err = errors.New("probe timed out")
	}
	return res
}

// runURLCheck reports how the shared fetch layer behaves for one URL: which
// strategy answered, the status code, the document title and the byte count.
// runEgressCheck reports the public address this process scrapes from. Bot
// management and geo-blocking key off the ASN and the country, so this is the
// first thing to check when providers fail on one host but pass on another.
func runEgressCheck(ctx context.Context) int {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	proxy := os.Getenv("METAX_PROVIDER_PROXY")
	info, err := antibot.DescribeEgress(ctx, proxy)
	if err != nil {
		fmt.Fprintf(os.Stderr, "egress lookup failed: %v\n", err)
		return 1
	}
	fmt.Printf("egress  : %s\n", info)
	if info.Country != "" {
		fmt.Printf("country : %s\n", info.Country)
	}
	if info.Cloud() {
		fmt.Println("network : cloud/hosting ASN — Cloudflare scores these as high risk,")
		fmt.Println("          so managed challenges and 1020 blocks are expected here")
	} else {
		fmt.Println("network : residential or unclassified — bot management is usually lenient")
	}
	if proxy != "" {
		fmt.Printf("proxy   : %s\n", antibot.RedactProxy(proxy))
	} else {
		fmt.Println("proxy   : none (direct connection)")
	}
	return 0
}

func runURLCheck(raw string, timeout time.Duration, disableBrowser bool) int {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	fetcher := antibot.New(antibot.Options{Timeout: timeout, DisableBrowser: disableBrowser})
	// Close before any exit path so a headless Chromium never outlives the run.
	defer fetcher.Close()
	req, err := http.NewRequest(http.MethodGet, raw, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid url: %v\n", err)
		return 2
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:128.0) Gecko/20100101 Firefox/128.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	start := time.Now()
	resp, err := fetcher.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		fmt.Printf("FAIL %s (%s)\n%s\n", raw, elapsed.Round(time.Millisecond), err)
		return 1
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	title := ""
	if match := regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`).FindSubmatch(body); len(match) == 2 {
		title = strings.Join(strings.Fields(string(match[1])), " ")
	}
	fmt.Printf("%s %s (%s) bytes=%d final=%s title=%q\n", resp.Status, raw, elapsed.Round(time.Millisecond), len(body), resp.Request.URL, title)
	if resp.StatusCode >= 400 {
		return 1
	}
	return 0
}

// isHarnessLimitation reports whether a probe failed for a reason that says
// nothing about the provider: the harness had no sample number, or the sample
// uses a format that provider never accepts.
func isHarnessLimitation(res result) bool {
	if res.err == nil {
		return false
	}
	if res.stage == "sample" {
		return true
	}
	msg := strings.ToLower(res.err.Error())
	for _, marker := range []string{
		"no sample number configured",
		"invalid movie id",
		"invalid id",
		"invalid url",
	} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

func printResult(res result, verbose bool) {
	if res.err == nil {
		fmt.Printf("OK    %-18s %-8s %6.2fs  %s  %s\n", res.provider, res.stage, res.elapsed.Seconds(), res.number, truncate(res.title, 48))
		return
	}
	if isHarnessLimitation(res) {
		fmt.Printf("SKIP  %-18s %-8s %6.2fs  %s\n", res.provider, res.stage, res.elapsed.Seconds(), truncate(res.err.Error(), 96))
		return
	}
	msg := res.err.Error()
	if verbose {
		msg = fmt.Sprintf("%+v", res.err)
	}
	fmt.Printf("FAIL  %-18s %-8s %6.2fs  %s\n", res.provider, res.stage, res.elapsed.Seconds(), truncate(msg, 160))
}

func jsonLine(res result) string {
	errText := ""
	if res.err != nil {
		errText = res.err.Error()
	}
	return fmt.Sprintf(`{"provider":%q,"stage":%q,"ok":%t,"elapsed_ms":%d,"number":%q,"title":%q,"error":%q}`,
		res.provider, res.stage, res.err == nil, res.elapsed.Milliseconds(), res.number, res.title, errText)
}

func truncate(s string, n int) string {
	r := []rune(strings.ReplaceAll(s, "\n", " "))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}
