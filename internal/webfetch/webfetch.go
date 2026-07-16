// Package webfetch is Talunor's guarded HTTP fetcher — the "network opt-IN" that
// counterbalances the network-OFF bash sandbox. Where bash needs a *kernel*
// boundary (it runs untrusted code), web_fetch needs an *application-layer*
// policy: the fetched bytes never execute, they are just handed to the model as
// text, so the real risks are SSRF (tricking the agent into reaching an internal
// service) and resource abuse (huge or slow responses). This package defends
// against both.
//
// The centrepiece is the SSRF guard. Rather than resolve a hostname, check the
// IP, then connect — which leaves a DNS-rebinding window between the check and
// the connect — the guard runs inside the dialer's Control hook, which fires
// with the *actual resolved address* immediately before connecting. The IP that
// is vetted is therefore the IP that is dialled, on every hop (initial request
// and each redirect alike). blockedIP is a pure function so it can be table-tested
// exhaustively without touching the network.
package webfetch

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// Limits bound a single fetch. DefaultLimits gives sensible values; cmd/talunor
// lets a few be tuned by env.
type Limits struct {
	Timeout      time.Duration // whole-request deadline (dial + headers + body).
	MaxBytes     int64         // response body cap; extra bytes are dropped and Result.Truncated set.
	MaxRedirects int           // redirect hops allowed before the fetch fails.
	AllowHTTP    bool          // permit plain http:// as well as https://.
}

// DefaultLimits: 10s, 512 KiB, 5 redirects, http+https allowed. The SSRF guard is
// IP-based, so allowing http adds no internal-network exposure (a plain-http URL
// still cannot resolve to a blocked address).
func DefaultLimits() Limits {
	return Limits{
		Timeout:      10 * time.Second,
		MaxBytes:     512 << 10, // 512 KiB
		MaxRedirects: 5,
		AllowHTTP:    true,
	}
}

// Result is one fetch's outcome. Body carries the (possibly truncated) text; for
// a non-textual content-type it is empty and only the metadata is returned, so
// binary blobs never flood the model's context.
type Result struct {
	FinalURL    string // URL after redirects.
	Status      int    // HTTP status code.
	ContentType string // response Content-Type header.
	Body        string // decoded text body, capped at Limits.MaxBytes (empty if non-textual).
	Truncated   bool   // true when the body was longer than MaxBytes.
	Textual     bool   // false when the content-type was treated as binary (Body left empty).
}

// Client is a reusable, SSRF-guarded HTTP fetcher. Build it with New.
type Client struct {
	http    *http.Client
	lim     Limits
	blocked func(net.IP) bool // address policy; DefaultLimits uses blockedIP.
}

// New builds a Client. A nil blocked policy defaults to blockedIP (the real SSRF
// guard); tests inject a permissive policy so they can talk to a loopback
// httptest server while still unit-testing blockedIP directly.
func New(lim Limits, blocked func(net.IP) bool) *Client {
	if blocked == nil {
		blocked = blockedIP
	}
	c := &Client{lim: lim, blocked: blocked}

	dialer := &net.Dialer{
		Timeout:   lim.Timeout,
		KeepAlive: 30 * time.Second,
		// Control fires with the resolved ip:port right before connect — the
		// DNS-rebinding-safe place to enforce the address policy.
		Control: c.guardDial,
	}
	c.http = &http.Client{
		Timeout: lim.Timeout,
		Transport: &http.Transport{
			DialContext:           dialer.DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          10,
			IdleConnTimeout:       30 * time.Second,
			TLSHandshakeTimeout:   lim.Timeout,
			ExpectContinueTimeout: time.Second,
		},
		CheckRedirect: c.checkRedirect,
	}
	return c
}

// guardDial rejects a connection whose resolved address is a blocked IP. It runs
// for the initial request and for every redirect hop (each dials afresh).
func (c *Client) guardDial(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("web_fetch: cannot parse dial address %q: %w", address, err)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("web_fetch: unresolved dial address %q", address)
	}
	if c.blocked(ip) {
		return fmt.Errorf("web_fetch: refusing to connect to blocked address %s (SSRF guard)", ip)
	}
	return nil
}

