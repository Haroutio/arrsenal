package wire

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// fakeArrConfig serves one GET/PUT config resource the way the arrs do:
// GET returns the singleton, PUT goes to /{id}.
type fakeArrConfig struct {
	path    string // e.g. /api/v3/config/naming
	current map[string]any
	puts    atomic.Int32
	lastPut map[string]any
}

func (f *fakeArrConfig) server() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+f.path, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(f.current)
	})
	mux.HandleFunc("PUT "+f.path+"/1", func(w http.ResponseWriter, r *http.Request) {
		f.puts.Add(1)
		_ = json.NewDecoder(r.Body).Decode(&f.lastPut)
		_, _ = w.Write([]byte(`{}`))
	})
	return httptest.NewServer(mux)
}

func TestEnsureSonarrNamingApplies(t *testing.T) {
	f := &fakeArrConfig{path: "/api/v3/config/naming", current: map[string]any{
		"id": 1, "renameEpisodes": false, "standardEpisodeFormat": "{Series Title} - {Episode Title}",
		"replaceIllegalCharacters": true, // a field we don't manage — must survive
	}}
	srv := f.server()
	defer srv.Close()

	r := EnsureSonarrNaming(context.Background(), wireClient(srv.URL), "", false)
	if r.Outcome != OutcomeWired || f.puts.Load() != 1 {
		t.Fatalf("fresh sonarr must be wired: %+v puts=%d", r, f.puts.Load())
	}
	if f.lastPut["renameEpisodes"] != true || f.lastPut["standardEpisodeFormat"] != sonarrStandardFormat {
		t.Fatalf("naming not applied: %+v", f.lastPut)
	}
	if f.lastPut["seasonFolderFormat"] != sonarrSeasonFolder || f.lastPut["multiEpisodeStyle"] != float64(multiEpisodeStylePrefixedRange) {
		t.Fatalf("folder/style not applied: %+v", f.lastPut)
	}
	if f.lastPut["replaceIllegalCharacters"] != true {
		t.Fatalf("unmanaged field lost in roundtrip: %+v", f.lastPut)
	}
}

func TestEnsureNamingAdoptedMeansZeroRequests(t *testing.T) {
	// No server at all: an adopted arr must short-circuit before any HTTP.
	c := NewClient("http://127.0.0.1:1", "key", "X-Api-Key")

	if r := EnsureSonarrNaming(context.Background(), c, "", true); r.Outcome != OutcomeExisted {
		t.Fatalf("adopted sonarr naming must be existed: %+v", r)
	}
	if r := EnsureRadarrNaming(context.Background(), c, "", true); r.Outcome != OutcomeExisted {
		t.Fatalf("adopted radarr naming must be existed: %+v", r)
	}
	if r := EnsureMediaManagement(context.Background(), c, "/api/v3", "Sonarr", true); r.Outcome != OutcomeExisted {
		t.Fatalf("adopted media management must be existed: %+v", r)
	}
}

func TestEnsureSonarrNamingIdempotent(t *testing.T) {
	f := &fakeArrConfig{path: "/api/v3/config/naming", current: map[string]any{
		"id": 1, "renameEpisodes": true, "standardEpisodeFormat": sonarrStandardFormat,
		"seriesFolderFormat": sonarrSeriesFolderByServer[""],
	}}
	srv := f.server()
	defer srv.Close()

	r := EnsureSonarrNaming(context.Background(), wireClient(srv.URL), "", false)
	if r.Outcome != OutcomeExisted || f.puts.Load() != 0 {
		t.Fatalf("second pass must not rewrite: %+v puts=%d", r, f.puts.Load())
	}
}

func TestEnsureRadarrNamingApplies(t *testing.T) {
	f := &fakeArrConfig{path: "/api/v3/config/naming", current: map[string]any{
		"id": 1, "renameMovies": false, "standardMovieFormat": "{Movie Title} ({Release Year})",
	}}
	srv := f.server()
	defer srv.Close()

	r := EnsureRadarrNaming(context.Background(), wireClient(srv.URL), "", false)
	if r.Outcome != OutcomeWired || f.puts.Load() != 1 {
		t.Fatalf("fresh radarr must be wired: %+v puts=%d", r, f.puts.Load())
	}
	if f.lastPut["renameMovies"] != true || f.lastPut["standardMovieFormat"] != radarrMovieFormatByServer[""] || f.lastPut["movieFolderFormat"] != radarrMovieFolderByServer[""] {
		t.Fatalf("naming not applied: %+v", f.lastPut)
	}
}

