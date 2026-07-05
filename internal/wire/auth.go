package wire

import (
	"context"
	"fmt"
)

// EnsureAuth configures forms authentication on an arr app with the single
// admin credential (DESIGN.md §7.3, §9.2) so nobody clicks through five
// identical first-run screens. The password is used, not kept: it lives in
// the arguments of this call and nowhere else.
//
// adopted marks apps whose appdata predates this run (preflight's adoption
// notice). Any auth an adopted app HAS is the user's configuration and is
// never modified. Method "none", though, is not a configuration — the
// modern arrs don't even offer it in the UI; it means the first-run auth
// screen was never completed (the app nags until it is). Completing it with
// the provided credential clobbers nothing, so adoption does not block it —
// a field report proved the old behavior just preserved the nag forever.
//
// apiBase is the arr-family prefix: "/api/v3" for the PVRs, "/api/v1" for
// Prowlarr — same host-config resource, different mount point.
func EnsureAuth(ctx context.Context, c *Client, appName, apiBase, username, password string, adopted bool) Result {
	conn := fmt.Sprintf("%s ← admin credential", appName)
	c.WithRedaction(password) // the body carries it; echoes must not

	// The host config is fetched as a loose map and written back the same
	// way: only the auth keys change, every other field — including ones
	// added by arr versions newer than this binary — rides through intact.
	var host map[string]any
	if err := c.GetJSON(ctx, apiBase+"/config/host", &host); err != nil {
		return Result{Connection: conn, Outcome: OutcomeFailed,
			Detail: fmt.Sprintf("reading host config: %v", err)}
	}

	method, _ := host["authenticationMethod"].(string)
	if method != "" && method != "none" {
		return Result{Connection: conn, Outcome: OutcomeExisted} // already authed
	}
	detail := ""
	if adopted {
		detail = "auth was never set up in the adopted config — completed it with your admin credential"
	}

	host["authenticationMethod"] = "forms"
	host["authenticationRequired"] = "enabled"
	host["username"] = username
	host["password"] = password
	host["passwordConfirmation"] = password
	// Privacy default while we hold the pen on a never-configured app:
	// the arrs ship with anonymous usage telemetry ON; a self-hosted stack
	// an installer provisions should not phone home (research-audit
	// finding). Takes effect on the app's next restart.
	host["analyticsEnabled"] = false

	path := apiBase + "/config/host"
	if id, ok := host["id"]; ok {
		path = fmt.Sprintf("%s/%v", path, id)
	}
	if err := c.PutJSON(ctx, path, host, nil); err != nil {
		return Result{Connection: conn, Outcome: OutcomeFailed,
			Detail: fmt.Sprintf("applying auth settings: %v", err)}
	}
	return Result{Connection: conn, Outcome: OutcomeWired, Detail: detail}
}
