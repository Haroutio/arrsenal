package wire

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

// fakeBazarr serves the two endpoints the language pre-seed touches.
type fakeBazarr struct {
	profiles string // JSON array of existing profiles
	posts    atomic.Int32
	lastForm url.Values
}

func (f *fakeBazarr) server() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/system/languages/profiles", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-KEY") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(f.profiles))
	})
	mux.HandleFunc("POST /api/system/settings", func(w http.ResponseWriter, r *http.Request) {
		f.posts.Add(1)
		_ = r.ParseForm()
		f.lastForm = r.PostForm
		w.WriteHeader(http.StatusNoContent)
	})
	return httptest.NewServer(mux)
}

func TestEnsureBazarrLanguagesSeedsEnglish(t *testing.T) {
	f := &fakeBazarr{profiles: `[]`}
	srv := f.server()
	defer srv.Close()

	c := NewClient(srv.URL, "bz-key", "X-API-KEY")
	r := EnsureBazarrLanguages(context.Background(), c, false)
	if r.Outcome != OutcomeWired || f.posts.Load() != 1 {
		t.Fatalf("fresh bazarr must be seeded: %+v posts=%d", r, f.posts.Load())
	}
	if got := f.lastForm.Get("languages-enabled"); got != "en" {
		t.Fatalf("languages-enabled: %q", got)
	}
	for _, k := range []string{
		"settings-general-serie_default_enabled",
		"settings-general-movie_default_enabled",
	} {
		if f.lastForm.Get(k) != "true" {
			t.Fatalf("%s: %q", k, f.lastForm.Get(k))
		}
	}
	if f.lastForm.Get("settings-general-serie_default_profile") != "1" ||
		f.lastForm.Get("settings-general-movie_default_profile") != "1" {
		t.Fatalf("default profile ids: %v", f.lastForm)
	}
	// The profile payload must be the JSON Bazarr's handler expects —
	// parseable, one profile named English on id 1 with language en.
	var profiles []struct {
		ProfileID int    `json:"profileId"`
		Name      string `json:"name"`
		Items     []struct {
			Language string `json:"language"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(f.lastForm.Get("languages-profiles")), &profiles); err != nil {
		t.Fatalf("languages-profiles must be valid JSON: %v", err)
	}
	if len(profiles) != 1 || profiles[0].ProfileID != 1 || profiles[0].Name != "English" ||
		len(profiles[0].Items) != 1 || profiles[0].Items[0].Language != "en" {
		t.Fatalf("profile payload: %+v", profiles)
	}
}

func TestEnsureBazarrLanguagesBacksOff(t *testing.T) {
	// A profile already exists — user-made or from an earlier run.
	populated := &fakeBazarr{profiles: `[{"profileId":3,"name":"Klingon"}]`}
	srv := populated.server()
	defer srv.Close()

	c := NewClient(srv.URL, "bz-key", "X-API-KEY")
	r := EnsureBazarrLanguages(context.Background(), c, false)
	if r.Outcome != OutcomeExisted || populated.posts.Load() != 0 {
		t.Fatalf("existing profiles must back the lane off: %+v posts=%d", r, populated.posts.Load())
	}

	// Adopted: no HTTP at all.
	dead := NewClient("http://127.0.0.1:1", "bz-key", "X-API-KEY")
	r = EnsureBazarrLanguages(context.Background(), dead, true)
	if r.Outcome != OutcomeExisted || !strings.Contains(r.Detail, "adopted") {
		t.Fatalf("adopted bazarr must short-circuit: %+v", r)
	}
}