func TestNamingFolderVariantFollowsMediaServer(t *testing.T) {
	// Jellyfin stack → tvdbid folder tag on Sonarr, imdbid on Radarr; the
	// guides publish these per-server variants and the wiring picks by
	// selected server instead of asking.
	f := &fakeArrConfig{path: "/api/v3/config/naming", current: map[string]any{"id": 1}}
	srv := f.server()
	defer srv.Close()
	if r := EnsureSonarrNaming(context.Background(), wireClient(srv.URL), "jellyfin", false); r.Outcome != OutcomeWired {
		t.Fatalf("jellyfin sonarr naming: %+v", r)
	}
	if f.lastPut["seriesFolderFormat"] != "{Series CleanTitleWithoutYear} {(Series Year)} [tvdbid-{TvdbId}]" {
		t.Fatalf("jellyfin series folder variant: %v", f.lastPut["seriesFolderFormat"])
	}

	rf := &fakeArrConfig{path: "/api/v3/config/naming", current: map[string]any{"id": 1}}
	rsrv := rf.server()
	defer rsrv.Close()
	if r := EnsureRadarrNaming(context.Background(), wireClient(rsrv.URL), "jellyfin", false); r.Outcome != OutcomeWired {
		t.Fatalf("jellyfin radarr naming: %+v", r)
	}
	if rf.lastPut["movieFolderFormat"] != "{Movie CleanTitle} ({Release Year}) [imdbid-{ImdbId}]" {
		t.Fatalf("jellyfin movie folder variant: %v", rf.lastPut["movieFolderFormat"])
	}
	if got, _ := rf.lastPut["standardMovieFormat"].(string); !strings.Contains(got, "[imdbid-{ImdbId}]") {
		t.Fatalf("jellyfin movie FILE format must carry the id too: %v", got)
	}

	// Plex uses curly imdb tags.
	pf := &fakeArrConfig{path: "/api/v3/config/naming", current: map[string]any{"id": 1}}
	psrv := pf.server()
	defer psrv.Close()
	if r := EnsureSonarrNaming(context.Background(), wireClient(psrv.URL), "plex", false); r.Outcome != OutcomeWired {
		t.Fatalf("plex sonarr naming: %+v", r)
	}
	if pf.lastPut["seriesFolderFormat"] != "{Series CleanTitleWithoutYear} {(Series Year)} {imdb-{ImdbId}}" {
		t.Fatalf("plex series folder variant: %v", pf.lastPut["seriesFolderFormat"])
	}
}

// fakeProfiles serves qualityprofile list + delete.
type fakeProfiles struct {
	profiles string
	deletes  []string
}

func (f *fakeProfiles) server() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v3/qualityprofile", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(f.profiles))
	})
	mux.HandleFunc("DELETE /api/v3/qualityprofile/", func(w http.ResponseWriter, r *http.Request) {
		f.deletes = append(f.deletes, r.URL.Path)
		w.WriteHeader(http.StatusOK)
	})
	return httptest.NewServer(mux)
}

