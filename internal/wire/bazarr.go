package wire

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

// Bazarr language pre-seed (issue #107). A fresh Bazarr has NO language
// profile — subtitles cannot flow until one exists and is set as the
// default, and that lives in Bazarr's database, not its config.yaml. So
// this is an API lane, run AFTER the tail apps boot (OrchestrateTail): it
// creates an English profile and enables it as the default for series and
// movies. The user still adds a provider account (their credentials, their
// choice of provider) — but that is then the ONLY step left.
//
// The settings endpoint is form-encoded; field shapes verified against
// Bazarr's own handler (bazarr/api/system/settings.py): languages-profiles
// is a JSON array, settings live under settings-<section>-<name> keys.

// bazarrEnglishProfile is the one profile we create, on profileId 1 — the
// lane only runs when the profile table is empty, so 1 is always free.
const bazarrEnglishProfile = `[{"profileId":1,"name":"English","items":[{"id":1,"language":"en","audio_exclude":"False","hi":"False","forced":"False"}],"cutoff":null,"mustContain":[],"mustNotContain":[],"originalFormat":false}]`

// EnsureBazarrLanguages makes English the working default on a fresh
// Bazarr. Any existing profile — adopted install or a user's own — means
// the whole lane backs off.
func EnsureBazarrLanguages(ctx context.Context, c *Client, adopted bool) Result {
	conn := "Bazarr ← English subtitles by default"
	if adopted {
		return Result{Connection: conn, Outcome: OutcomeExisted,
			Detail: "languages left exactly as the adopted config had them"}
	}

	var profiles []json.RawMessage
	if err := c.GetJSON(ctx, "/api/system/languages/profiles", &profiles); err != nil {
		return Result{Connection: conn, Outcome: OutcomeFailed,
			Detail: fmt.Sprintf("reading language profiles: %v", err)}
	}
	if len(profiles) > 0 {
		return Result{Connection: conn, Outcome: OutcomeExisted}
	}

	err := c.PostForm(ctx, "/api/system/settings", url.Values{
		"languages-enabled":                      {"en"},
		"languages-profiles":                     {bazarrEnglishProfile},
		"settings-general-serie_default_enabled": {"true"},
		"settings-general-serie_default_profile": {"1"},
		"settings-general-movie_default_enabled": {"true"},
		"settings-general-movie_default_profile": {"1"},
	}, nil)
	if err != nil {
		return Result{Connection: conn, Outcome: OutcomeFailed,
			Detail: fmt.Sprintf("applying language defaults: %v", err)}
	}
	return Result{Connection: conn, Outcome: OutcomeWired,
		Detail: "add a subtitle provider account in Bazarr's UI to start downloading"}
}
