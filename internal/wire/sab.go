package wire

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// NewSABClient builds a client for SABnzbd, whose API is query-param based:
// mode + apikey ride in the URL, JSON comes out. No key header is sent; the
// key lives in each path and the redaction machinery scrubs it from errors.
func NewSABClient(base, apiKey string) *Client {
	return NewClient(base, apiKey, "")
}

// sabPath builds an API path with the key and mode baked in.
func sabPath(key, mode string, extra url.Values) string {
	v := url.Values{}
	v.Set("mode", mode)
	v.Set("apikey", key)
	v.Set("output", "json")
	for k, vals := range extra {
		for _, val := range vals {
			v.Add(k, val)
		}
	}
	return "/api?" + v.Encode()
}

// EnsureSABFolders points SABnzbd's download directories at the shared data
// tree — but ONLY when they still sit at SAB's stock defaults. A fresh
// install downloads into its own config volume (Downloads/…), which
// silently breaks the hardlink promise; a customized value is the user's
// choice and is never touched.
func EnsureSABFolders(ctx context.Context, sab *Client) Result {
	conn := "SABnzbd ← download folders under /data/usenet"

	var cfg struct {
		Config struct {
			Misc struct {
				DownloadDir string `json:"download_dir"`
				CompleteDir string `json:"complete_dir"`
			} `json:"misc"`
		} `json:"config"`
	}
	if err := sab.GetJSON(ctx, sabPath(sab.key, "get_config", url.Values{"section": {"misc"}}), &cfg); err != nil {
		return Result{Connection: conn, Outcome: OutcomeFailed,
			Detail: fmt.Sprintf("reading SABnzbd config: %v", err)}
	}

	misc := cfg.Config.Misc
	wantIncomplete, wantComplete := "/data/usenet/incomplete", "/data/usenet/complete"
	if misc.DownloadDir == wantIncomplete && misc.CompleteDir == wantComplete {
		return Result{Connection: conn, Outcome: OutcomeExisted}
	}
	if misc.DownloadDir != "Downloads/incomplete" || misc.CompleteDir != "Downloads/complete" {
		return Result{Connection: conn, Outcome: OutcomeExisted,
			Detail: fmt.Sprintf("left as configured (%s / %s) — not SAB defaults, so they are the user's",
				misc.DownloadDir, misc.CompleteDir)}
	}

	for keyword, value := range map[string]string{
		"download_dir": wantIncomplete,
		"complete_dir": wantComplete,
	} {
		err := sab.GetJSON(ctx, sabPath(sab.key, "set_config",
			url.Values{"section": {"misc"}, "keyword": {keyword}, "value": {value}}), nil)
		if err != nil {
			return Result{Connection: conn, Outcome: OutcomeFailed,
				Detail: fmt.Sprintf("setting %s: %v", keyword, err)}
		}
	}
	return Result{Connection: conn, Outcome: OutcomeWired}
}

// EnsureSABCategory creates a download category (tv/movies/music) whose
// directory is the category name — landing completed downloads in
// complete/<name>, where the arrs' import paths expect them. Fresh SAB has
// no categories at all, and an arr refuses a download client whose category
// does not exist (found live).
func EnsureSABCategory(ctx context.Context, sab *Client, name string) Result {
	conn := fmt.Sprintf("SABnzbd ← category %q", name)

	var cfg struct {
		Config struct {
			Categories []struct {
				Name string `json:"name"`
			} `json:"categories"`
		} `json:"config"`
	}
	if err := sab.GetJSON(ctx, sabPath(sab.key, "get_config", url.Values{"section": {"categories"}}), &cfg); err != nil {
		return Result{Connection: conn, Outcome: OutcomeFailed,
			Detail: fmt.Sprintf("reading SABnzbd categories: %v", err)}
	}
	for _, c := range cfg.Config.Categories {
		if c.Name == name {
			return Result{Connection: conn, Outcome: OutcomeExisted}
		}
	}

	err := sab.GetJSON(ctx, sabPath(sab.key, "set_config",
		url.Values{"section": {"categories"}, "keyword": {name}, "name": {name}, "dir": {name}}), nil)
	if err != nil {
		return Result{Connection: conn, Outcome: OutcomeFailed,
			Detail: fmt.Sprintf("creating category: %v", err)}
	}
	return Result{Connection: conn, Outcome: OutcomeWired}
}

// EnsureSABWhitelist makes SABnzbd accept requests addressed to its
// container name. Fresh SAB-in-docker installs whitelist only the random
// hex hostname from first boot, so every container-name call — including
// the connection test an arr runs when registering SAB — dies with 403
// hostname verification. Found live; the fake servers never knew.
func EnsureSABWhitelist(ctx context.Context, sab *Client, hostname string) Result {
	conn := fmt.Sprintf("SABnzbd ← host whitelist %q", hostname)

	var cfg struct {
		Config struct {
			Misc struct {
				HostWhitelist []string `json:"host_whitelist"`
			} `json:"misc"`
		} `json:"config"`
	}
	if err := sab.GetJSON(ctx, sabPath(sab.key, "get_config", url.Values{"section": {"misc"}}), &cfg); err != nil {
		return Result{Connection: conn, Outcome: OutcomeFailed,
			Detail: fmt.Sprintf("reading SABnzbd config: %v", err)}
	}

	current := cfg.Config.Misc.HostWhitelist
	for _, h := range current {
		if h == hostname {
			return Result{Connection: conn, Outcome: OutcomeExisted}
		}
	}

	updated := strings.Join(append(append([]string{}, current...), hostname), ",")
	err := sab.GetJSON(ctx, sabPath(sab.key, "set_config",
		url.Values{"section": {"misc"}, "keyword": {"host_whitelist"}, "value": {updated}}), nil)
	if err != nil {
		return Result{Connection: conn, Outcome: OutcomeFailed,
			Detail: fmt.Sprintf("updating SABnzbd host whitelist: %v", err)}
	}
	return Result{Connection: conn, Outcome: OutcomeWired}
}

