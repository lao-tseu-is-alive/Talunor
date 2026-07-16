package webfetch

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// permissive lets every address through (for happy-path tests over a loopback
// httptest server, which the real guard would block).
func permissive(net.IP) bool { return false }

// allowLoopback relaxes only loopback (so httptest works) and applies the real
// SSRF rule to everything else — used to prove a redirect to an internal address
// is still blocked.
func allowLoopback(ip net.IP) bool {
	if ip.IsLoopback() {
		return false
	}
	return blockedIP(ip)
}

// TestBlockedIP exhaustively table-tests the SSRF classifier — pure, no network.
func TestBlockedIP(t *testing.T) {
	cases := []struct {
		ip      string
		blocked bool
	}{
		{"127.0.0.1", true},           // loopback
		{"::1", true},                 // loopback v6
		{"10.0.0.1", true},            // private
		{"172.16.5.4", true},          // private
		{"192.168.1.1", true},         // private
		{"169.254.169.254", true},     // cloud metadata (link-local)
		{"169.254.1.1", true},         // link-local
		{"fe80::1", true},             // link-local v6
		{"fc00::1", true},             // unique-local v6 (private)
		{"100.64.0.1", true},          // CGNAT (RFC6598)
		{"100.100.100.200", true},     // Alibaba metadata (in CGNAT)
		{"0.0.0.0", true},             // unspecified
		{"::", true},                  // unspecified v6
		{"224.0.0.1", true},           // multicast
		{"::ffff:127.0.0.1", true},    // IPv4-in-IPv6 loopback
		{"::ffff:10.0.0.1", true},     // IPv4-in-IPv6 private
		{"8.8.8.8", false},            // public
		{"1.1.1.1", false},            // public
		{"93.184.216.34", false},      // public (example.com)
		{"2606:2800:220:1::1", false}, // public v6
	}
	for _, c := range cases {
		ip := net.ParseIP(c.ip)
		if ip == nil {
			t.Fatalf("bad test IP %q", c.ip)
		}
		if got := blockedIP(ip); got != c.blocked {
			t.Errorf("blockedIP(%s) = %v; want %v", c.ip, got, c.blocked)
		}
	}
	// A nil IP fails closed.
	if !blockedIP(nil) {
		t.Error("blockedIP(nil) = false; want true (fail closed)")
	}
}

// TestFetchHappyPath fetches text and reports status/content-type/body.
func TestFetchHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("hello talunor"))
	}))
	defer srv.Close()

	c := New(DefaultLimits(), permissive)
	res, err := c.Fetch(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if res.Status != 200 || res.Body != "hello talunor" || !res.Textual {
		t.Errorf("got %+v; want 200/'hello talunor'/textual", res)
	}
}

// TestFetchRedirectToInternalBlocked is the crux: a public-looking hop that
// redirects to an internal (link-local) address must be refused by the guard.
func TestFetchRedirectToInternalBlocked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	}))
	defer srv.Close()

	c := New(DefaultLimits(), allowLoopback) // loopback ok; the redirect target is not.
	_, err := c.Fetch(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("expected the redirect to 169.254.169.254 to be blocked")
	}
	if !strings.Contains(err.Error(), "SSRF") && !strings.Contains(err.Error(), "blocked") {
		t.Errorf("error = %v; want an SSRF/blocked-address error", err)
	}
}

// TestFetchTruncates caps an oversized body and flags it.
func TestFetchTruncates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(strings.Repeat("x", 1000)))
	}))
	defer srv.Close()

	lim := DefaultLimits()
	lim.MaxBytes = 100
	res, err := New(lim, permissive).Fetch(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if !res.Truncated || len(res.Body) != 100 {
		t.Errorf("body len = %d, truncated = %v; want 100/true", len(res.Body), res.Truncated)
	}
}

// TestFetchNonTextual returns metadata only for a binary content-type.
func TestFetchNonTextual(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte{0x89, 0x50, 0x4e, 0x47})
	}))
	defer srv.Close()

	res, err := New(DefaultLimits(), permissive).Fetch(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if res.Textual || res.Body != "" {
		t.Errorf("got textual=%v body=%q; want non-textual, empty body", res.Textual, res.Body)
	}
	if res.ContentType != "image/png" {
		t.Errorf("content-type = %q; want image/png", res.ContentType)
	}
}

// TestFetchTimeout fails when the server is slower than the deadline.
func TestFetchTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte("too late"))
	}))
	defer srv.Close()

	lim := DefaultLimits()
	lim.Timeout = 40 * time.Millisecond
	_, err := New(lim, permissive).Fetch(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("expected a timeout error")
	}
}

// TestFetchSchemeRejected refuses non-http(s) schemes and, when configured,
// plain http.
func TestFetchSchemeRejected(t *testing.T) {
	c := New(DefaultLimits(), permissive)
	for _, u := range []string{"gopher://evil/", "ftp://host/f", "file:///etc/passwd"} {
		if _, err := c.Fetch(context.Background(), u); err == nil {
			t.Errorf("scheme in %q should be rejected", u)
		}
	}

	lim := DefaultLimits()
	lim.AllowHTTP = false
	if _, err := New(lim, permissive).Fetch(context.Background(), "http://example.com/"); err == nil {
		t.Error("http:// should be rejected when AllowHTTP is false")
	}
}
