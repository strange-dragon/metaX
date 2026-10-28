// Copyright 2026 metaX contributors.
//
// Diagnostics for the machine's public egress. Cloudflare decisions and
// geo-blocks key off the ASN and country a request comes from, so when a
// provider fails it matters whether the host sits on a residential line, a
// cloud ASN, or a region the site refuses to serve.

package antibot

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Egress describes the public address a process reaches the network from.
type Egress struct {
	IP      string `json:"ip"`
	Country string `json:"country"`
	Region  string `json:"region"`
	City    string `json:"city"`
	ASN     string `json:"asn"`
	Org     string `json:"org"`
	Source  string `json:"source"`
}

// String renders the egress as one readable line.
func (e Egress) String() string {
	place := strings.Trim(strings.Join([]string{e.City, e.Region, e.Country}, ", "), ", ")
	network := strings.TrimSpace(e.ASN + " " + e.Org)
	if network == "" {
		network = "unknown network"
	}
	if place == "" {
		place = "unknown location"
	}
	return fmt.Sprintf("%s (%s, %s)", e.IP, network, place)
}

// Cloud reports whether the address most likely belongs to a hosting provider
// rather than a residential or mobile line. Bot management treats cloud ranges
// as high risk, so a failed challenge from a VPS is expected rather than a bug.
func (e Egress) Cloud() bool {
	text := strings.ToLower(e.Org + " " + e.ASN)
	for _, marker := range []string{
		"amazon", "aws", "google", "microsoft", "azure", "digitalocean", "linode",
		"vultr", "hetzner", "ovh", "oracle", "alibaba", "aliyun", "tencent",
		"koyeb", "fly.io", "scaleway", "contabo", "leaseweb", "choopa", "cloud",
		"hosting", "server", "datacenter", "data center", "colo",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

// DescribeEgress looks up the public address of this process. It talks to a
// public IP echo service through the same proxy and TLS profile stack the
// providers use, so the answer reflects the path a scrape would really take.
func DescribeEgress(ctx context.Context, proxyURL string) (Egress, error) {
	timeout := 12 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining < timeout {
			timeout = remaining
		}
	}
	client := &http.Client{Timeout: timeout}
	if proxyURL = strings.TrimSpace(proxyURL); proxyURL != "" {
		parsed, err := url.Parse(proxyURL)
		if err != nil {
			return Egress{}, fmt.Errorf("parse proxy %q: %w", proxyURL, err)
		}
		client.Transport = &http.Transport{Proxy: http.ProxyURL(parsed)}
	}

	var lastErr error
	for _, endpoint := range []string{
		"https://ipinfo.io/json",
		"https://api.ipify.org?format=json",
	} {
		egress, err := fetchEgress(ctx, client, endpoint)
		if err != nil {
			lastErr = err
			continue
		}
		if egress.IP != "" {
			return egress, nil
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no egress service answered")
	}
	return Egress{}, lastErr
}

// RedactProxy hides the credentials of a proxy URL so it can be logged. The
// host and port stay visible because they are what actually matter when
// diagnosing which exit node a scrape used.
func RedactProxy(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" {
		return "(unparseable proxy url)"
	}
	if parsed.User != nil {
		parsed.User = url.User("***")
	}
	return parsed.String()
}

func fetchEgress(ctx context.Context, client *http.Client, endpoint string) (Egress, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Egress{}, err
	}
	req.Header.Set("User-Agent", "metaX-egress-check")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return Egress{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Egress{}, fmt.Errorf("%s answered %s", endpoint, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return Egress{}, err
	}
	var payload struct {
		IP       string `json:"ip"`
		Country  string `json:"country"`
		Region   string `json:"region"`
		City     string `json:"city"`
		Org      string `json:"org"`
		ASN      string `json:"asn"`
		Readme   string `json:"readme"`
		Postal   string `json:"postal"`
		Hostname string `json:"hostname"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return Egress{}, err
	}
	egress := Egress{
		IP:      payload.IP,
		Country: payload.Country,
		Region:  payload.Region,
		City:    payload.City,
		Source:  endpoint,
	}
	// ipinfo returns "org": "AS24940 Hetzner Online GmbH"; split it once so
	// callers can reason about the ASN separately from the operator name.
	if payload.Org != "" {
		if asn, org, ok := strings.Cut(payload.Org, " "); ok && strings.HasPrefix(strings.ToUpper(asn), "AS") {
			egress.ASN = asn
			egress.Org = strings.TrimSpace(org)
		} else {
			egress.Org = payload.Org
		}
	}
	if payload.ASN != "" && egress.ASN == "" {
		egress.ASN = payload.ASN
	}
	return egress, nil
}
