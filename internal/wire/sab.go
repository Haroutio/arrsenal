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
