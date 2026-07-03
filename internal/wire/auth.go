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
// notice). Their auth state — including a deliberate "none" — is the user's
// configuration and is never modified; only method "none" on an app
// Arrsenal itself created counts as "not yet set up".
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
	if adopted {
		return Result{Connection: conn, Outcome: OutcomeExisted,
			Detail: "authentication left exactly as the adopted config had it"}
	}

	host["authenticationMethod"] = "forms"
	host["authenticationRequired"] = "enabled"
	host["username"] = username
	host["password"] = password
	host["passwordConfirmation"] = password

	path := apiBase + "/config/host"
	if id, ok := host["id"]; ok {
		path = fmt.Sprintf("%s/%v", path, id)
	}
	if err := c.PutJSON(ctx, path, host, nil); err != nil {
		return Result{Connection: conn, Outcome: OutcomeFailed,
			Detail: fmt.Sprintf("applying auth settings: %v", err)}
	}
	return Result{Connection: conn, Outcome: OutcomeWired}
}
