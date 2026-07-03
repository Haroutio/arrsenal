package wire

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client is the HTTP substrate every wiring step rides on: API-key auth,
// bounded retries with backoff on transient failures, and hard redaction —
// no error, log line, or report may ever carry a secret (DESIGN.md §9).
type Client struct {
	base   string // http://sonarr:8989 — container-name URLs (DESIGN.md §6)
	key    string
	header string // header carrying the key, e.g. X-Api-Key
	http   *http.Client

	// redactions are additional secrets (beyond the key) scrubbed from any
	// text that could reach a human — request bodies carry passwords, and
	// servers echo bodies into errors more often than they should.
	redactions []string
	// headers are static extras sent on every request.
	headers map[string]string

	// retry policy; fixed defaults, overridable in tests
	attempts int
	backoff  time.Duration
}

// WithRedaction registers extra secrets to scrub from error output. Callers
// sending a secret in a request body MUST register it first.
func (c *Client) WithRedaction(secrets ...string) *Client {
	for _, s := range secrets {
		if s != "" {
			c.redactions = append(c.redactions, s)
		}
	}
	return c
}

// WithHeader sets a static header on every request (Jellyfin's
// X-Emby-Authorization identity, for example).
func (c *Client) WithHeader(name, value string) *Client {
	if c.headers == nil {
		c.headers = map[string]string{}
	}
	c.headers[name] = value
	return c
}

// WithRetry overrides the retry policy. Best-effort steps use a snappy one
// so the run never stalls on an app that may simply be down.
func (c *Client) WithRetry(attempts int, backoff time.Duration) *Client {
	if attempts > 0 {
		c.attempts = attempts
	}
	c.backoff = backoff
	return c
}

// NewClient builds a client for one app's API.
func NewClient(base, apiKey, keyHeader string) *Client {
	return &Client{
		base:     strings.TrimRight(base, "/"),
		key:      apiKey,
		header:   keyHeader,
		http:     &http.Client{Timeout: 15 * time.Second},
		attempts: 4,
		backoff:  2 * time.Second,
	}
}

// ErrAuth means the app rejected our key — almost always a stale or foreign
// config; the caller should say which app and suggest re-reading the key.
var ErrAuth = errors.New("the app rejected the API key")

// GetJSON fetches path into out.
func (c *Client) GetJSON(ctx context.Context, path string, out any) error {
	return c.do(ctx, http.MethodGet, path, nil, out)
}

// PostJSON sends body (marshalled) to path, decoding any response into out
// when out is non-nil.
func (c *Client) PostJSON(ctx context.Context, path string, body, out any) error {
	return c.do(ctx, http.MethodPost, path, body, out)
}

// PutJSON updates a resource. The idempotency contract means wiring code
// only PUTs resources it just created or verified absent-then-created —
// never entries that already existed (DESIGN.md §7).
func (c *Client) PutJSON(ctx context.Context, path string, body, out any) error {
	return c.do(ctx, http.MethodPut, path, body, out)
}

// errf builds an error with every registered secret scrubbed — paths can
// carry keys in query strings (SABnzbd), bodies can be echoed by servers.
func (c *Client) errf(format string, args ...any) error {
	return errors.New(c.redactAll(fmt.Sprintf(format, args...)))
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return c.errf("%s %s: encoding request: %v", method, path, err)
		}
	}

	var lastErr error
	for attempt := 1; attempt <= c.attempts; attempt++ {
		if attempt > 1 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(c.backoff * time.Duration(attempt-1)):
			}
		}

		req, err := http.NewRequestWithContext(ctx, method, c.base+path, bytes.NewReader(payload))
		if err != nil {
			return c.errf("%s %s: %v", method, path, err)
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if c.header != "" {
			req.Header.Set(c.header, c.key)
		}
		for name, value := range c.headers {
			req.Header.Set(name, value)
		}

		resp, err := c.http.Do(req)
		if err != nil {
			// Network errors can embed the URL; the URL never contains the
			// key (it rides in a header), so this is safe to surface.
			lastErr = c.errf("%s %s: %v", method, path, err)
			continue // connection refused etc. — the app may still be starting
		}

		respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
		if readErr != nil {
			lastErr = c.errf("%s %s: reading response: %v", method, path, readErr)
			continue
		}

		switch {
		case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
			return fmt.Errorf("%s: %w", c.redactAll(method+" "+path), ErrAuth) // retrying a bad key is noise
		case resp.StatusCode >= 500:
			lastErr = c.errf("%s %s: HTTP %d (transient)", method, path, resp.StatusCode)
			continue
		case resp.StatusCode >= 400:
			return c.errf("%s %s: HTTP %d: %s", method, path, resp.StatusCode, string(respBody))
		}

		if out != nil {
			if err := json.Unmarshal(respBody, out); err != nil {
				return c.errf("%s %s: decoding response: %v", method, path, err)
			}
		}
		return nil
	}
	return fmt.Errorf("after %d attempts: %w", c.attempts, lastErr)
}

// redactAll strips every registered secret from text destined for humans.
// Belt and braces: response bodies should never echo secrets, but "should"
// is not a guarantee.
func (c *Client) redactAll(s string) string {
	for _, secret := range append([]string{c.key}, c.redactions...) {
		if secret != "" {
			s = strings.ReplaceAll(s, secret, "[redacted]")
		}
	}
	return s
}
