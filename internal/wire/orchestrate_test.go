package wire

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Haroutio/arrsenal/internal/registry"
)

// fakeSonarrFull serves everything the orchestrator touches on one arr.
type fakeSonarrFull struct {
	authPuts, dcPosts, rfPosts int
}

func (f *fakeSonarrFull) server() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v3/config/host", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 1, "authenticationMethod": "none"})
	})
	mux.HandleFunc("PUT /api/v3/config/host/{id}", func(w http.ResponseWriter, _ *http.Request) {
		f.authPuts++
		_, _ = w.Write([]byte(`{}`))
	})
	mux.HandleFunc("GET /api/v3/downloadclient", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	})
	mux.HandleFunc("GET /api/v3/downloadclient/schema", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]downloadClient{
			{Implementation: "Sabnzbd", ConfigContract: "SabnzbdSettings", Fields: []appField{
				{Name: "host"}, {Name: "port"}, {Name: "apiKey"}, {Name: "tvCategory"},
			}},
		})
	})
	mux.HandleFunc("POST /api/v3/downloadclient", func(w http.ResponseWriter, _ *http.Request) {
		f.dcPosts++
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{}`))
	})
	mux.HandleFunc("GET /api/v3/rootfolder", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	})
	mux.HandleFunc("POST /api/v3/rootfolder", func(w http.ResponseWriter, _ *http.Request) {
		f.rfPosts++
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{}`))
	})
	return httptest.NewServer(mux)
}

func TestOrchestrateSonarrOnly(t *testing.T) {
	// Appdata with a ready key so ReadKey returns instantly.
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sonarr"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sonarr", "config.xml"), []byte(arrConfigXML), 0o644); err != nil {
		t.Fatal(err)
	}

	f := &fakeSonarrFull{}
	srv := f.server()
	defer srv.Close()

	sonarr, _ := registry.ByID("sonarr")
	results := Orchestrate(context.Background(), Spec{
		Apps:        []registry.App{sonarr},
		Adopted:     map[string]bool{},
		AppdataRoot: root,
		AdminUser:   "adminuser", AdminPass: "pw-SECRET",
		Access:     func(string) string { return srv.URL },
		KeyTimeout: 2 * time.Second,
	})

	// Sonarr alone: auth + root folder. No prowlarr app, no download
	// clients (none selected), no jellyfin, no tail, no jellyseerr.
	if len(results) != 2 {
		t.Fatalf("want exactly auth + root folder, got %d: %+v", len(results), results)
	}
	for _, r := range results {
		if r.Outcome != OutcomeWired {
			t.Fatalf("all should wire: %+v", r)
		}
	}
	if f.authPuts != 1 || f.rfPosts != 1 || f.dcPosts != 0 {
		t.Fatalf("calls: auth=%d rf=%d dc=%d", f.authPuts, f.rfPosts, f.dcPosts)
	}

	report := RenderReport(results)
	if strings.Contains(report, "pw-SECRET") {
		t.Fatal("report leaked the credential")
	}
	if !strings.Contains(report, "2 wired") {
		t.Fatalf("summary: %s", report)
	}
}

func TestOrchestrateMissingKeyFailsThatAppOnly(t *testing.T) {
	root := t.TempDir() // no configs at all → key read times out
	sonarr, _ := registry.ByID("sonarr")
	results := Orchestrate(context.Background(), Spec{
		Apps:        []registry.App{sonarr},
		AppdataRoot: root,
		AdminUser:   "u", AdminPass: "p",
		Access:     func(string) string { return "http://127.0.0.1:1" },
		KeyTimeout: 50 * time.Millisecond,
	})
	if len(results) != 1 || results[0].Outcome != OutcomeFailed {
		t.Fatalf("keyless app must fail its key line and skip its lanes: %+v", results)
	}
	if !strings.Contains(results[0].Detail, "docker logs sonarr") {
		t.Fatalf("failure should point at the container logs: %+v", results[0])
	}
}
