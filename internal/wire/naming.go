package wire

import (
	"context"
	"fmt"
)

// TRaSH-recommended naming, verbatim from the guides' own JSON data
// (docs/json/{sonarr,radarr}/naming/ — the "default" variants). Set through
// the arrs' own config API: Recyclarr v8 dropped its naming presets (its
// `list naming` returns nothing), so the direct route is the stable one.
// These strings are maintenance data of the same class as the trash_ids:
// they change when the guides change, and the guides change them rarely.
const (
	sonarrStandardFormat = "{Series CleanTitleWithoutYear} {(Series Year)} - S{season:00}E{episode:00} - {Episode CleanTitle:90} {[Custom Formats]}{[Quality Full]}{[Mediainfo AudioCodec}{ Mediainfo AudioChannels]}{[MediaInfo VideoDynamicRangeType]}{[Mediainfo VideoCodec]}{-Release Group}"
	sonarrDailyFormat    = "{Series CleanTitleWithoutYear} {(Series Year)} - {Air-Date} - {Episode CleanTitle:90} {[Custom Formats]}{[Quality Full]}{[Mediainfo AudioCodec}{ Mediainfo AudioChannels]}{[MediaInfo VideoDynamicRangeType]}{[Mediainfo VideoCodec]}{-Release Group}"
	sonarrAnimeFormat    = "{Series CleanTitleWithoutYear} {(Series Year)} - S{season:00}E{episode:00} - {absolute:000} - {Episode CleanTitle:90} {[Custom Formats]}{[Quality Full]}{[Mediainfo AudioCodec}{ Mediainfo AudioChannels]}{MediaInfo AudioLanguages}{[MediaInfo VideoDynamicRangeType]}[{Mediainfo VideoCodec }{MediaInfo VideoBitDepth}bit]{-Release Group}"
	sonarrSeasonFolder   = "Season {season:00}"
)

// Folder (and, for Radarr, file) formats vary BY MEDIA SERVER: the guides
// publish per-server variants that embed a database ID in the folder name
// — it prevents misidentification of similarly-named titles, and each
// server wants its own bracket-and-ID dialect. The wiring picks the
// variant matching the selected server; the empty key is the plain
// default when no server is in the stack. Verbatim from the guides' JSON.
var sonarrSeriesFolderByServer = map[string]string{
	"":         "{Series CleanTitleWithoutYear} {(Series Year)}",
	"jellyfin": "{Series CleanTitleWithoutYear} {(Series Year)} [tvdbid-{TvdbId}]",
	"plex":     "{Series CleanTitleWithoutYear} {(Series Year)} {imdb-{ImdbId}}",
	"emby":     "{Series CleanTitleWithoutYear} {(Series Year)} [imdb-{ImdbId}]",
}

var radarrMovieFolderByServer = map[string]string{
	"":         "{Movie CleanTitle} ({Release Year})",
	"jellyfin": "{Movie CleanTitle} ({Release Year}) [imdbid-{ImdbId}]",
	"plex":     "{Movie CleanTitle} ({Release Year}) {imdb-{ImdbId}}",
	"emby":     "{Movie CleanTitle} ({Release Year}) [imdb-{ImdbId}]",
}

var radarrMovieFormatByServer = map[string]string{
	"":         "{Movie CleanTitle} {(Release Year)} - {{Edition Tags}} {[MediaInfo 3D]}{[Custom Formats]}{[Quality Full]}{[Mediainfo AudioCodec}{ Mediainfo AudioChannels]}{[MediaInfo VideoDynamicRangeType]}{[Mediainfo VideoCodec]}{-Release Group}",
	"jellyfin": "{Movie CleanTitle} {(Release Year)} [imdbid-{ImdbId}] - {{Edition Tags}} {[MediaInfo 3D]}{[Custom Formats]}{[Quality Full]}{[Mediainfo AudioCodec}{ Mediainfo AudioChannels]}{[MediaInfo VideoDynamicRangeType]}{[Mediainfo VideoCodec]}{-Release Group}",
	"plex":     "{Movie CleanTitle} {(Release Year)} {imdb-{ImdbId}} - {edition-{Edition Tags}} {[MediaInfo 3D]}{[Custom Formats]}{[Quality Full]}{[Mediainfo AudioCodec}{ Mediainfo AudioChannels]}{[MediaInfo VideoDynamicRangeType]}{[Mediainfo VideoCodec]}{-Release Group}",
	"emby":     "{Movie CleanTitle} {(Release Year)} [imdb-{ImdbId}] - {edition-{Edition Tags}} {[MediaInfo 3D]}{[Custom Formats]}{[Quality Full]}{[Mediainfo AudioCodec}{ Mediainfo AudioChannels]}{[MediaInfo VideoDynamicRangeType]}{[Mediainfo VideoCodec]}{-Release Group}",
}

