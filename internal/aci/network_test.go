package aci

import (
	"net"
	"testing"
)

func TestValidateOutboundURLRejectsLocalAndUnsafeTargets(t *testing.T) {
	cases := []string{
		"file:///C:/Windows/win.ini",
		"http://localhost/",
		"http://127.0.0.1/",
		"http://10.0.0.1/",
		"http://169.254.169.254/latest/meta-data/",
		"http://metadata.google.internal/",
		"http://[::1]/",
		"https://user:secret@example.com/",
	}
	for _, raw := range cases {
		if _, err := validateOutboundURL(raw); err == nil {
			t.Fatalf("unsafe URL accepted: %s", raw)
		}
	}
	for _, raw := range []string{"https://example.com/path?q=1", "https://8.8.8.8/"} {
		if _, err := validateOutboundURL(raw); err != nil {
			t.Fatalf("public URL rejected %s: %v", raw, err)
		}
	}
}

func TestIsPublicNetworkIPRejectsSpecialUse(t *testing.T) {
	cases := []struct {
		raw  string
		want bool
	}{
		{"8.8.8.8", true},
		{"1.1.1.1", true},
		{"127.0.0.1", false},
		{"10.0.0.1", false},
		{"172.16.0.1", false},
		{"192.168.1.1", false},
		{"169.254.1.1", false},
		{"100.64.0.1", false},
		{"::1", false},
		{"fe80::1", false},
	}
	for _, tc := range cases {
		if got := isPublicNetworkIP(net.ParseIP(tc.raw)); got != tc.want {
			t.Fatalf("isPublicNetworkIP(%s)=%v want %v", tc.raw, got, tc.want)
		}
	}
}