func TestCleanupStockProfiles(t *testing.T) {
	// TRaSH profiles present → the six factory ones go.
	f := &fakeProfiles{profiles: `[
		{"id":1,"name":"Any"},{"id":2,"name":"SD"},{"id":3,"name":"HD-720p"},
		{"id":4,"name":"HD-1080p"},{"id":5,"name":"HD - 720p/1080p"},{"id":6,"name":"Ultra-HD"},
		{"id":7,"name":"WEB-1080p"}]`}
	srv := f.server()
	defer srv.Close()
	r := CleanupStockProfiles(context.Background(), wireClient(srv.URL), "/api/v3", "Sonarr")
	if r.Outcome != OutcomeWired || len(f.deletes) != 6 {
		t.Fatalf("stock must be removed: %+v deletes=%v", r, f.deletes)
	}

	// ONLY stock profiles (TRaSH sync failed) → refuse to strand the arr.
	f2 := &fakeProfiles{profiles: `[{"id":1,"name":"Any"},{"id":2,"name":"HD-1080p"}]`}
	srv2 := f2.server()
	defer srv2.Close()
	r = CleanupStockProfiles(context.Background(), wireClient(srv2.URL), "/api/v3", "Sonarr")
	if r.Outcome != OutcomeManual || len(f2.deletes) != 0 {
		t.Fatalf("must keep stock when nothing replaces it: %+v deletes=%v", r, f2.deletes)
	}

	// Second run: stock already gone → existed.
	f3 := &fakeProfiles{profiles: `[{"id":7,"name":"WEB-1080p"}]`}
	srv3 := f3.server()
	defer srv3.Close()
	r = CleanupStockProfiles(context.Background(), wireClient(srv3.URL), "/api/v3", "Sonarr")
	if r.Outcome != OutcomeExisted || len(f3.deletes) != 0 {
		t.Fatalf("clean arr must be existed: %+v", r)
	}
}

func TestEnsureMediaManagementApplies(t *testing.T) {
	f := &fakeArrConfig{path: "/api/v3/config/mediamanagement", current: map[string]any{
		"id": 1, "downloadPropersAndRepacks": "preferAndUpgrade", "enableMediaInfo": false,
		"importExtraFiles": false, "extraFileExtensions": "",
		"episodeTitleRequired": "always",        // sonarr-only key → the sonarr branch
		"recycleBin":           "/data/recycle", // unmanaged — must survive
	}}
	srv := f.server()
	defer srv.Close()

	r := EnsureMediaManagement(context.Background(), wireClient(srv.URL), "/api/v3", "Sonarr", false)
	if r.Outcome != OutcomeWired || f.puts.Load() != 1 {
		t.Fatalf("fresh arr must be wired: %+v puts=%d", r, f.puts.Load())
	}
	if f.lastPut["downloadPropersAndRepacks"] != "doNotPrefer" || f.lastPut["enableMediaInfo"] != true {
		t.Fatalf("defaults not applied: %+v", f.lastPut)
	}
	if f.lastPut["importExtraFiles"] != true || f.lastPut["extraFileExtensions"] != "srt" {
		t.Fatalf("sidecar-subtitle import not applied: %+v", f.lastPut)
	}
	if f.lastPut["episodeTitleRequired"] != "bulkSeasonReleases" {
		t.Fatalf("episode-title stall guard not applied: %+v", f.lastPut)
	}
	if f.lastPut["recycleBin"] != "/data/recycle" {
		t.Fatalf("unmanaged field lost in roundtrip: %+v", f.lastPut)
	}

	// Radarr has no episodeTitleRequired key — the sonarr branch must not
	// invent one.
	rf := &fakeArrConfig{path: "/api/v3/config/mediamanagement", current: map[string]any{
		"id": 1, "downloadPropersAndRepacks": "preferAndUpgrade", "enableMediaInfo": false,
	}}
	rsrv := rf.server()
	defer rsrv.Close()
	if r := EnsureMediaManagement(context.Background(), wireClient(rsrv.URL), "/api/v3", "Radarr", false); r.Outcome != OutcomeWired {
		t.Fatalf("radarr path: %+v", r)
	}
	if _, invented := rf.lastPut["episodeTitleRequired"]; invented {
		t.Fatalf("episodeTitleRequired invented on radarr: %+v", rf.lastPut)
	}

	// Second pass over the now-correct config: no rewrite.
	f.current = f.lastPut
	r = EnsureMediaManagement(context.Background(), wireClient(srv.URL), "/api/v3", "Sonarr", false)
	if r.Outcome != OutcomeExisted || f.puts.Load() != 1 {
		t.Fatalf("second pass must not rewrite: %+v puts=%d", r, f.puts.Load())
	}
}
