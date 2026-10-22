package engine

import (
	"net/url"
	"strings"
)

// redactProxyForLog removes credentials and query data from a proxy URL before
// it is written to the process log. The proxy host and scheme remain visible
// for diagnosis, but usernames, passwords, tokens, and query strings do not.
func redactProxyForLog(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "<redacted-proxy>"
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}
