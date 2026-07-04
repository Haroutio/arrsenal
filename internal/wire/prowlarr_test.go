package wire

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeProwlarr mimics the applications API surface observed on a live
// Prowlarr (schema template with version-specific category defaults).
type fakeProwlarr struct {
	existing []application
	posts    atomic.Int32
	lastPost application
}

func (f *fakeProwlarr) server() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/applications", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(f.existing)
	})
	mux.HandleFunc("GET /api/v1/applications/schema", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]application{
			{Implementation: "Radarr", ConfigContract: "RadarrSettings", Fields: []appField{{Name: "baseUrl", Value: "http://localhost:7878"}}},
			{Implementation: "Sonarr", ConfigContract: "SonarrSettings", Fields: []appField{
				{Name: "prowlarrUrl", Value: "http://localhost:9696"},
				{Name: "baseUrl", Value: "http://localhost:8989"},
				{Name: "apiKey", Value: nil},
				{Name: "syncCategories", Value: []int{5000, 5010, 5020, 5030, 5040, 5045, 5050, 5090}},
			}},
		})
	})
	mux.HandleFunc("POST /api/v1/applications", func(w http.ResponseWriter, r *http.Request) {
		f.posts.Add(1)
		_ = json.NewDecoder(r.Body).Decode(&f.lastPost)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{}`))
	})
	return httptest.NewServer(mux)
}

func prowlarrTarget() ArrTarget {
	return ArrTarget{
		Name: "Sonarr", Implementation: "Sonarr",
		URL: "http://sonarr:8989", APIKey: "sonarr-key-SECRET",
		ProwlarrURL: "http://prowlarr:9696",
	}
}

func wireClient(url string) *Client {
	c := NewClient(url, "prowlarr-key", "X-Api-Key")
	c.backoff = time.Millisecond
	return c
}

func TestEnsureApplicationCreatesFromSchema(t *testing.T) {
	f := &fakeProwlarr{}
	srv := f.server()
	defer srv.Close()

	r := EnsureApplication(context.Background(), wireClient(srv.URL), prowlarrTarget())
	if r.Outcome != OutcomeWired {
		t.Fatalf("want wired: %+v", r)
	}
	if f.posts.Load() != 1 {
		t.Fatalf("posts = %d", f.posts.Load())
	}
	p := f.lastPost
	if p.Name != "Sonarr" || p.Implementation != "Sonarr" || p.ConfigContract != "SonarrSettings" || p.SyncLevel != "fullSync" {
		t.Fatalf("payload envelope wrong: %+v", p)
	}
	got := map[string]any{}
	for _, fld := range p.Fields {
		got[fld.Name] = fld.Value
	}
	if got["baseUrl"] != "http://sonarr:8989" || got["apiKey"] != "sonarr-key-SECRET" || got["prowlarrUrl"] != "http://prowlarr:9696" {
		t.Fatalf("connection fields wrong: %v", got)
	}
	// The schema's own category defaults must survive — they are the
	// running Prowlarr's, not ours.
	if cats, ok := got["syncCategories"].([]any); !ok || len(cats) != 8 {
		t.Fatalf("schema defaults lost: %v", got["syncCategories"])
	}
}

func TestEnsureApplicationExistingMeansZeroWrites(t *testing.T) {
	f := &fakeProwlarr{existing: []application{{Name: "Sonarr", Implementation: "Sonarr"}}}
	srv := f.server()
	defer srv.Close()

	r := EnsureApplication(context.Background(), wireClient(srv.URL), prowlarrTarget())
	if r.Outcome != OutcomeExisted || f.posts.Load() != 0 {
		t.Fatalf("existing app must short-circuit: %+v posts=%d", r, f.posts.Load())
	}
}

func TestEnsureApplicationUnknownImplementationFails(t *testing.T) {
	f := &fakeProwlarr{}
	srv := f.server()
	defer srv.Close()

	target := prowlarrTarget()
	target.Name, target.Implementation = "Whisparr", "Whisparr"
	r := EnsureApplication(context.Background(), wireClient(srv.URL), target)
	if r.Outcome != OutcomeFailed || f.posts.Load() != 0 {
		t.Fatalf("missing template must fail without posting: %+v", r)
	}
}

// fakeProwlarrIndexers serves the indexer surfaces: list, schema, create.
type fakeProwlarrIndexers struct {
	existing string // JSON array of existing indexers
	posts    atomic.Int32
	lastBody map[string]any
}

func (f *fakeProwlarrIndexers) server() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/indexer", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(f.existing))
	})
	mux.HandleFunc("GET /api/v1/indexer/schema", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[
			{"name":"Some Torrent Tracker","implementation":"Cardigann","configContract":"CardigannSettings","fields":[]},
			{"name":"Generic Newznab","implementation":"Newznab","configContract":"NewznabSettings","protocol":"usenet",
			 "appProfileId":0,
			 "fields":[{"name":"baseUrl","value":""},{"name":"apiPath","value":"/api"},{"name":"apiKey","value":""}]}
		]`))
	})
	mux.HandleFunc("POST /api/v1/indexer", func(w http.ResponseWriter, r *http.Request) {
		f.posts.Add(1)
		_ = json.NewDecoder(r.Body).Decode(&f.lastBody)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{}`))
	})
	return httptest.NewServer(mux)
}

func TestEnsureNewznabIndexerRegisters(t *testing.T) {
	f := &fakeProwlarrIndexers{existing: `[]`}
	srv := f.server()
	defer srv.Close()

	r := EnsureNewznabIndexer(context.Background(), wireClient(srv.URL), NewznabIndexer{
		Name: "NZBgeek", URL: "https://api.nzbgeek.info", APIKey: "geek-key-SECRET"})
	if r.Outcome != OutcomeWired || f.posts.Load() != 1 {
		t.Fatalf("fresh indexer must be created: %+v posts=%d", r, f.posts.Load())
	}
	if f.lastBody["name"] != "NZBgeek" || f.lastBody["enable"] != true {
		t.Fatalf("payload basics wrong: %+v", f.lastBody)
	}
	if f.lastBody["appProfileId"] != float64(1) {
		t.Fatalf("default sync profile must be set: %+v", f.lastBody["appProfileId"])
	}
	fields := f.lastBody["fields"].([]any)
	got := map[string]any{}
	for _, fl := range fields {
		m := fl.(map[string]any)
		got[m["name"].(string)] = m["value"]
	}
	if got["baseUrl"] != "https://api.nzbgeek.info" || got["apiKey"] != "geek-key-SECRET" {
		t.Fatalf("connection fields wrong: %+v", got)
	}
}

func TestEnsureNewznabIndexerExistingUntouched(t *testing.T) {
	f := &fakeProwlarrIndexers{existing: `[{"name":"NZBgeek"}]`}
	srv := f.server()
	defer srv.Close()

	r := EnsureNewznabIndexer(context.Background(), wireClient(srv.URL), NewznabIndexer{
		Name: "NZBgeek", URL: "https://elsewhere", APIKey: "other"})
	if r.Outcome != OutcomeExisted || f.posts.Load() != 0 {
		t.Fatalf("existing indexer must be untouched: %+v posts=%d", r, f.posts.Load())
	}
}

func TestCheckIndexersHonesty(t *testing.T) {
	empty := &fakeProwlarrIndexers{existing: `[]`}
	srv := empty.server()
	defer srv.Close()

	r := CheckIndexers(context.Background(), wireClient(srv.URL), "http://host:9696")
	if r == nil || r.Outcome != OutcomeManual || !strings.Contains(r.Detail, "no indexers") {
		t.Fatalf("zero indexers must produce the manual warning: %+v", r)
	}

	populated := &fakeProwlarrIndexers{existing: `[{"name":"NZBgeek"}]`}
	srv2 := populated.server()
	defer srv2.Close()
	if r := CheckIndexers(context.Background(), wireClient(srv2.URL), "http://host:9696"); r != nil {
		t.Fatalf("populated prowlarr must be silent: %+v", r)
	}
}
