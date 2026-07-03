package wire

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeSeerr models the init API surface the lane drives.
type fakeSeerr struct {
	initialized bool
	signInBody  map[string]any
	enabled     string
	arrBodies   map[string]map[string]any
	finalized   bool
	calls       []string
}

func (f *fakeSeerr) server() *httptest.Server {
	if f.arrBodies == nil {
		f.arrBodies = map[string]map[string]any{}
	}
	mux := http.NewServeMux()
	record := func(r *http.Request) { f.calls = append(f.calls, r.Method+" "+r.URL.Path) }

	mux.HandleFunc("GET /api/v1/settings/public", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		_ = json.NewEncoder(w).Encode(map[string]any{"initialized": f.initialized})
	})
	mux.HandleFunc("POST /api/v1/auth/jellyfin", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		_ = json.NewDecoder(r.Body).Decode(&f.signInBody)
		http.SetCookie(w, &http.Cookie{Name: "connect.sid", Value: "session", Path: "/"})
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 1})
	})
	mux.HandleFunc("GET /api/v1/settings/jellyfin/library", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		if c, err := r.Cookie("connect.sid"); err != nil || c.Value != "session" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if enable := r.URL.Query().Get("enable"); enable != "" {
			f.enabled = enable
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": "lib-movies", "name": "Movies", "enabled": f.enabled != ""},
			{"id": "lib-shows", "name": "Shows", "enabled": f.enabled != ""},
		})
	})
	for _, arr := range []string{"sonarr", "radarr"} {
		arr := arr
		mux.HandleFunc("POST /api/v1/settings/"+arr, func(w http.ResponseWriter, r *http.Request) {
			record(r)
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.arrBodies[arr] = body
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(body)
		})
	}
	mux.HandleFunc("POST /api/v1/settings/initialize", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		if c, err := r.Cookie("connect.sid"); err != nil || c.Value != "session" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		f.finalized = true
		_ = json.NewEncoder(w).Encode(map[string]any{"initialized": true})
	})
	return httptest.NewServer(mux)
}

func seerrTarget(url string) SeerrTarget {
	return SeerrTarget{
		URL: url, HostAccessURL: "http://host:5055",
		ServerType: "jellyfin", ServerHost: "jellyfin", ServerPort: 8096,
		AdminUser: "harout", AdminPass: "seerr-pass-SECRET",
		Sonarr: &SeerrArr{Name: "Sonarr", Host: "sonarr", Port: 8989, APIKey: "sonk",
			ProfileID: 7, ProfileName: "WEB-1080p", RootFolder: "/data/media/tv"},
		Radarr: &SeerrArr{Name: "Radarr", Host: "radarr", Port: 7878, APIKey: "radk",
			ProfileID: 9, ProfileName: "HD Bluray + WEB", RootFolder: "/data/media/movies"},
	}
}

func TestSeerrFullAutoWithJellyfin(t *testing.T) {
	f := &fakeSeerr{}
	srv := f.server()
	defer srv.Close()

	results := EnsureSeerr(context.Background(), seerrTarget(srv.URL))

	for _, r := range results {
		if r.Outcome != OutcomeWired {
			t.Fatalf("everything should wire on fresh: %+v", results)
		}
	}
	if len(results) != 5 { // sign-in, libraries, sonarr, radarr, initialized
		t.Fatalf("want 5 results, got %+v", results)
	}
	if !f.finalized {
		t.Fatal("initialize flag never flipped")
	}
	if f.signInBody["serverType"] != float64(2) || f.signInBody["hostname"] != "jellyfin" {
		t.Fatalf("sign-in payload wrong: %+v", f.signInBody)
	}
	if f.enabled != "lib-movies,lib-shows" {
		t.Fatalf("all libraries must be enabled, got %q", f.enabled)
	}
	son := f.arrBodies["sonarr"]
	if son["activeProfileId"] != float64(7) || son["activeDirectory"] != "/data/media/tv" ||
		son["isDefault"] != true || son["syncEnabled"] != true {
		t.Fatalf("sonarr payload wrong: %+v", son)
	}
	if f.arrBodies["radarr"]["activeProfileName"] != "HD Bluray + WEB" {
		t.Fatalf("radarr payload wrong: %+v", f.arrBodies["radarr"])
	}
}

func TestSeerrInitializedIsAdopted(t *testing.T) {
	f := &fakeSeerr{initialized: true}
	srv := f.server()
	defer srv.Close()

	results := EnsureSeerr(context.Background(), seerrTarget(srv.URL))
	if len(results) != 1 || results[0].Outcome != OutcomeExisted {
		t.Fatalf("initialized seerr must be adopted untouched: %+v", results)
	}
	if len(f.calls) != 1 { // the public probe and nothing else
		t.Fatalf("adopted seerr must see zero writes: %v", f.calls)
	}
}

func TestSeerrPlexStaysManual(t *testing.T) {
	f := &fakeSeerr{}
	srv := f.server()
	defer srv.Close()

	target := seerrTarget(srv.URL)
	target.ServerType = "plex"
	results := EnsureSeerr(context.Background(), target)
	if len(results) != 1 || results[0].Outcome != OutcomeManual {
		t.Fatalf("plex pairing is the manual wizard: %+v", results)
	}
	if !strings.Contains(results[0].Detail, "OAuth") {
		t.Fatalf("the manual note should say why: %+v", results[0])
	}
}

func TestSeerrNoCredentialStaysManual(t *testing.T) {
	f := &fakeSeerr{}
	srv := f.server()
	defer srv.Close()

	target := seerrTarget(srv.URL)
	target.AdminPass = ""
	results := EnsureSeerr(context.Background(), target)
	if len(results) != 1 || results[0].Outcome != OutcomeManual {
		t.Fatalf("no credential → manual wizard: %+v", results)
	}
}

func TestSeerrSignInFailureDegradesToManual(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/settings/public", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"initialized": false})
	})
	mux.HandleFunc("POST /api/v1/auth/jellyfin", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	results := EnsureSeerr(context.Background(), seerrTarget(srv.URL))
	last := results[len(results)-1]
	if last.Outcome != OutcomeManual || !strings.Contains(last.Detail, "sign-in") {
		t.Fatalf("a failed step must degrade to the manual note: %+v", results)
	}
}

func TestSeerrUnreachableIsManualNeverFailed(t *testing.T) {
	results := EnsureSeerr(context.Background(), SeerrTarget{
		URL: "http://127.0.0.1:1", HostAccessURL: "http://host:5055", ServerType: "jellyfin", AdminPass: "x"})
	if len(results) != 1 || results[0].Outcome != OutcomeManual {
		t.Fatalf("unreachable seerr must never fail the run: %+v", results)
	}
}