// UsenetProvider is one news server as SABnzbd should know it. Presets fill
// everything but the credentials; a custom provider is just a hostname.
type UsenetProvider struct {
	Name        string // display name, e.g. "Newshosting" — also SAB's server keyword
	Host        string
	Port        int
	SSL         bool
	Connections int
	Username    string
	Password    string
}

// UsenetPresets are the major commercial providers, keyed by the lowercase
// name a user types. Ports are the standard TLS endpoints; connection counts
// are each provider's documented allowance at the common tier (SAB treats
// too-high counts as errors mid-download, so conservative beats optimistic).
var UsenetPresets = map[string]UsenetProvider{
	"newshosting":  {Name: "Newshosting", Host: "news.newshosting.com", Port: 563, SSL: true, Connections: 30},
	"eweka":        {Name: "Eweka", Host: "news.eweka.nl", Port: 563, SSL: true, Connections: 20},
	"usenetserver": {Name: "UsenetServer", Host: "news.usenetserver.com", Port: 563, SSL: true, Connections: 20},
	"frugal":       {Name: "Frugal Usenet", Host: "news.frugalusenet.com", Port: 563, SSL: true, Connections: 30},
	"easynews":     {Name: "Easynews", Host: "news.easynews.com", Port: 563, SSL: true, Connections: 20},
}

// EnsureSABServer registers the news server — the piece without which the
// whole stack downloads nothing. Idempotent by host: any existing server
// with the same address is the user's and is never modified (not even the
// credentials — a typo'd password is fixed in SAB's UI, not by re-running
// the installer over a working config).
func EnsureSABServer(ctx context.Context, sab *Client, p UsenetProvider) Result {
	conn := fmt.Sprintf("SABnzbd ← usenet provider (%s)", p.Host)
	sab.WithRedaction(p.Password)

	var cfg struct {
		Config struct {
			Servers []struct {
				Host string `json:"host"`
			} `json:"servers"`
		} `json:"config"`
	}
	if err := sab.GetJSON(ctx, sabPath(sab.key, "get_config", url.Values{"section": {"servers"}}), &cfg); err != nil {
		return Result{Connection: conn, Outcome: OutcomeFailed,
			Detail: fmt.Sprintf("reading SABnzbd servers: %v", err)}
	}
	for _, s := range cfg.Config.Servers {
		if strings.EqualFold(s.Host, p.Host) {
			return Result{Connection: conn, Outcome: OutcomeExisted,
				Detail: "server already configured — left untouched"}
		}
	}

	ssl := "0"
	if p.SSL {
		ssl = "1"
	}
	err := sab.GetJSON(ctx, sabPath(sab.key, "set_config", url.Values{
		"section":     {"servers"},
		"keyword":     {p.Name},
		"host":        {p.Host},
		"port":        {fmt.Sprintf("%d", p.Port)},
		"ssl":         {ssl},
		"username":    {p.Username},
		"password":    {p.Password},
		"connections": {fmt.Sprintf("%d", p.Connections)},
		"enable":      {"1"},
	}), nil)
	if err != nil {
		return Result{Connection: conn, Outcome: OutcomeFailed,
			Detail: fmt.Sprintf("adding the server: %v", err)}
	}

	// Registration proves nothing about the credentials — SAB accepts any
	// config and only fails on the first real download. Its own test
	// endpoint (the UI's "Test Server" button) does a live NNTP login, so
	// a ✓ here means TESTED, and a typo'd password is a ⚠ now instead of
	// a mystery next week. Only new servers get here; existing entries
	// were never touched, so they are never probed either.
	var test struct {
		Value struct {
			Result  bool   `json:"result"`
			Message string `json:"message"`
		} `json:"value"`
	}
	err = sab.GetJSON(ctx, sabPath(sab.key, "config", url.Values{
		"name":        {"test_server"},
		"host":        {p.Host},
		"port":        {fmt.Sprintf("%d", p.Port)},
		"ssl":         {ssl},
		"username":    {p.Username},
		"password":    {p.Password},
		"connections": {"2"},
	}), &test)
	switch {
	case err != nil:
		return Result{Connection: conn, Outcome: OutcomeWired,
			Detail: "registered (connection test could not run)"}
	case !test.Value.Result:
		return Result{Connection: conn, Outcome: OutcomeManual,
			Detail: fmt.Sprintf("registered, but SABnzbd's connection test failed: %s — check the server in SAB's web UI (Settings → Servers)",
				test.Value.Message)}
	}
	return Result{Connection: conn, Outcome: OutcomeWired, Detail: "connection tested"}
}
