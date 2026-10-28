// Copyright 2026 metaX contributors.

package antibot

import (
	"runtime"
	"strings"
	"testing"
)

func TestEgressCloudDetection(t *testing.T) {
	cases := []struct {
		name string
		in   Egress
		want bool
	}{
		{"aws", Egress{ASN: "AS16509", Org: "Amazon.com, Inc."}, true},
		{"koyeb", Egress{Org: "Koyeb SAS"}, true},
		{"hetzner", Egress{ASN: "AS24940", Org: "Hetzner Online GmbH"}, true},
		{"residential jp", Egress{ASN: "AS2516", Org: "KDDI CORPORATION"}, false},
		{"residential cn", Egress{ASN: "AS4134", Org: "Chinanet"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.in.Cloud(); got != tc.want {
				t.Fatalf("Cloud() = %v, want %v for %+v", got, tc.want, tc.in)
			}
		})
	}
}

func TestEgressString(t *testing.T) {
	got := Egress{IP: "165.85.9.7", ASN: "AS394089", Org: "Google LLC", City: "Singapore", Country: "SG"}.String()
	for _, want := range []string{"165.85.9.7", "AS394089", "Google LLC", "Singapore", "SG"} {
		if !strings.Contains(got, want) {
			t.Fatalf("String() = %q, missing %q", got, want)
		}
	}
	empty := Egress{IP: "203.0.113.9"}.String()
	if !strings.Contains(empty, "unknown") {
		t.Fatalf("String() = %q, want unknown placeholders for missing fields", empty)
	}
}

func TestRedactProxy(t *testing.T) {
	got := RedactProxy("socks5://user:secret@127.0.0.1:1080")
	if strings.Contains(got, "secret") {
		t.Fatalf("RedactProxy leaked the password: %q", got)
	}
	if !strings.Contains(got, "127.0.0.1:1080") {
		t.Fatalf("RedactProxy dropped the host: %q", got)
	}
	if got := RedactProxy("://bad"); got != "(unparseable proxy url)" {
		t.Fatalf("RedactProxy(%q) = %q", "://bad", got)
	}
}

// The sandbox flags must never be added on a developer workstation: dropping
// the Chromium sandbox locally would be a real regression.
func TestInContainerFalseOnWorkstation(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("linux CI may legitimately run inside a container")
	}
	if inContainer() {
		t.Fatal("inContainer() = true on a non-Linux workstation")
	}
}
