package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/lao-tseu-is-alive/Talunor/internal/webfetch"
)

// WebFetch fetches a URL over the internet and returns its text, under the leash
// of internal/webfetch (SSRF guard, timeout, size cap). It is the counterpart to
// the network-off bash tool: opt-in (TALUNOR_WEBFETCH=1, see cmd/talunor) and
// gated. Gating is per-URL via [ApprovableFor]: a host on the user's allowlist is
// waved through, everything else needs explicit human approval. The allowlist
// only skips the *prompt* — the SSRF guard still applies, so an allowlisted host
// that resolves to an internal address is refused all the same.
type WebFetch struct {
	client *webfetch.Client
	allow  hostAllowlist
}

// NewWebFetch builds the tool over a fetch client and a list of hosts that skip
// approval (empty = every fetch is approval-gated).
func NewWebFetch(client *webfetch.Client, allow []string) *WebFetch {
	return &WebFetch{client: client, allow: newHostAllowlist(allow)}
}

func (*WebFetch) Name() string { return "web_fetch" }

func (*WebFetch) Description() string {
	return "Fetch the contents of an http(s) URL from the internet and return it " +
		"as text (for reading a web page, docs, or a JSON API). The response is " +
		"size-capped and non-text content (images, binaries) is not downloaded. " +
		"Requests to private, loopback, or cloud-metadata addresses are refused. " +
		"Unless the host is pre-approved, each call needs the user's explicit " +
		"approval. Use it to read something online — not to call state-changing APIs."
}

func (*WebFetch) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"url": {
				"type": "string",
				"description": "The absolute http(s) URL to fetch, e.g. \"https://example.com/page\"."
			}
		},
		"required": ["url"]
	}`)
}

// RequiresApprovalForArgs gates the call: approval is required unless the URL's
// host is on the allowlist. A URL that cannot be parsed fails safe (approval
// required). See [ApprovableFor].
func (w *WebFetch) RequiresApprovalForArgs(args json.RawMessage) bool {
	u, err := parseURLArg(args)
	if err != nil {
		return true
	}
	return !w.allow.allows(u.Hostname())
}

func (w *WebFetch) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	u, err := parseURLArg(args)
	if err != nil {
		return "", err
	}
	res, err := w.client.Fetch(ctx, u.String())
	if err != nil {
		return "", err
	}
	return formatResult(res), nil
}

// parseURLArg decodes the {url} argument into a parsed URL.
func parseURLArg(args json.RawMessage) (*url.URL, error) {
	var in struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if strings.TrimSpace(in.URL) == "" {
		return nil, fmt.Errorf("url is required")
	}
	u, err := url.Parse(strings.TrimSpace(in.URL))
	if err != nil {
		return nil, fmt.Errorf("invalid url: %w", err)
	}
	return u, nil
}

// formatResult renders a fetch result as the observation the model sees: a status
// line, then the body (or a note that binary content was skipped).
func formatResult(r webfetch.Result) string {
	head := fmt.Sprintf("GET %s → %d (%s)", r.FinalURL, r.Status, orNone(r.ContentType))
	if !r.Textual {
		return head + "\n[non-text content — not fetched]"
	}
	body := r.Body
	if r.Truncated {
		body += "\n…[truncated]"
	}
	if strings.TrimSpace(body) == "" {
		return head + "\n(empty body)"
	}
	return head + "\n\n" + body
}

func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "no content-type"
	}
	return s
}

// hostAllowlist matches hostnames against user-configured entries. An entry is an
// exact host ("example.com") or a leading-dot suffix (".example.com") that also
// matches sub-domains (docs.example.com). Matching is case-insensitive.
type hostAllowlist []string

func newHostAllowlist(entries []string) hostAllowlist {
	var out hostAllowlist
	for _, e := range entries {
		if e = strings.ToLower(strings.TrimSpace(e)); e != "" {
			out = append(out, e)
		}
	}
	return out
}

func (a hostAllowlist) allows(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false
	}
	for _, e := range a {
		if strings.HasPrefix(e, ".") {
			if host == e[1:] || strings.HasSuffix(host, e) {
				return true
			}
		} else if host == e {
			return true
		}
	}
	return false
}
