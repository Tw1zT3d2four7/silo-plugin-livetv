// Package testutil contains helpers used only by Live TV's automated tests.
package testutil

import (
	"net"
	"net/http"
	"time"
)

// LocalHTTPClient is for tests only. httptest servers bind to loopback, which
// production's SSRF-guarded clients correctly reject. Tests inject this client
// for local fixtures without weakening production network protections.
func LocalHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 60 * time.Second,
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			ForceAttemptHTTP2: true,
			MaxIdleConns: 100,
			IdleConnTimeout: 90 * time.Second,
			TLSHandshakeTimeout: 10 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
			ExpectContinueTimeout: time.Second,
		},
	}
}