// checkRedirect caps the redirect chain and re-validates the scheme of each
// target (the target IP is re-checked by guardDial when the next hop dials).
func (c *Client) checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= c.lim.MaxRedirects {
		return fmt.Errorf("web_fetch: too many redirects (>%d)", c.lim.MaxRedirects)
	}
	return c.checkScheme(req.URL)
}

// checkScheme allows https (and http when configured) and rejects everything
// else (file, ftp, gopher, data, …) — gopher/file in particular are classic SSRF
// and local-file vectors.
func (c *Client) checkScheme(u *url.URL) error {
	switch strings.ToLower(u.Scheme) {
	case "https":
		return nil
	case "http":
		if c.lim.AllowHTTP {
			return nil
		}
		return fmt.Errorf("web_fetch: http:// is disabled (https only)")
	default:
		return fmt.Errorf("web_fetch: unsupported scheme %q (only http/https)", u.Scheme)
	}
}

// Fetch GETs rawURL under the client's limits and SSRF guard and returns the
// result. A non-2xx status is *not* an error — it is reported in Result.Status so
// the model can observe it; only transport/policy failures return an error.
func (c *Client) Fetch(ctx context.Context, rawURL string) (Result, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return Result{}, fmt.Errorf("web_fetch: invalid URL: %w", err)
	}
	if u.Host == "" {
		return Result{}, fmt.Errorf("web_fetch: URL has no host: %q", rawURL)
	}
	if err := c.checkScheme(u); err != nil {
		return Result{}, err
	}

	ctx, cancel := context.WithTimeout(ctx, c.lim.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Result{}, fmt.Errorf("web_fetch: %w", err)
	}
	req.Header.Set("User-Agent", "Talunor-web_fetch/1.0")
	req.Header.Set("Accept", "text/*, application/json, application/xml;q=0.9, */*;q=0.1")

	resp, err := c.http.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("web_fetch: %w", err)
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	res := Result{
		FinalURL:    resp.Request.URL.String(),
		Status:      resp.StatusCode,
		ContentType: ct,
		Textual:     isTextual(ct),
	}
	if !res.Textual {
		// Don't pull a binary body into the model's context; the metadata is
		// enough for it to decide what to do.
		return res, nil
	}

	// Read one byte past the cap so we can tell "exactly MaxBytes" from "more".
	body, err := io.ReadAll(io.LimitReader(resp.Body, c.lim.MaxBytes+1))
	if err != nil {
		return Result{}, fmt.Errorf("web_fetch: reading body: %w", err)
	}
	if int64(len(body)) > c.lim.MaxBytes {
		body = body[:c.lim.MaxBytes]
		res.Truncated = true
	}
	res.Body = string(body)
	return res, nil
}

// isTextual reports whether a Content-Type is safe to hand to the model as text.
// An empty type is treated as text (many servers omit it); known binary families
// are excluded so images/archives/etc. are reported by metadata only.
func isTextual(contentType string) bool {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = strings.TrimSpace(ct[:i]) // drop "; charset=…".
	}
	if ct == "" || strings.HasPrefix(ct, "text/") {
		return true
	}
	switch ct {
	case "application/json", "application/ld+json", "application/xml",
		"application/xhtml+xml", "application/javascript", "application/x-ndjson",
		"application/rss+xml", "application/atom+xml", "image/svg+xml":
		return true
	}
	return false
}

// blockedCIDRs holds ranges not already covered by the net.IP helper methods —
// notably RFC6598 carrier-grade NAT (100.64.0.0/10), which some clouds use for
// metadata (e.g. Alibaba's 100.100.100.200).
var blockedCIDRs = func() []*net.IPNet {
	var out []*net.IPNet
	for _, cidr := range []string{"100.64.0.0/10"} {
		if _, n, err := net.ParseCIDR(cidr); err == nil {
			out = append(out, n)
		}
	}
	return out
}()

// blockedIP is the SSRF guard's core: it reports whether ip must not be dialled.
// It blocks loopback, private (RFC1918 + ULA), link-local (incl. the
// 169.254.169.254 cloud-metadata address), CGNAT, unspecified, and multicast
// addresses. A nil/unclassifiable IP is blocked (fail closed). Pure and
// side-effect-free so it is exhaustively table-testable.
func blockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4 // normalise IPv4-in-IPv6 (::ffff:127.0.0.1) to its v4 form.
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsInterfaceLocalMulticast() {
		return true
	}
	for _, n := range blockedCIDRs {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}