// multiEpisodeStylePrefixedRange is Sonarr's enum value for the guide's
// recommended "Prefixed Range" style (S01E01-E03).
const multiEpisodeStylePrefixedRange = 5

// EnsureSonarrNaming applies the TRaSH naming scheme to a FRESH Sonarr:
// rename on, the recommended formats, prefixed-range multi-episodes, and
// the folder-ID variant matching the stack's media server. An adopted
// Sonarr's naming is the user's — one ● line, zero writes.
func EnsureSonarrNaming(ctx context.Context, c *Client, mediaServer string, adopted bool) Result {
	conn := "Sonarr ← TRaSH naming scheme"
	if adopted {
		return Result{Connection: conn, Outcome: OutcomeExisted,
			Detail: "naming left exactly as the adopted config had it"}
	}
	seriesFolder := sonarrSeriesFolderByServer[mediaServer]

	var cfg map[string]any
	if err := c.GetJSON(ctx, "/api/v3/config/naming", &cfg); err != nil {
		return Result{Connection: conn, Outcome: OutcomeFailed,
			Detail: fmt.Sprintf("reading naming config: %v", err)}
	}
	if cfg["renameEpisodes"] == true && cfg["standardEpisodeFormat"] == sonarrStandardFormat &&
		cfg["seriesFolderFormat"] == seriesFolder {
		return Result{Connection: conn, Outcome: OutcomeExisted}
	}

	cfg["renameEpisodes"] = true
	cfg["standardEpisodeFormat"] = sonarrStandardFormat
	cfg["dailyEpisodeFormat"] = sonarrDailyFormat
	cfg["animeEpisodeFormat"] = sonarrAnimeFormat
	cfg["seriesFolderFormat"] = seriesFolder
	cfg["seasonFolderFormat"] = sonarrSeasonFolder
	cfg["multiEpisodeStyle"] = multiEpisodeStylePrefixedRange
	if err := putConfig(ctx, c, "/api/v3/config/naming", cfg); err != nil {
		return Result{Connection: conn, Outcome: OutcomeFailed,
			Detail: fmt.Sprintf("applying naming: %v", err)}
	}
	return Result{Connection: conn, Outcome: OutcomeWired}
}

// EnsureRadarrNaming is the movie twin of EnsureSonarrNaming.
func EnsureRadarrNaming(ctx context.Context, c *Client, mediaServer string, adopted bool) Result {
	conn := "Radarr ← TRaSH naming scheme"
	if adopted {
		return Result{Connection: conn, Outcome: OutcomeExisted,
			Detail: "naming left exactly as the adopted config had it"}
	}
	movieFormat := radarrMovieFormatByServer[mediaServer]

	var cfg map[string]any
	if err := c.GetJSON(ctx, "/api/v3/config/naming", &cfg); err != nil {
		return Result{Connection: conn, Outcome: OutcomeFailed,
			Detail: fmt.Sprintf("reading naming config: %v", err)}
	}
	if cfg["renameMovies"] == true && cfg["standardMovieFormat"] == movieFormat {
		return Result{Connection: conn, Outcome: OutcomeExisted}
	}

	cfg["renameMovies"] = true
	cfg["standardMovieFormat"] = movieFormat
	cfg["movieFolderFormat"] = radarrMovieFolderByServer[mediaServer]
	if err := putConfig(ctx, c, "/api/v3/config/naming", cfg); err != nil {
		return Result{Connection: conn, Outcome: OutcomeFailed,
			Detail: fmt.Sprintf("applying naming: %v", err)}
	}
	return Result{Connection: conn, Outcome: OutcomeWired}
}

// stockProfileNames are the factory quality profiles every arr ships with.
var stockProfileNames = map[string]bool{
	"Any": true, "SD": true, "HD-720p": true, "HD-1080p": true,
	"HD - 720p/1080p": true, "Ultra-HD": true,
}

