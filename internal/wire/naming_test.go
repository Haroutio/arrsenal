package wire

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

	r := EnsureSonarrNaming(context.Background(), wireClient(srv.URL), false)
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

	if r := EnsureSonarrNaming(context.Background(), c, true); r.Outcome != OutcomeExisted {
		t.Fatalf("adopted sonarr naming must be existed: %+v", r)
	}
	if r := EnsureRadarrNaming(context.Background(), c, true); r.Outcome != OutcomeExisted {
		t.Fatalf("adopted radarr naming must be existed: %+v", r)
	}
	if r := EnsureMediaManagement(context.Background(), c, "/api/v3", "Sonarr", true); r.Outcome != OutcomeExisted {
		t.Fatalf("adopted media management must be existed: %+v", r)
	}
}

func TestEnsureSonarrNamingIdempotent(t *testing.T) {
	f := &fakeArrConfig{path: "/api/v3/config/naming", current: map[string]any{
		"id": 1, "renameEpisodes": true, "standardEpisodeFormat": sonarrStandardFormat,
	}}
	srv := f.server()
	defer srv.Close()

	r := EnsureSonarrNaming(context.Background(), wireClient(srv.URL), false)
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

	r := EnsureRadarrNaming(context.Background(), wireClient(srv.URL), false)
	if r.Outcome != OutcomeWired || f.puts.Load() != 1 {
		t.Fatalf("fresh radarr must be wired: %+v puts=%d", r, f.puts.Load())
	}
	if f.lastPut["renameMovies"] != true || f.lastPut["standardMovieFormat"] != radarrMovieFormat || f.lastPut["movieFolderFormat"] != radarrMovieFolder {
		t.Fatalf("naming not applied: %+v", f.lastPut)
	}
}

func TestEnsureMediaManagementApplies(t *testing.T) {
	f := &fakeArrConfig{path: "/api/v3/config/mediamanagement", current: map[string]any{
		"id": 1, "downloadPropersAndRepacks": "preferAndUpgrade", "enableMediaInfo": false,
		"recycleBin": "/data/recycle", // unmanaged — must survive
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
	if f.lastPut["recycleBin"] != "/data/recycle" {
		t.Fatalf("unmanaged field lost in roundtrip: %+v", f.lastPut)
	}

	// Second pass over the now-correct config: no rewrite.
	f.current = f.lastPut
	r = EnsureMediaManagement(context.Background(), wireClient(srv.URL), "/api/v3", "Sonarr", false)
	if r.Outcome != OutcomeExisted || f.puts.Load() != 1 {
		t.Fatalf("second pass must not rewrite: %+v puts=%d", r, f.puts.Load())
	}
}
