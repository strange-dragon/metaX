package engine

import (
	"strings"
	"testing"
)

func TestRedactProxyForLog(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"http credentials", "http://alice:secret@proxy.example:8080", "http://proxy.example:8080"},
		{"socks token username", "socks5://token-value@proxy.example:1080", "socks5://proxy.example:1080"},
		{"query token", "http://proxy.example:8080/?token=secret", "http://proxy.example:8080/"},
		{"plain host", "http://proxy.example:8080", "http://proxy.example:8080"},
		{"empty", "   ", ""},
		{"invalid", "://not a URL", "<redacted-proxy>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := redactProxyForLog(tc.raw)
			if got != tc.want {
				t.Fatalf("redactProxyForLog(%q) = %q, want %q", tc.raw, got, tc.want)
			}
			if strings.Contains(got, "secret") || strings.Contains(got, "token-value") {
				t.Fatalf("redacted proxy still contains a secret: %q", got)
			}
		})
	}
}