// CleanupStockProfiles deletes the arr's factory quality profiles once the
// TRaSH-named ones exist — a dropdown offering "Any" next to a curated
// WEB-1080p invites picking the wrong one. Seatbelts: FRESH installs only
// (the caller gates), and never when deleting would leave the arr without
// a replacement profile (a failed Recyclarr sync must not strand it).
func CleanupStockProfiles(ctx context.Context, c *Client, apiBase, arrName string) Result {
	conn := arrName + " ← stock quality profiles removed"

	var profiles []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	if err := c.GetJSON(ctx, apiBase+"/qualityprofile", &profiles); err != nil {
		return Result{Connection: conn, Outcome: OutcomeFailed,
			Detail: fmt.Sprintf("reading quality profiles: %v", err)}
	}
	var stock []int
	nonStock := 0
	for _, p := range profiles {
		if stockProfileNames[p.Name] {
			stock = append(stock, p.ID)
		} else {
			nonStock++
		}
	}
	if len(stock) == 0 {
		return Result{Connection: conn, Outcome: OutcomeExisted}
	}
	if nonStock == 0 {
		return Result{Connection: conn, Outcome: OutcomeManual,
			Detail: "no replacement profiles exist yet (did the TRaSH sync fail?) — stock profiles kept"}
	}

	removed := 0
	for _, id := range stock {
		if err := c.Delete(ctx, fmt.Sprintf("%s/qualityprofile/%d", apiBase, id)); err != nil {
			// In use or refused — leave it; this is housekeeping, not law.
			continue
		}
		removed++
	}
	return Result{Connection: conn, Outcome: OutcomeWired,
		Detail: fmt.Sprintf("%d removed — only the TRaSH profiles remain", removed)}
}

// EnsureMediaManagement applies the guide's companion toggles to a FRESH
// arr: propers/repacks to "Do Not Prefer" (the custom formats score them
// now — the old toggle fights the CF system), analyze video files on
// (accurate mediainfo prevents re-download loops), and sidecar-subtitle
// import (srt only — releases often ship subs that would otherwise be
// discarded, and Bazarr tracks imported ones). On Sonarr, episode titles
// are required only for season packs: the default "always" stalls daily
// and freshly-aired episodes in the queue while TVDB still says TBA.
func EnsureMediaManagement(ctx context.Context, c *Client, apiBase, arrName string, adopted bool) Result {
	conn := arrName + " ← media management defaults"
	if adopted {
		return Result{Connection: conn, Outcome: OutcomeExisted,
			Detail: "left exactly as the adopted config had it"}
	}

	var cfg map[string]any
	if err := c.GetJSON(ctx, apiBase+"/config/mediamanagement", &cfg); err != nil {
		return Result{Connection: conn, Outcome: OutcomeFailed,
			Detail: fmt.Sprintf("reading media management config: %v", err)}
	}
	if cfg["downloadPropersAndRepacks"] == "doNotPrefer" && cfg["enableMediaInfo"] == true &&
		cfg["importExtraFiles"] == true {
		return Result{Connection: conn, Outcome: OutcomeExisted}
	}

	cfg["downloadPropersAndRepacks"] = "doNotPrefer"
	cfg["enableMediaInfo"] = true
	cfg["importExtraFiles"] = true
	cfg["extraFileExtensions"] = "srt"
	if _, isSonarr := cfg["episodeTitleRequired"]; isSonarr {
		cfg["episodeTitleRequired"] = "bulkSeasonReleases"
	}
	if err := putConfig(ctx, c, apiBase+"/config/mediamanagement", cfg); err != nil {
		return Result{Connection: conn, Outcome: OutcomeFailed,
			Detail: fmt.Sprintf("applying media management defaults: %v", err)}
	}
	return Result{Connection: conn, Outcome: OutcomeWired}
}

// putConfig writes a fetched-and-mutated config resource back, targeting
// the /{id} path the arrs expect for updates. Unknown fields ride through
// intact — the same loose-map discipline as EnsureAuth.
func putConfig(ctx context.Context, c *Client, base string, cfg map[string]any) error {
	path := base
	if id, ok := cfg["id"]; ok {
		path = fmt.Sprintf("%s/%v", base, id)
	}
	return c.PutJSON(ctx, path, cfg, nil)
}
