package wire

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// qBittorrent category pre-seed (issue #107): the arrs hand qBittorrent a
// category per PVR (EnsureDownloadClient), but a bare category saves into
// the default download path — outside /data/torrents, where imports COPY
// instead of hardlink. Registering each category with a save path under
// /data/torrents keeps every download on the shared tree from the first
// grab.

// NewQBitSession logs into qBittorrent's WebUI API and returns a client
// carrying the session cookie. qBittorrent's CSRF guard rejects logins
// without a Referer matching the target; its login endpoint answers 200
// with a plain-text "Fails." on a bad password, hence the body check.
func NewQBitSession(ctx context.Context, base, user, pass string) (*Client, error) {
	c := NewClient(base, "", "").WithCookies().WithRedaction(pass).WithHeader("Referer", base)
	var body string
	err := c.PostForm(ctx, "/api/v2/auth/login",
		url.Values{"username": {user}, "password": {pass}}, &body)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(body, "Ok") {
		return nil, fmt.Errorf("qBittorrent rejected the WebUI credentials")
	}
	return c, nil
}

// EnsureQBitCategory registers one category with its save path. An existing
// category of the same name is left exactly as it is — whatever its save
// path, it is the user's arrangement.
func EnsureQBitCategory(ctx context.Context, c *Client, name, savePath string) Result {
	conn := fmt.Sprintf("qBittorrent ← category %q → %s", name, savePath)

	var existing map[string]struct {
		Name     string `json:"name"`
		SavePath string `json:"savePath"`
	}
	if err := c.GetJSON(ctx, "/api/v2/torrents/categories", &existing); err != nil {
		return Result{Connection: conn, Outcome: OutcomeFailed,
			Detail: fmt.Sprintf("listing categories: %v", err)}
	}
	if _, ok := existing[name]; ok {
		return Result{Connection: conn, Outcome: OutcomeExisted}
	}

	err := c.PostForm(ctx, "/api/v2/torrents/createCategory",
		url.Values{"category": {name}, "savePath": {savePath}}, nil)
	if err != nil {
		return Result{Connection: conn, Outcome: OutcomeFailed, Detail: err.Error()}
	}
	return Result{Connection: conn, Outcome: OutcomeWired}
}
