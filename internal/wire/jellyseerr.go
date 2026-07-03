package wire

import (
	"context"
	"fmt"
	"time"
)

// EnsureJellyseerr handles the semi-auto tier (DESIGN.md §7, registry WiringSemiAuto).
// Its first-run setup is deliberately NOT automated:
//
//   - Plex accounts require a browser OAuth login — impossible headlessly.
//   - Even the Jellyfin path (where Arrsenal holds the admin credential)
//     needs an undocumented multi-step init: settings can't be written
//     pre-auth (401), and the first-user auth endpoint drives a
//     version-specific sequence (NO_ADMIN_USER / INVALID_URL) that the
//     project — freshly rebranded Jellyseerr → Seerr and actively churning
//     — offers no stable contract for. Investigated against a live pair;
//     the maintenance risk dwarfs the payoff for a wizard that takes the
//     user two minutes, once.
//
// So the honest job here: confirm reachability and setup state, leave a
// configured instance untouched, and for a fresh one point the user at its
// own wizard. Never block the run.
func EnsureJellyseerr(ctx context.Context, url, hostAccessURL string) Result {
	conn := "Seerr requests"
	// Best-effort: never stall the run on a Seerr that may be down.
	c := NewClient(url, "", "").WithRetry(2, 500*time.Millisecond)

	// The public settings endpoint needs no auth and reports init state.
	var pub struct {
		Initialized bool `json:"initialized"`
	}
	if err := c.GetJSON(ctx, "/api/v1/settings/public", &pub); err != nil {
		return Result{Connection: conn, Outcome: OutcomeManual,
			Detail:      fmt.Sprintf("could not reach Seerr to check its setup (%v) — finish it at its web UI", err),
			FallbackURL: hostAccessURL}
	}
	if pub.Initialized {
		return Result{Connection: conn, Outcome: OutcomeExisted,
			Detail: "already set up — left as configured"}
	}
	return Result{Connection: conn, Outcome: OutcomeManual,
		Detail: "installed and running — finish its 2-minute setup wizard (sign in with your Jellyfin, Plex or Emby account, " +
			"then add Sonarr/Radarr); it cannot be automated because that sign-in needs a browser",
		FallbackURL: hostAccessURL}
}
