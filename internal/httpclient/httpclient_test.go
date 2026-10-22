package httpclient

import (
	"net"
	"testing"
)

func TestIsPublicIP(t *testing.T) {
	cases := []struct {
		ip   string
		want bool
	}{
		{"8.8.8.8", true},
		{"1.1.1.1", true},
		{"203.0.113.5", true},
		{"2606:4700:4700::1111", true},
		// Blocked: loopback / private / link-local / metadata / unspecified.
		{"127.0.0.1", false},
		{"::1", false},
		{"10.0.0.1", false},
		{"10.255.255.255", false},
		{"172.16.0.1", false},
		{"172.31.255.255", false},
		{"172.32.0.1", true}, // just outside RFC1918
		{"192.168.1.1", false},
		{"169.254.169.254", false}, // AWS metadata
		{"100.64.0.1", false},      // CGNAT
		{"0.0.0.0", false},
		{"fc00::1", false}, // ULA
		{"fd12:3456::1", false},
		{"fe80::1", false}, // link-local v6
		{"ff02::1", false}, // multicast v6
	}
	for _, c := range cases {
		ip := net.ParseIP(c.ip)
		if ip == nil {
			t.Fatalf("bad test ip %q", c.ip)
		}
		if got := isPublicIP(ip); got != c.want {
			t.Errorf("isPublicIP(%s) = %v, want %v", c.ip, got, c.want)
		}
	}
}

func TestGuardedControlBlocksPrivate(t *testing.T) {
	if err := guardedControl("tcp", "169.254.169.254:80", nil); err == nil {
		t.Fatal("expected metadata address to be blocked")
	}
	if err := guardedControl("tcp", "10.1.2.3:443", nil); err == nil {
		t.Fatal("expected RFC1918 address to be blocked")
	}
	if err := guardedControl("tcp", "8.8.8.8:443", nil); err != nil {
		t.Fatalf("expected public address to pass, got %v", err)
	}
	// A non-IP address (should never happen post-resolution) is rejected.
	if err := guardedControl("tcp", "not-an-ip:80", nil); err == nil {
		t.Fatal("expected non-IP address to be blocked")
	}
}

func TestStreamingHasNoOverallTimeout(t *testing.T) {
	if c := Streaming(); c.Timeout != 0 {
		t.Errorf("Streaming().Timeout = %v, want 0 (unbounded)", c.Timeout)
	}
}

func TestStreamingForSourceHasNoOverallTimeout(t *testing.T) {
	if c := StreamingForSource("http://192.168.1.20:8080/live/1.ts"); c.Timeout != 0 {
		t.Errorf("StreamingForSource().Timeout = %v, want 0 (unbounded)", c.Timeout)
	}
	if err := guardedControlConfiguredSource("tcp", "192.168.1.20:8080", nil); err != nil {
		t.Errorf("explicit private stream host should be allowed: %v", err)
	}
	if err := guardedControlConfiguredSource("tcp", "127.0.0.1:8080", nil); err == nil {
		t.Error("loopback must remain blocked for stream URLs")
	}
	if err := guardedControlConfiguredSource("tcp", "169.254.169.254:80", nil); err == nil {
		t.Error("metadata address must remain blocked for stream URLs")
	}
}

func TestShortLivedHasTimeout(t *testing.T) {
	if c := ShortLived(); c.Timeout != shortLivedTimeout {
		t.Errorf("ShortLived().Timeout = %v, want %v", c.Timeout, shortLivedTimeout)
	}
}

func TestRedactURL(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"http://user:pass@host.example/path?token=abc", "http://host.example/path"},
		{"https://host.example/get.php?username=u&password=p", "https://host.example/get.php"},
		{"https://host.example/playlist.m3u", "https://host.example/playlist.m3u"},
		{"://bad url", "<unparseable-url>"},
	}
	for _, c := range cases {
		if got := RedactURL(c.in); got != c.want {
			t.Errorf("RedactURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestConfiguredSourceGuardAllowsPrivateUnicastOnly(t *testing.T) {
	for _, address := range []string{"10.0.0.1:80", "172.16.0.1:80", "192.168.1.1:80", "100.64.0.1:80", "[fd12:3456::1]:80", "8.8.8.8:80"} {
		if err := guardedControlConfiguredSource("tcp", address, nil); err != nil {
			t.Errorf("configured source address %s should pass: %v", address, err)
		}
	}
	for _, address := range []string{"127.0.0.1:80", "169.254.169.254:80", "0.0.0.0:80", "192.0.0.1:80", "[::1]:80", "[fe80::1]:80"} {
		if err := guardedControlConfiguredSource("tcp", address, nil); err == nil {
			t.Errorf("configured source address %s should remain blocked", address)
		}
	}
}
