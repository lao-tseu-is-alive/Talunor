package tools

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lao-tseu-is-alive/Talunor/internal/webfetch"
)

func urlArgs(u string) json.RawMessage { return json.RawMessage(`{"url":` + jsonString(u) + `}`) }

func jsonString(s string) string { b, _ := json.Marshal(s); return string(b) }

// TestWebFetchAllowlistGating: the allowlist decides which URLs skip approval;
// exact entries do not match sub-domains, leading-dot entries do; unparseable
// input fails safe (approval required).
func TestWebFetchAllowlistGating(t *testing.T) {
	w := NewWebFetch(nil, []string{"Example.com", ".trusted.org"})

	cases := []struct {
		url          string
		needsApprove bool
	}{
		{"https://example.com/x", false},      // exact (case-insensitive)
		{"https://sub.example.com/x", true},   // exact entry doesn't cover sub-domains
		{"https://trusted.org/x", false},      // .trusted.org matches the base
		{"https://docs.trusted.org/a", false}, // …and sub-domains
		{"https://evil.com/x", true},          // not listed
	}
	for _, c := range cases {
		if got := w.RequiresApprovalForArgs(urlArgs(c.url)); got != c.needsApprove {
			t.Errorf("RequiresApprovalForArgs(%s) = %v; want %v", c.url, got, c.needsApprove)
		}
	}

	// Unparseable / empty arguments must fail safe (require approval).
	if !w.RequiresApprovalForArgs(json.RawMessage(`{"url":""}`)) {
		t.Error("empty url should require approval")
	}
	if !w.RequiresApprovalForArgs(json.RawMessage(`not json`)) {
		t.Error("bad args should require approval")
	}
}

// TestWebFetchExecute drives a fetch end-to-end through the tool, with a
// permissive address guard so it can reach the loopback test server.
func TestWebFetchExecute(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("page body here"))
	}))
	defer srv.Close()

	client := webfetch.New(webfetch.DefaultLimits(), func(net.IP) bool { return false })
	w := NewWebFetch(client, nil)

	obs, err := w.Execute(context.Background(), urlArgs(srv.URL))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(obs, "200") || !strings.Contains(obs, "page body here") {
		t.Errorf("observation missing status/body:\n%s", obs)
	}
}

// TestWebFetchImplementsApprovableFor guards the interface wiring the agent relies
// on (a plain Approvable would silently bypass the per-URL allowlist).
func TestWebFetchImplementsApprovableFor(t *testing.T) {
	var _ ApprovableFor = (*WebFetch)(nil)
}
